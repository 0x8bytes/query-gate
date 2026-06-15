package handler

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// indexData 是根路径自描述页的数据:agent 打开即可知道
// (1) 有哪些数据库 (2) 怎么调接口 (3) 当前域名。
// Accept: application/json 时直接作为结构化契约返回。
type indexData struct {
	Service   string       `json:"service"`
	BaseURL   string       `json:"base_url"`
	Auth      indexAuth    `json:"auth"`
	Databases []indexDB    `json:"databases"`
	Endpoints []indexRoute `json:"endpoints"`
	Allowed   []string     `json:"allowed"`
	Rejected  []string     `json:"rejected"`
	Flow      []string     `json:"recommended_flow"`
}

type indexAuth struct {
	Type        string   `json:"type"`
	Header      string   `json:"header"`
	PublicPaths []string `json:"public_paths"`
}

type indexDB struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Description string `json:"description"`
}

type indexRoute struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// 页面静态展示内容(接口清单、放行/拒绝规则、推荐流程)。接口变动改这里。
var (
	staticAuth = indexAuth{
		Type:        "api_key",
		Header:      "X-API-Key",
		PublicPaths: []string{"/", "/health"},
	}
	staticEndpoints = []indexRoute{
		{"POST", "/api/v1/databases/list", "List queryable databases. Body: {}."},
		{"POST", "/api/v1/tables/list", `List tables/collections. Body: {"db":"<name>"}.`},
		{"POST", "/api/v1/tables/detail", `Per-table DDL or sampled schema (max 20). Body: {"db":"<name>","names":["t1","t2"]}.`},
		{"POST", "/api/v1/query", `Run read-only query. Body: {"db","query","limit?"}. SELECT * is forbidden; list columns explicitly. Mongo query is JSON {"collection","pipeline"|"find"}.`},
		{"POST", "/api/v1/exec", `Run write op (super_admin key only). Body: {"db","sql"}. Routes to exec_dsn (write account). No guard. Returns rows_affected.`},
	}
	staticAllowed  = []string{"SELECT", "SHOW", "DESCRIBE", "EXPLAIN"}
	staticRejected = []string{"writes/DDL/grants", "multi-statement", "INTO OUTFILE", "EXPLAIN ANALYZE", "FOR UPDATE", "SLEEP/BENCHMARK/LOAD_FILE"}
	staticFlow     = []string{"POST /api/v1/databases/list", "POST /api/v1/tables/detail", "POST /api/v1/query"}
)

// Index 渲染根路径 / 的公开自描述页(无需认证),模板在 internal/view/index.html。
// Accept: application/json 时返回结构化契约(给 agent/程序)。
func (h *Handler) Index(c *gin.Context) {
	dbs := make([]indexDB, 0)
	for _, d := range h.Registry.List() {
		dbs = append(dbs, indexDB{Name: d.Name, Driver: d.Driver, Description: d.Description})
	}

	data := indexData{
		Service:   "querygate",
		BaseURL:   baseURL(c.Request),
		Auth:      staticAuth,
		Databases: dbs,
		Endpoints: staticEndpoints,
		Allowed:   staticAllowed,
		Rejected:  staticRejected,
		Flow:      staticFlow,
	}

	if wantsJSON(c.Request) {
		c.JSON(http.StatusOK, data)
		return
	}

	contract, _ := json.MarshalIndent(data, "", "  ")
	c.HTML(http.StatusOK, "index.html", gin.H{
		"BaseURL":   data.BaseURL,
		"Databases": dbs,
		"ExampleDB": exampleDB(dbs),
		"LoggedIn":  h.loggedIn(c),         // 右上角入口按登录态切换
		"Contract":  template.JS(contract), // 内嵌机器可读 JSON,不转义
	})
}

// loggedIn 轻量判断请求是否带有效 session（仅验签，不查库）——
// 用于首页右上角入口在「用户登录」与「用户中心」之间切换。
func (h *Handler) loggedIn(c *gin.Context) bool {
	cookie, err := c.Request.Cookie(middleware.SessionCookie)
	if err != nil {
		return false
	}
	_, _, ok := auth.ParseSession(h.ServerSecret, cookie.Value)
	return ok
}

// exampleDB 返回示例用的库别名:有真实库则用第一个,否则用占位。
func exampleDB(dbs []indexDB) string {
	if len(dbs) > 0 {
		return dbs[0].Name
	}
	return "<db>"
}

// baseURL 根据请求推断当前服务的对外地址(Railway 代理用 X-Forwarded-* 传递)。
func baseURL(r *http.Request) string {
	scheme := "http"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// wantsJSON 判断调用方是否更想要 JSON(agent/程序)而非 HTML。
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
