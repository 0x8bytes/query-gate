package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newIPTestEngine 构建只信任 RemoteAddr 的 gin 引擎(SetTrustedProxies(nil)),
// 注册 IPWhitelist 中间件 + 一个 200 handler。
func newIPTestEngine(allowed []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(IPWhitelist(allowed))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestIPWhitelist_Wildcard(t *testing.T) {
	r := newIPTestEngine([]string{"*"})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:12345"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wildcard should allow all, got %d", rec.Code)
	}
}

func TestIPWhitelist_SpecificIPs(t *testing.T) {
	r := newIPTestEngine([]string{"203.0.113.9", "198.51.100.1"})

	cases := []struct {
		remote string
		want   int
	}{
		{"203.0.113.9:1", http.StatusOK},
		{"198.51.100.1:2", http.StatusOK},
		{"10.0.0.1:3", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("remote=%s code=%d want=%d", c.remote, rec.Code, c.want)
		}
	}
}
