package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

func init() { gin.SetMode(gin.TestMode) }

var secret = []byte("test-secret")

func newStore(t *testing.T) *data.Store {
	t.Helper()
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func addUser(t *testing.T, st *data.Store, name, role, status string) {
	t.Helper()
	if err := st.CreateUser(model.User{Username: name, PasswordHash: "h", APIKey: "k_" + name, Role: role, Status: status, CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
}

func TestSession_NoCookie(t *testing.T) {
	st := newStore(t)
	r := gin.New()
	r.POST("/p", Session(st, secret, false), func(c *gin.Context) { c.JSON(200, gin.H{}) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/p", nil))
	if w.Code != 401 {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestSession_PageRedirect(t *testing.T) {
	st := newStore(t)
	r := gin.New()
	r.GET("/admin", Session(st, secret, true), func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin", nil))
	if w.Code != 302 {
		t.Fatalf("want 302, got %d", w.Code)
	}
}

func TestSession_ValidInjectsUser(t *testing.T) {
	st := newStore(t)
	addUser(t, st, "bob", "user", "enabled")
	r := gin.New()
	r.GET("/admin", Session(st, secret, true), func(c *gin.Context) {
		c.String(200, CurrentUser(c)+"/"+CurrentRole(c))
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth.SignSession(secret, "bob", "user")})
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "bob/user" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestSession_DisabledUserRejected(t *testing.T) {
	st := newStore(t)
	addUser(t, st, "bob", "user", "disabled")
	r := gin.New()
	r.POST("/p", Session(st, secret, false), func(c *gin.Context) { c.JSON(200, gin.H{}) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/p", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth.SignSession(secret, "bob", "user")})
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("disabled user must be rejected, got %d", w.Code)
	}
}

func TestSession_RoleMismatchRejected(t *testing.T) {
	st := newStore(t)
	addUser(t, st, "bob", "user", "enabled")
	r := gin.New()
	r.POST("/p", Session(st, secret, false), func(c *gin.Context) { c.JSON(200, gin.H{}) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/p", nil)
	// token claims super_admin but DB says user → reject
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth.SignSession(secret, "bob", "super_admin")})
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("role mismatch must be rejected, got %d", w.Code)
	}
}

func TestRequireRole(t *testing.T) {
	st := newStore(t)
	addUser(t, st, "bob", "user", "enabled")
	addUser(t, st, "adm", "super_admin", "enabled")
	r := gin.New()
	r.GET("/x", Session(st, secret, false), RequireRole("super_admin"), func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth.SignSession(secret, "bob", "user")})
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403 for user, got %d", w.Code)
	}
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/x", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth.SignSession(secret, "adm", "super_admin")})
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("want 200 for super_admin, got %d", w2.Code)
	}
}
