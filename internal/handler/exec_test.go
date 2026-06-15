package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/model"
)

type stubExecDriver struct{}

func (stubExecDriver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: "wdb", Driver: "mysql"}
}
func (stubExecDriver) Tables(_ context.Context) ([]model.TableInfo, error) { return nil, nil }
func (stubExecDriver) Schema(_ context.Context, _ []string) (map[string]string, []string, error) {
	return map[string]string{}, nil, nil
}
func (stubExecDriver) Query(_ context.Context, _ string, _, _ int) (*model.QueryResult, error) {
	return &model.QueryResult{}, nil
}
func (stubExecDriver) Exec(_ context.Context, _ string) (int64, error) { return 7, nil }
func (stubExecDriver) Close() error                                    { return nil }

// errExecDriver 的 Exec 永远失败,用于覆盖 exec 错误路径(审计 + 错误回传)。
type errExecDriver struct{ stubExecDriver }

func (errExecDriver) Exec(_ context.Context, _ string) (int64, error) {
	return 0, errors.New("boom: write failed")
}

func TestExec_SuperAdminOK(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")

	execReg := driver.NewRegistry()
	execReg.Register("wdb", stubExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec", func(c *gin.Context) {
		c.Set("api_key_name", "root")
		c.Set("api_key_role", model.RoleSuperAdmin)
		c.Next()
	}, h.Exec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"wdb","sql":"UPDATE t SET a=1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"rows_affected":7`) {
		t.Fatalf("want rows_affected 7, got %s", rec.Body.String())
	}
}

func TestExec_NormalUserForbidden(t *testing.T) {
	st := openStore(t)
	execReg := driver.NewRegistry()
	execReg.Register("wdb", stubExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec", func(c *gin.Context) {
		c.Set("api_key_name", "bob")
		c.Set("api_key_role", model.RoleUser)
		c.Next()
	}, h.Exec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"wdb","sql":"DELETE FROM t"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestExec_UnknownExecDB(t *testing.T) {
	st := openStore(t)
	execReg := driver.NewRegistry()
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec", func(c *gin.Context) {
		c.Set("api_key_name", "root")
		c.Set("api_key_role", model.RoleSuperAdmin)
		c.Next()
	}, h.Exec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"nope","sql":"DELETE FROM t"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("unknown exec db should not 200, got body %s", rec.Body.String())
	}
}

// TestExec_ExecErrorIsLogged 覆盖 exec 执行失败路径:返回非 200,且审计写入一条 error 记录。
func TestExec_ExecErrorIsLogged(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")

	execReg := driver.NewRegistry()
	execReg.Register("wdb", errExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec", func(c *gin.Context) {
		c.Set("api_key_name", "root")
		c.Set("api_key_role", model.RoleSuperAdmin)
		c.Next()
	}, h.Exec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"wdb","sql":"UPDATE t SET a=1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("exec error should not 200, got body %s", rec.Body.String())
	}

	// 审计:应有一条 status=error、actor=root 的记录。
	logs, err := st.ListQueryLogs(model.QueryLogFilter{DB: "wdb"})
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Status != "error" || logs[0].APIKeyName != "root" {
		t.Fatalf("expected one error log by root, got %+v", logs)
	}
}

// TestExec_MissingFields 覆盖 bad_request:缺 sql 字段返回非 200。
func TestExec_MissingFields(t *testing.T) {
	st := openStore(t)
	execReg := driver.NewRegistry()
	execReg.Register("wdb", stubExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec", func(c *gin.Context) {
		c.Set("api_key_name", "root")
		c.Set("api_key_role", model.RoleSuperAdmin)
		c.Next()
	}, h.Exec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"wdb"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sql should be 400, got %d body %s", rec.Code, rec.Body.String())
	}
}

// TestUIExec_OK 验证后台 UIExec 走 runExec:注入 session current_user,POST 后断言 200 + rows_affected。
func TestUIExec_OK(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")

	execReg := driver.NewRegistry()
	execReg.Register("wdb", stubExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// UIExec 不校验权限(路由层 RequireRole 才校验),仅注入 session 用户名。
	r.POST("/exec", func(c *gin.Context) {
		c.Set("current_user", "root")
		c.Next()
	}, h.UIExec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"wdb","sql":"UPDATE t SET a=1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"rows_affected":7`) {
		t.Fatalf("want rows_affected 7, got %s", rec.Body.String())
	}

	// 审计:actor 应为 session 用户名 root。
	logs, err := st.ListQueryLogs(model.QueryLogFilter{DB: "wdb"})
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 || logs[0].APIKeyName != "root" {
		t.Fatalf("expected one log by root, got %+v", logs)
	}
}

// TestUIExecDatabases 验证下拉接口返回 ExecRegistry 中的库。
func TestUIExecDatabases(t *testing.T) {
	st := openStore(t)
	execReg := driver.NewRegistry()
	execReg.Register("wdb", stubExecDriver{}, driver.SourceDynamic)
	h := &handler.Handler{Store: st, ExecRegistry: execReg, MaxRows: 100}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/exec/databases", h.UIExecDatabases)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec/databases", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"wdb"`) {
		t.Fatalf("want databases to contain wdb, got %s", rec.Body.String())
	}
}
