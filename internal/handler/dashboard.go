package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

const sessionMaxAge = 7 * 24 * 60 * 60 // 7 天

// LoginPage 渲染登录页。无用户时跳初始化；已登录则进 dashboard。
func (h *Handler) LoginPage(c *gin.Context) {
	if n, err := h.Store.CountUsers(); err == nil && n == 0 {
		c.Redirect(http.StatusFound, "/setup")
		return
	}
	c.HTML(http.StatusOK, "login.html", gin.H{"Error": c.Query("error") != ""})
}

// LoginSubmit 校验 username+password，成功下发 session cookie。
func (h *Handler) LoginSubmit(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	u, ok, err := h.Store.GetUserByName(username)
	if err != nil || !ok || u.Status != "enabled" || !auth.CheckPassword(u.PasswordHash, password) {
		c.Redirect(http.StatusFound, "/admin/login?error=1")
		return
	}
	h.issueSession(c, u.Username, u.Role)
	c.Redirect(http.StatusFound, "/admin")
}

// Logout 清除 session cookie。
func (h *Handler) Logout(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.SessionCookie, "", -1, "/", "", isHTTPS(c.Request), true)
	c.Redirect(http.StatusFound, "/admin/login")
}

// Dashboard 渲染后台主页，注入当前用户名与角色（前端按角色显隐）。
func (h *Handler) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin.html", gin.H{
		"Username": middleware.CurrentUser(c),
		"Role":     middleware.CurrentRole(c),
	})
}

// issueSession 下发签名 session cookie。
func (h *Handler) issueSession(c *gin.Context, username, role string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.SessionCookie, auth.SignSession(h.ServerSecret, username, role), sessionMaxAge, "/", "", isHTTPS(c.Request), true)
}

// isHTTPS 判断当前请求是否经由 https（含反向代理 X-Forwarded-Proto）。
// 用于自动决定 session cookie 的 Secure 属性，免去手动配置。
func isHTTPS(r *http.Request) bool {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p == "https"
	}
	return r.TLS != nil
}

// nowRFC3339 返回当前 UTC RFC3339 时间串。
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
