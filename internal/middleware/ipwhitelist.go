package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/pkg/apierror"
)

// IPWhitelist 校验客户端 IP。列表含 "*" 时放行所有。
func IPWhitelist(allowed []string) gin.HandlerFunc {
	wildcard := false
	set := map[string]bool{}
	for _, a := range allowed {
		if a == "*" {
			wildcard = true
		}
		set[a] = true
	}
	return func(c *gin.Context) {
		if wildcard {
			c.Next()
			return
		}
		// c.ClientIP() 已对 RemoteAddr 做 SplitHostPort 解析;
		// 引擎设置 SetTrustedProxies(nil) 后只信任 RemoteAddr。
		if !set[c.ClientIP()] {
			apierror.Write(c.Writer, apierror.ErrForbiddenIP)
			c.Abort()
			return
		}
		c.Next()
	}
}
