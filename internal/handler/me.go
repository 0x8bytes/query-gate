package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/pkg/apierror"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// 个人中心 handler 一律只操作 CurrentUser(c)，绝不信任 body 里的用户名（防越权）。

// MeGet 返回当前登录用户自身信息。
func (h *Handler) MeGet(c *gin.Context) {
	me := middleware.CurrentUser(c)
	u, found, err := h.Store.GetUserByName(me)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"username": u.Username,
		"role":     u.Role,
		"api_key":  u.APIKey,
	})
}

type mePasswordReq struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// MePassword 改自己密码，须校验旧密码。
func (h *Handler) MePassword(c *gin.Context) {
	var req mePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.New == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "new password is required"))
		return
	}
	me := middleware.CurrentUser(c)
	u, found, err := h.Store.GetUserByName(me)
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	if !found {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "user not found"})
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Old) {
		apierror.Write(c.Writer, &apierror.APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "old password incorrect"})
		return
	}
	hash, err := auth.HashPassword(req.New)
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
