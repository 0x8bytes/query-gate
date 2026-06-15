package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/pkg/apierror"
)

// UITree 返回左树数据。不传 db → 顶层连接列表；传 db → 该连接的分类+表。
func (h *Handler) UITree(c *gin.Context) {
	var req struct {
		DB string `json:"db"`
	}
	_ = c.ShouldBindJSON(&req) // 空 body 合法（顶层）

	if req.DB == "" {
		dbs := make([]gin.H, 0)
		for _, d := range h.Registry.List() {
			dbs = append(dbs, gin.H{"name": d.Name, "driver": d.Driver, "description": d.Description})
		}
		c.JSON(http.StatusOK, gin.H{"databases": dbs})
		return
	}

	drv, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	tbls, err := drv.Tables(ctx)
	if err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("tables_failed", err.Error()))
		return
	}
	tables := make([]gin.H, 0, len(tbls))
	for _, t := range tbls {
		tables = append(tables, gin.H{"name": t.Name, "comment": t.Comment})
	}
	c.JSON(http.StatusOK, gin.H{
		"categories": []gin.H{
			{"name": "Tables", "tables": tables},
		},
	})
}

// adminLogActor 是后台数据查询在审计日志里的标记主体。
const adminLogActor = "admin"

// UIQuery 执行 admin 后台查询——不走 guard，允许 SELECT *、全字段。
// 与 agent 侧一致写入 query_logs，api_key 标记为 admin，便于在查询日志里看到后台操作。
func (h *Handler) UIQuery(c *gin.Context) {
	var req struct {
		DB  string `json:"db"`
		SQL string `json:"sql"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" || req.SQL == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db and sql are required"))
		return
	}
	drv, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	res, err := drv.Query(ctx, req.SQL, 0, h.MaxRows)
	if err != nil {
		h.logQuery(adminLogActor, req.DB, req.SQL, 0, 0, "error", err.Error())
		apierror.Write(c.Writer, apierror.BadRequest("query_failed", err.Error()))
		return
	}
	h.logQuery(adminLogActor, req.DB, req.SQL, res.RowCount, res.ElapsedMs, "ok", "")
	c.JSON(http.StatusOK, gin.H{
		"columns":   res.Columns,
		"rows":      res.Rows,
		"row_count": res.RowCount,
	})
}

// UIDDL 返回单表建表语句（复用 driver.Schema）。
func (h *Handler) UIDDL(c *gin.Context) {
	var req struct {
		DB    string `json:"db"`
		Table string `json:"table"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" || req.Table == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db and table are required"))
		return
	}
	drv, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	ddl, notFound, err := drv.Schema(ctx, []string{req.Table})
	if err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("ddl_failed", err.Error()))
		return
	}
	if len(notFound) > 0 {
		apierror.Write(c.Writer, apierror.BadRequest("not_found", "table not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ddl": ddl[req.Table]})
}

// UIExec 后台 Exec 执行：仅 super_admin（路由层 RequireRole 已保证），走 exec_dsn。
func (h *Handler) UIExec(c *gin.Context) {
	actor := middleware.CurrentUser(c)
	if actor == "" {
		actor = adminLogActor
	}
	h.runExec(c, actor)
}

// UIExecDatabases 返回配了 exec_dsn 的库列表（供 Exec 页面下拉）。
func (h *Handler) UIExecDatabases(c *gin.Context) {
	dbs := make([]gin.H, 0)
	if h.ExecRegistry != nil {
		for _, d := range h.ExecRegistry.List() {
			dbs = append(dbs, gin.H{"name": d.Name, "driver": d.Driver})
		}
	}
	c.JSON(http.StatusOK, gin.H{"databases": dbs})
}
