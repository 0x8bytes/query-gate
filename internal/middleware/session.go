package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

const SessionCookie = "qn_session"

const (
	ctxCurrentUser = "current_user"
	ctxCurrentRole = "current_role"
)

// Session 校验后台 session cookie 并每请求查库确认用户 enabled 且角色未变。
// redirect=true 用于页面请求（失败 302 /admin/login）；false 用于 JSON（失败 401）。
func Session(store *data.Store, secret []byte, redirect bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		fail := func() {
			if redirect {
				c.Redirect(http.StatusFound, "/admin/login")
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "unauthorized", "message": "login required"}})
			}
			c.Abort()
		}
		cookie, err := c.Request.Cookie(SessionCookie)
		if err != nil {
			fail()
			return
		}
		username, role, ok := auth.ParseSession(secret, cookie.Value)
		if !ok {
			fail()
			return
		}
		u, found, err := store.GetUserByName(username)
		if err != nil || !found || u.Status != "enabled" || u.Role != role {
			fail()
			return
		}
		c.Set(ctxCurrentUser, username)
		c.Set(ctxCurrentRole, role)
		c.Next()
	}
}

// RequireRole 要求当前会话角色等于 role，否则 403。须在 Session 之后。
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentRole(c) != role {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "forbidden", "message": "insufficient role"}})
			c.Abort()
			return
		}
		c.Next()
	}
}

func CurrentUser(c *gin.Context) string {
	if v, ok := c.Get(ctxCurrentUser); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func CurrentRole(c *gin.Context) string {
	if v, ok := c.Get(ctxCurrentRole); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
