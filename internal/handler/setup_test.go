package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/middleware"
)

// setupTestEngine 自建最小 gin 引擎，仅注册 setup + login 相关路由并加载视图模板。
// 不依赖 router.Setup（refactor 期间 router 暂不可编译）。
func setupTestEngine(t *testing.T) (*gin.Engine, *handler.Handler) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := &handler.Handler{Store: st, ServerSecret: []byte("s")}

	r := gin.New()
	r.LoadHTMLGlob(viewGlobForTest())
	r.GET("/admin/login", h.LoginPage)
	r.POST("/admin/login", h.LoginSubmit)
	r.GET("/setup", h.SetupPage)
	r.POST("/setup", h.SetupSubmit)
	return r, h
}

// viewGlobForTest 定位 internal/view/*.html，兼容不同 CWD。
func viewGlobForTest() string {
	const rel = "../view/*.html"
	if m, _ := filepath.Glob(rel); len(m) > 0 {
		return rel
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		abs := filepath.Join(filepath.Dir(thisFile), "..", "view", "*.html")
		if m, _ := filepath.Glob(abs); len(m) > 0 {
			return abs
		}
	}
	return rel
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.ServeHTTP(rec, req)
	return rec
}

func hasSessionCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.SessionCookie && c.Value != "" {
			return true
		}
	}
	return false
}

func TestSetup_EmptyStoreRedirectsLoginToSetup(t *testing.T) {
	r, _ := setupTestEngine(t)
	rec := do(r, http.MethodGet, "/admin/login", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("want 302 -> /setup, got %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSetup_CreatesFirstSuperAdmin(t *testing.T) {
	r, h := setupTestEngine(t)
	form := url.Values{"username": {"root"}, "password": {"pw123456"}}.Encode()
	rec := do(r, http.MethodPost, "/setup", form)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("want 302 -> /admin, got %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if !hasSessionCookie(rec) {
		t.Fatalf("expected %s cookie to be set", middleware.SessionCookie)
	}
	if n, err := h.Store.CountUsers(); err != nil || n != 1 {
		t.Fatalf("want 1 user, got %d (err=%v)", n, err)
	}

	// 初始化后 GET /setup 应跳登录。
	rec2 := do(r, http.MethodGet, "/setup", "")
	if rec2.Code != http.StatusFound || rec2.Header().Get("Location") != "/admin/login" {
		t.Fatalf("want 302 -> /admin/login, got %d -> %q", rec2.Code, rec2.Header().Get("Location"))
	}
}

func TestSetup_RejectsEmptyFields(t *testing.T) {
	r, h := setupTestEngine(t)
	form := url.Values{"username": {""}, "password": {"pw"}}.Encode()
	rec := do(r, http.MethodPost, "/setup", form)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup?error=1" {
		t.Fatalf("want 302 -> /setup?error=1, got %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if n, _ := h.Store.CountUsers(); n != 0 {
		t.Fatalf("expected no user created, got %d", n)
	}
}

func TestLogin_CorrectAndWrongCreds(t *testing.T) {
	r, _ := setupTestEngine(t)
	// 先初始化一个用户。
	do(r, http.MethodPost, "/setup", url.Values{"username": {"root"}, "password": {"pw123456"}}.Encode())

	// 正确凭据。
	ok := do(r, http.MethodPost, "/admin/login", url.Values{"username": {"root"}, "password": {"pw123456"}}.Encode())
	if ok.Code != http.StatusFound || ok.Header().Get("Location") != "/admin" {
		t.Fatalf("correct creds: want 302 -> /admin, got %d -> %q", ok.Code, ok.Header().Get("Location"))
	}
	if !hasSessionCookie(ok) {
		t.Fatalf("correct creds: expected session cookie")
	}

	// 错误凭据。
	bad := do(r, http.MethodPost, "/admin/login", url.Values{"username": {"root"}, "password": {"wrong"}}.Encode())
	if bad.Code != http.StatusFound || bad.Header().Get("Location") != "/admin/login?error=1" {
		t.Fatalf("wrong creds: want 302 -> /admin/login?error=1, got %d -> %q", bad.Code, bad.Header().Get("Location"))
	}
}
