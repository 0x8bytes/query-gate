// Package middleware 提供认证中间件。
package middleware

import (
	"log"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/pkg/apierror"
)

const apiKeyNameKey = "api_key_name"
const apiKeyRoleKey = "api_key_role"

// APIKey 校验 X-API-Key 是否为有效(enabled)普通 key:直查 SQLite,不依赖内存缓存,
// admin 改 key 后立即生效。通过则把 key 的 name 存入 gin context。
func APIKey(store *data.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			apierror.Write(c.Writer, apierror.ErrUnauthorized)
			c.Abort()
			return
		}
		rec, ok, err := store.GetUserByAPIKey(key)
		if err != nil {
			log.Printf("apikey lookup: %v", err)
			apierror.Write(c.Writer, apierror.ErrUnauthorized)
			c.Abort()
			return
		}
		if !ok || rec.Status != "enabled" {
			apierror.Write(c.Writer, apierror.ErrUnauthorized)
			c.Abort()
			return
		}
		c.Set(apiKeyNameKey, rec.Username)
		c.Set(apiKeyRoleKey, rec.Role)
		c.Next()
	}
}

// APIKeyName 从 gin context 取调用方 key 的 name(用于查询日志)。空表示未认证。
func APIKeyName(c *gin.Context) string {
	if v, ok := c.Get(apiKeyNameKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// APIKeyRole 从 gin context 取调用方 key 对应用户的角色。空表示未认证。
func APIKeyRole(c *gin.Context) string {
	if v, ok := c.Get(apiKeyRoleKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
