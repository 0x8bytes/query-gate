package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/internal/router"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// testSecret 测试用 session 签名密钥；cookie 与 handler.ServerSecret 必须一致。
var testSecret = []byte("test-secret")

// seedSuperAdmin 往 store 写入一个 enabled super_admin 用户 root（密码 pw）。
func seedSuperAdmin(t *testing.T, st *data.Store) {
	t.Helper()
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.CreateUser(model.User{
		Username: "root", PasswordHash: hash, APIKey: "qn_root",
		Role: model.RoleSuperAdmin, Status: "enabled", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed super_admin: %v", err)
	}
}

// newAuthHandler 构建一个带 store(已 seed root super_admin)+ ServerSecret 的 handler。
func newAuthHandler(t *testing.T) *handler.Handler {
	t.Helper()
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedSuperAdmin(t, st)
	return &handler.Handler{Store: st, ServerSecret: testSecret}
}

func TestLoginSubmit_GoodPassword_SetsCookie(t *testing.T) {
	h := newAuthHandler(t)
	r := router.Setup(h)
	form := url.Values{"username": {"root"}, "password": {"pw"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", w.Code)
	}
	want := auth.SignSession(testSecret, "root", model.RoleSuperAdmin)
	var found bool
	for _, c := range w.Result().Cookies() {
		// token 用 base64url + "." 分隔，不会被 cookie 写入 URL 编码，往返一致。
		if c.Name == middleware.SessionCookie && c.Value == want {
			found = true
		}
	}
	if !found {
		t.Fatal("expected valid session cookie")
	}
}

func TestLoginSubmit_BadPassword_NoCookie(t *testing.T) {
	h := newAuthHandler(t)
	r := router.Setup(h)
	form := url.Values{"username": {"root"}, "password": {"wrong"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("want 302 back to login, got %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == middleware.SessionCookie && c.Value != "" && c.MaxAge >= 0 {
			t.Fatal("must not set a valid cookie on bad password")
		}
	}
}

func TestDashboard_NoCookie_Redirects(t *testing.T) {
	h := newAuthHandler(t)
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", w.Code)
	}
}

type fakeDriver struct {
	info   model.DatabaseInfo
	tables []model.TableInfo
	ddl    map[string]string
	result *model.QueryResult
}

func (f *fakeDriver) Info() model.DatabaseInfo                              { return f.info }
func (f *fakeDriver) Tables(ctx context.Context) ([]model.TableInfo, error) { return f.tables, nil }
func (f *fakeDriver) Schema(ctx context.Context, names []string) (map[string]string, []string, error) {
	out := map[string]string{}
	var nf []string
	for _, n := range names {
		if d, ok := f.ddl[n]; ok {
			out[n] = d
		} else {
			nf = append(nf, n)
		}
	}
	return out, nf, nil
}
func (f *fakeDriver) Query(ctx context.Context, q string, limit, maxRows int) (*model.QueryResult, error) {
	return f.result, nil
}
func (f *fakeDriver) Exec(ctx context.Context, command string) (int64, error) { return 0, nil }
func (f *fakeDriver) Close() error                                            { return nil }

func handlerWithDB(t *testing.T, name string, fd *fakeDriver) *handler.Handler {
	t.Helper()
	reg := driver.NewRegistry()
	reg.Register(name, fd, driver.SourceDynamic)
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedSuperAdmin(t, st)
	return &handler.Handler{Registry: reg, Store: st, ServerSecret: testSecret, MaxRows: 1000}
}

func sessionCookie() *http.Cookie {
	return &http.Cookie{Name: middleware.SessionCookie, Value: auth.SignSession(testSecret, "root", model.RoleSuperAdmin)}
}

func TestUITree_TopLevel(t *testing.T) {
	fd := &fakeDriver{info: model.DatabaseInfo{Name: "dh", Driver: "mysql", Description: "x"}}
	h := handlerWithDB(t, "dh", fd)
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/tree", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie())
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"dh"`) {
		t.Fatalf("expected db dh: %s", w.Body.String())
	}
}

func TestUITree_DBLevel(t *testing.T) {
	fd := &fakeDriver{
		info:   model.DatabaseInfo{Name: "dh", Driver: "mysql"},
		tables: []model.TableInfo{{Name: "users", Comment: "agent"}},
	}
	h := handlerWithDB(t, "dh", fd)
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/tree", strings.NewReader(`{"db":"dh"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie())
	r.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Tables") || !strings.Contains(body, "users") {
		t.Fatalf("expected Tables/users: %s", body)
	}
}

func TestUIQuery_OK(t *testing.T) {
	fd := &fakeDriver{result: &model.QueryResult{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}, RowCount: 1}}
	h := handlerWithDB(t, "dh", fd)
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/query",
		strings.NewReader(`{"db":"dh","sql":"select * from users limit 50"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie())
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id"`) {
		t.Fatalf("expected column id: %s", w.Body.String())
	}
}

func TestUIQuery_UnknownDB(t *testing.T) {
	h := handlerWithDB(t, "dh", &fakeDriver{})
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/query",
		strings.NewReader(`{"db":"nope","sql":"select 1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie())
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUIDDL_OK(t *testing.T) {
	fd := &fakeDriver{ddl: map[string]string{"users": "CREATE TABLE users(...)"}}
	h := handlerWithDB(t, "dh", fd)
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/ddl",
		strings.NewReader(`{"db":"dh","table":"users"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie())
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "CREATE TABLE") {
		t.Fatalf("expected ddl: %s", w.Body.String())
	}
}

func TestUIQuery_NoCookie_401(t *testing.T) {
	h := handlerWithDB(t, "dh", &fakeDriver{})
	r := router.Setup(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ui/query",
		strings.NewReader(`{"db":"dh","sql":"select 1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}
