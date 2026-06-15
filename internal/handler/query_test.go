package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/internal/router"
)

// TestQuery_GuardRejectsStarAndLogs 验证 SELECT * 被 queryguard 拒绝，并写入 denied 日志。
func TestQuery_GuardRejectsStarAndLogs(t *testing.T) {
	h := newAdminHandler(t) // 复用:提供 Store + Registry；IPWhitelist=["*"]
	h.MaxRows = 100
	h.QueryTimeout = 5_000_000_000
	// 注入一个 enabled api-key，用于 /api/v1/query 路由认证
	seedAuthKey(t, h.Store, "k", "tester")
	h.Registry.Register("prod", stubDriver{}, driver.SourceSeed)

	body := `{"db":"prod","query":"SELECT * FROM users"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rr := httptest.NewRecorder()
	router.Setup(h).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("SELECT * 应被拒绝返回 403，got %d: %s", rr.Code, rr.Body)
	}
	logs, err := h.Store.ListQueryLogs(model.QueryLogFilter{Limit: 10})
	if err != nil || len(logs) != 1 || logs[0].Status != "denied" {
		t.Fatalf("期望 1 条 denied 日志，got %+v err=%v", logs, err)
	}
}

type mongoStubDriver struct{}

func (mongoStubDriver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: "mg", Driver: "mongodb"}
}
func (mongoStubDriver) Tables(context.Context) ([]model.TableInfo, error) { return nil, nil }
func (mongoStubDriver) Schema(context.Context, []string) (map[string]string, []string, error) {
	return nil, nil, nil
}
func (mongoStubDriver) Query(context.Context, string, int, int) (*model.QueryResult, error) {
	return &model.QueryResult{Columns: []string{"document"}, Rows: [][]any{{`{"a":1}`}}, RowCount: 1}, nil
}
func (mongoStubDriver) Exec(context.Context, string) (int64, error) { return 0, nil }
func (mongoStubDriver) Close() error                                { return nil }

// TestQuery_MongoNotBlockedBySQLGuard 验证 Mongo JSON 查询不被 SQL guard 误拦（Fix 1）。
func TestQuery_MongoNotBlockedBySQLGuard(t *testing.T) {
	h := newAdminHandler(t)
	h.MaxRows = 100
	h.QueryTimeout = 5_000_000_000
	seedAuthKey(t, h.Store, "k", "tester")
	h.Registry.Register("mg", mongoStubDriver{}, driver.SourceSeed)

	body := `{"db":"mg","query":"{\"collection\":\"orders\",\"find\":{\"status\":\"active\"}}"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rr := httptest.NewRecorder()
	router.Setup(h).ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("mongo query should pass guard, got %d: %s", rr.Code, rr.Body)
	}
}

// TestQuery_SuccessLogsOk 验证合法查询返回 200，并写入 ok 日志（含调用方 key name）。
func TestQuery_SuccessLogsOk(t *testing.T) {
	h := newAdminHandler(t)
	h.MaxRows = 100
	h.QueryTimeout = 5_000_000_000
	seedAuthKey(t, h.Store, "k", "tester")
	h.Registry.Register("prod", stubDriver{}, driver.SourceSeed)

	body := `{"db":"prod","query":"SELECT id FROM users"}`
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	rr := httptest.NewRecorder()
	router.Setup(h).ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("期望 200，got %d: %s", rr.Code, rr.Body)
	}
	logs, _ := h.Store.ListQueryLogs(model.QueryLogFilter{Limit: 10})
	if len(logs) != 1 || logs[0].Status != "ok" || logs[0].APIKeyName != "tester" {
		t.Fatalf("期望 1 条 ok 日志 by tester，got %+v", logs)
	}
}
