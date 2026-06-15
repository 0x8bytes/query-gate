package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// SetupPage 首次初始化页。已有用户则跳登录。
func (h *Handler) SetupPage(c *gin.Context) {
	if n, err := h.Store.CountUsers(); err != nil || n > 0 {
		c.Redirect(http.StatusFound, "/admin/login")
		return
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{"Error": c.Query("error") != ""})
}

// SetupSubmit 创建第一个 super_admin（仅当无用户时）。
func (h *Handler) SetupSubmit(c *gin.Context) {
	if n, err := h.Store.CountUsers(); err != nil || n > 0 {
		c.String(http.StatusForbidden, "already initialized")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	if username == "" || password == "" || strings.Contains(username, "|") {
		c.Redirect(http.StatusFound, "/setup?error=1")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		c.Redirect(http.StatusFound, "/setup?error=1")
		return
	}
	now := nowRFC3339()
	if err := h.Store.CreateUser(model.User{
		Username: username, PasswordHash: hash, APIKey: genKey(),
		Role: model.RoleSuperAdmin, Status: "enabled", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		c.Redirect(http.StatusFound, "/setup?error=1")
		return
	}
	h.issueSession(c, username, model.RoleSuperAdmin)
	c.Redirect(http.StatusFound, "/admin")
}
