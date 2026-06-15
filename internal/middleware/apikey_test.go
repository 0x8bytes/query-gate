package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/model"
)

func TestAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	// 一个 enabled 用户与一个 disabled 用户（API key 即用户的 APIKey）
	if err := st.CreateUser(model.User{Username: "claude", PasswordHash: "h", APIKey: "good", Role: "user", Status: "enabled", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(model.User{Username: "off", PasswordHash: "h", APIKey: "off", Role: "user", Status: "disabled", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(APIKey(st))
	r.GET("/x", func(c *gin.Context) {
		if APIKeyName(c) != "claude" {
			t.Errorf("name not in ctx")
		}
		c.Status(http.StatusOK)
	})

	cases := []struct {
		key  string
		want int
	}{
		{"good", http.StatusOK},
		{"off", http.StatusUnauthorized}, // disabled 用户被拒
		{"bad", http.StatusUnauthorized}, // 不存在
		{"", http.StatusUnauthorized},    // 缺 header
	}
	for _, cse := range cases {
		req := httptest.NewRequest("GET", "/x", nil)
		if cse.key != "" {
			req.Header.Set("X-API-Key", cse.key)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != cse.want {
			t.Errorf("key=%q code=%d want=%d", cse.key, rec.Code, cse.want)
		}
	}
}
