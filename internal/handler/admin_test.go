package handler_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/internal/router"
)

// fakeDriverForTest 用于 seed-delete 测试:注册一个 SourceSeed 实例,验证只读保护。
type fakeDriverForTest struct{}

func (fakeDriverForTest) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: "seedprod", Driver: "mysql"}
}
func (fakeDriverForTest) Tables(context.Context) ([]model.TableInfo, error) { return nil, nil }
func (fakeDriverForTest) Schema(context.Context, []string) (map[string]string, []string, error) {
	return nil, nil, nil
}
func (fakeDriverForTest) Query(context.Context, string, int, int) (*model.QueryResult, error) {
	return nil, nil
}
func (fakeDriverForTest) Exec(context.Context, string) (int64, error) { return 0, nil }
func (fakeDriverForTest) Close() error                                { return nil }

func newAdminHandler(t *testing.T) *handler.Handler {
	t.Helper()
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedSuperAdmin(t, st)
	return &handler.Handler{
		Registry:     driver.NewRegistry(),
		Store:        st,
		ServerSecret: testSecret,
		IPWhitelist:  []string{"*"},
	}
}

// postAdmin 以 super_admin session cookie 调用后台 ui 接口。
func postAdmin(h *handler.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.AddCookie(sessionCookie())
	rr := httptest.NewRecorder()
	router.Setup(h).ServeHTTP(rr, req)
	return rr
}

func TestAdmin_DBTest_MissingDSN(t *testing.T) {
	h := newAdminHandler(t)
	rr := postAdmin(h, "/api/v1/admin/ui/databases/test", `{"driver":"mysql"}`)
	if rr.Code != 400 {
		t.Fatalf("want 400 for missing dsn, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdmin_DBTest_UnreachableReturnsOKFalse(t *testing.T) {
	h := newAdminHandler(t)
	// 指向不可达地址:连不上是预期内的探测结果,接口须返回 200 + ok:false,且不入库。
	rr := postAdmin(h, "/api/v1/admin/ui/databases/test",
		`{"name":"t","driver":"mysql","dsn":"u:p@tcp(127.0.0.1:1)/db"}`)
	if rr.Code != 200 {
		t.Fatalf("want 200 (test result, not error), got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"ok":false`) {
		t.Fatalf("want ok:false, got %s", rr.Body.String())
	}
	// 不应写入数据库列表
	list := postAdmin(h, "/api/v1/admin/ui/databases/list", `{}`)
	if strings.Contains(list.Body.String(), `"t"`) {
		t.Fatalf("test must not persist db: %s", list.Body.String())
	}
}

func TestAdmin_RequiresSession(t *testing.T) {
	h := newAdminHandler(t)
	req := httptest.NewRequest("POST", "/api/v1/admin/ui/databases/list", strings.NewReader("{}"))
	// no session cookie
	rr := httptest.NewRecorder()
	router.Setup(h).ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("want 401 without session, got %d", rr.Code)
	}
}

func TestAdmin_SensitiveColumnCRUD(t *testing.T) {
	h := newAdminHandler(t)
	if rr := postAdmin(h, "/api/v1/admin/ui/sensitive-columns/create", `{"db":"crm","column":"mobile"}`); rr.Code != 200 {
		t.Fatalf("create got %d: %s", rr.Code, rr.Body)
	}
	rr := postAdmin(h, "/api/v1/admin/ui/sensitive-columns/list", `{"db":"crm"}`)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "mobile") {
		t.Fatalf("list got %d: %s", rr.Code, rr.Body)
	}
}

func TestAdmin_DeleteSeedDatabaseForbidden(t *testing.T) {
	h := newAdminHandler(t)
	h.Registry.Register("seedprod", fakeDriverForTest{}, driver.SourceSeed)
	rr := postAdmin(h, "/api/v1/admin/ui/databases/delete", `{"name":"seedprod"}`)
	if rr.Code != 403 {
		t.Fatalf("delete seed got %d (want 403): %s", rr.Code, rr.Body)
	}
}

func TestAdmin_QueryLogsList(t *testing.T) {
	h := newAdminHandler(t)
	rr := postAdmin(h, "/api/v1/admin/ui/query-logs/list", `{}`)
	if rr.Code != 200 {
		t.Fatalf("logs list got %d: %s", rr.Code, rr.Body)
	}
}
