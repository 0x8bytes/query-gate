package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/apierror"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// 这些 handler 假定调用方已是 super_admin（由 router 的 RequireRole 保证），handler 内不再复查角色。

const (
	statusEnabled  = "enabled"
	statusDisabled = "disabled"
)

// validRole 校验角色取值。
func validRole(role string) bool {
	return role == model.RoleSuperAdmin || role == model.RoleUser
}

// isLastSuperAdmin 判断 target 是否为最后一个 enabled 超管。
func (h *Handler) isLastSuperAdmin(target model.User) (bool, error) {
	if target.Role != model.RoleSuperAdmin || target.Status != statusEnabled {
		return false, nil
	}
	n, err := h.Store.CountEnabledSuperAdmins()
	if err != nil {
		return false, err
	}
	return n <= 1, nil
}

// UserList 返回全部用户（api_key 以明文返回 —— 产品决策）。
func (h *Handler) UserList(c *gin.Context) {
	users, err := h.Store.ListUsers()
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, gin.H{
			"username":   u.Username,
			"role":       u.Role,
			"status":     u.Status,
			"api_key":    u.APIKey,
			"created_at": u.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type userCreateReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UserCreate 创建用户，返回一次性明文 api_key。
func (h *Handler) UserCreate(c *gin.Context) {
	var req userCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Username == "" || strings.Contains(req.Username, "|") {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "username is required and must not contain '|'"))
		return
	}
	if req.Password == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "password is required"))
		return
	}
	if !validRole(req.Role) {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "role must be super_admin or user"))
		return
	}
	if _, exists, err := h.Store.GetUserByName(req.Username); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	} else if exists {
		apierror.Write(c.Writer, apierror.BadRequest("duplicate", "username already exists"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	now := nowRFC3339()
	key := genKey()
	if err := h.Store.CreateUser(model.User{
		Username:     req.Username,
		PasswordHash: hash,
		APIKey:       key,
		Role:         req.Role,
		Status:       statusEnabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"username": req.Username, "api_key": key})
}

type userResetPasswordReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// UserResetPassword 重置指定用户密码，保留其余字段不变。
func (h *Handler) UserResetPassword(c *gin.Context) {
	var req userResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Username == "" || req.Password == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "username and password are required"))
		return
	}
	u, found, err := h.Store.GetUserByName(req.Username)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	u.PasswordHash = hash
	u.UpdatedAt = nowRFC3339()
	if err := h.Store.UpdateUser(u); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type userSetRoleReq struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// UserSetRole 改角色。把最后一个 enabled 超管降级会被拒（保留至少一名超管）。
func (h *Handler) UserSetRole(c *gin.Context) {
	var req userSetRoleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Username == "" || !validRole(req.Role) {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "username and valid role are required"))
		return
	}
	u, found, err := h.Store.GetUserByName(req.Username)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	// 降级守卫：把最后一个 enabled 超管降为非超管 → 拒绝。
	if req.Role != model.RoleSuperAdmin {
		last, err := h.isLastSuperAdmin(u)
		if err != nil {
			apierror.Write(c.Writer, apierror.ErrInternal)
			return
		}
		if last {
			apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "must keep at least one super admin"})
			return
		}
	}
	u.Role = req.Role
	u.UpdatedAt = nowRFC3339()
	if err := h.Store.UpdateUser(u); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type userSetStatusReq struct {
	Username string `json:"username"`
	Status   string `json:"status"`
}

// UserSetStatus 启用/停用用户。不可停用自己；不可停用最后一个超管。
func (h *Handler) UserSetStatus(c *gin.Context) {
	var req userSetStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Username == "" || (req.Status != statusEnabled && req.Status != statusDisabled) {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "username and valid status are required"))
		return
	}
	if req.Status == statusDisabled && req.Username == middleware.CurrentUser(c) {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "cannot disable yourself"})
		return
	}
	u, found, err := h.Store.GetUserByName(req.Username)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	if req.Status == statusDisabled {
		last, err := h.isLastSuperAdmin(u)
		if err != nil {
			apierror.Write(c.Writer, apierror.ErrInternal)
			return
		}
		if last {
			apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "must keep at least one super admin"})
			return
		}
	}
	u.Status = req.Status
	u.UpdatedAt = nowRFC3339()
	if err := h.Store.UpdateUser(u); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type userDeleteReq struct {
	Username string `json:"username"`
}

// UserDelete 删除用户。不可删自己；不可删最后一个超管。
func (h *Handler) UserDelete(c *gin.Context) {
	var req userDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Username == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "username is required"))
		return
	}
	if req.Username == middleware.CurrentUser(c) {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "cannot delete yourself"})
		return
	}
	u, found, err := h.Store.GetUserByName(req.Username)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	last, err := h.isLastSuperAdmin(u)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if last {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "must keep at least one super admin"})
		return
	}
	if err := h.Store.DeleteUser(req.Username); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
