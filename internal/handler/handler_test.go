package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/internal/router"
)

type stubDriver struct{}

func (stubDriver) Info() model.DatabaseInfo { return model.DatabaseInfo{Name: "prod", Driver: "mysql"} }
func (stubDriver) Tables(context.Context) ([]model.TableInfo, error) {
	return []model.TableInfo{{Name: "orders", Comment: "订单表"}}, nil
}
func (stubDriver) Schema(_ context.Context, tables []string) (map[string]string, []string, error) {
	return map[string]string{"orders": "CREATE TABLE `orders` (...)"}, nil, nil
}
func (stubDriver) Query(_ context.Context, _ string, _, _ int) (*model.QueryResult, error) {
	return &model.QueryResult{Columns: []string{"id"}, Rows: [][]any{{1}}, RowCount: 1}, nil
}
func (stubDriver) Exec(context.Context, string) (int64, error) { return 0, nil }
func (stubDriver) Close() error                                { return nil }

// seedAuthKey 往 store 写入一个 enabled 用户(api_key=key),供 /api/v1 路由认证用。
func seedAuthKey(t *testing.T, st *data.Store, key, name string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.CreateUser(model.User{
		Username: name, PasswordHash: "x", APIKey: key,
		Role: model.RoleUser, Status: "enabled", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
}

func newTestStore(t *testing.T) *data.Store {
	t.Helper()
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedAuthKey(t, st, "k", "test") // 默认注入 key "k",测试用 X-API-Key: k 认证
	return st
}

func newTestHandler(t *testing.T) http.Handler {
	reg := driver.NewRegistry()
	reg.Register("prod", stubDriver{}, driver.SourceSeed)
	h := &handler.Handler{
		Registry:     reg,
		Store:        newTestStore(t),
		QueryTimeout: 5 * time.Second,
		MaxRows:      10000,
		IPWhitelist:  []string{"*"},
	}
	return router.Setup(h)
}

type errDriver struct{ err error }

func (errDriver) Info() model.DatabaseInfo                          { return model.DatabaseInfo{Name: "prod", Driver: "mysql"} }
func (errDriver) Tables(context.Context) ([]model.TableInfo, error) { return nil, nil }
func (errDriver) Schema(context.Context, []string) (map[string]string, []string, error) {
	return nil, nil, nil
}
func (e errDriver) Query(context.Context, string, int, int) (*model.QueryResult, error) {
	return nil, e.err
}
func (e errDriver) Exec(context.Context, string) (int64, error) { return 0, e.err }
func (errDriver) Close() error                                  { return nil }

func newErrTestHandler(t *testing.T, err error) http.Handler {
	reg := driver.NewRegistry()
	reg.Register("prod", errDriver{err: err}, driver.SourceSeed)
	h := &handler.Handler{
		Registry:     reg,
		Store:        newTestStore(t),
		QueryTimeout: 5 * time.Second,
		MaxRows:      10000,
		IPWhitelist:  []string{"*"},
	}
	return router.Setup(h)
}

func TestHealth_NoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health code = %d", rec.Code)
	}
}

func TestDatabases_RequiresKey(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/databases/list", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestQuery_RejectsWriteSQL(t *testing.T) {
	body := `{"db":"prod","query":"DELETE FROM orders"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for write sql, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestQuery_Success(t *testing.T) {
	body := `{"db":"prod","query":"SELECT id FROM orders"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body)
	}
	var res model.QueryResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.RowCount != 1 {
		t.Fatalf("row_count = %d", res.RowCount)
	}
}

func TestQuery_Timeout(t *testing.T) {
	body := `{"db":"prod","query":"SELECT id FROM orders"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newErrTestHandler(t, context.DeadlineExceeded).ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 504, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestQuery_DBError(t *testing.T) {
	body := `{"db":"prod","query":"SELECT id FROM orders"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newErrTestHandler(t, errors.New("Error 1146: Table x doesn't exist")).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d (%s)", rec.Code, rec.Body)
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "query_error" {
		t.Fatalf("error = %q, want query_error", resp.Error)
	}
}

func TestQuery_BadJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader("{not json"))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestSchema_RequiresTable(t *testing.T) {
	body := `{"db":"prod"}`
	req := httptest.NewRequest("POST", "/api/v1/tables/detail", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 when names missing, got %d", rec.Code)
	}
}

func TestTables_ReturnsNameAndComment(t *testing.T) {
	body := `{"db":"prod"}`
	req := httptest.NewRequest("POST", "/api/v1/tables/list", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var body2 struct {
		Tables []struct {
			Name    string `json:"name"`
			Comment string `json:"comment"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if len(body2.Tables) != 1 || body2.Tables[0].Name != "orders" || body2.Tables[0].Comment != "订单表" {
		t.Errorf("tables = %+v, want [{orders 订单表}]", body2.Tables)
	}
}

// TestTables_RequiresDB 校验：请求体缺少 db 字段时，返回 400（bad_request），
// 而非 404（unknown_database），让调用方清楚是参数遗漏而非 db 别名错误。
func TestTables_RequiresDB(t *testing.T) {
	body := `{}`
	req := httptest.NewRequest("POST", "/api/v1/tables/list", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 db 字段期望 400，got %d (%s)", rec.Code, rec.Body)
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "bad_request" {
		t.Fatalf("error code = %q，want bad_request", resp.Error)
	}
}

func TestUnknownDB(t *testing.T) {
	body := `{"db":"nope"}`
	req := httptest.NewRequest("POST", "/api/v1/tables/list", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestIndex_PublicHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index code = %d, want 200 (无需认证)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "querygate") {
		t.Error("index 页应包含服务名 querygate")
	}
	if !strings.Contains(body, `id="api-contract"`) {
		t.Error("index 页应内嵌机器可读的 api-contract JSON（给 AI 读）")
	}
}

func TestIndex_OnlyExactRoot(t *testing.T) {
	// 未知路径不应命中 index（确认 /{$} 精确匹配，不是兜底吞掉一切）
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/nonexistent", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("未知路径 /nonexistent 不应返回 200（index 不该兜底），got %d", rec.Code)
	}
}

func TestIndex_JSONViewListsRealDatabases(t *testing.T) {
	// agent 用 Accept: application/json 拿结构化自描述
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var body struct {
		BaseURL   string `json:"base_url"`
		Databases []struct {
			Name string `json:"name"`
		} `json:"databases"`
		Endpoints []struct {
			Path string `json:"path"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if body.BaseURL == "" {
		t.Error("应包含 base_url（当前域名）")
	}
	// stubDriver 注册的别名是 prod，应被动态列出
	found := false
	for _, d := range body.Databases {
		if d.Name == "prod" {
			found = true
		}
	}
	if !found {
		t.Errorf("databases 应动态列出真实库 prod，got %+v", body.Databases)
	}
	if len(body.Endpoints) == 0 {
		t.Error("应列出 endpoints")
	}
}
