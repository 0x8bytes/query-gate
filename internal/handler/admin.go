package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/driver/dbfactory"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/apierror"
)

// ---- databases ----

func (h *Handler) AdminDBList(c *gin.Context) {
	recs, err := h.Store.ListDatabases()
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	// 本接口仅 super_admin 可达（路由层 RequireRole 保证），回填编辑用,直接返回 dsn/exec_dsn 明文。
	out := make([]gin.H, 0, len(recs))
	for _, r := range recs {
		out = append(out, gin.H{
			"name": r.Name, "driver": r.Driver, "description": r.Description,
			"dsn": r.DSN, "exec_dsn": r.ExecDSN, "has_exec": r.ExecDSN != "",
		})
	}
	c.JSON(http.StatusOK, gin.H{"databases": out})
}

type adminDBCreateReq struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	DSN         string `json:"dsn"`
	ExecDSN     string `json:"exec_dsn"`
	Description string `json:"description"`
}

// AdminDBTest 仅测试 DSN 能否连通：试连后立即关闭，不入库、不注册。
// 「连不上」是预期内的探测结果而非请求错误，故始终返回 200，
// 用 body 的 ok 字段表达连通与否（缺 dsn 等真正的请求问题才返回 400）。
func (h *Handler) AdminDBTest(c *gin.Context) {
	var req adminDBCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DSN == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "dsn is required"))
		return
	}
	drv, err := dbfactory.OpenDriver(req.Name, req.Driver, req.DSN, req.Description)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	_ = drv.Close()
	// 若提供了 exec_dsn,读连接 OK 后再测写连接。
	if req.ExecDSN != "" {
		edrv, err := dbfactory.OpenDriver(req.Name, req.Driver, req.ExecDSN, req.Description)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "读连接 OK,写连接失败: " + err.Error()})
			return
		}
		_ = edrv.Close()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminDBCreate(c *gin.Context) {
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	var req adminDBCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Name == "" || req.DSN == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "name and dsn are required"))
		return
	}
	if _, exists := h.Registry.Get(req.Name); exists {
		apierror.Write(c.Writer, apierror.BadRequest("duplicate", "database alias already exists"))
		return
	}
	drv, err := dbfactory.OpenDriver(req.Name, req.Driver, req.DSN, req.Description)
	if err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("connect_failed", err.Error()))
		return
	}
	if err := h.Store.UpsertDatabase(model.DatabaseRecord{
		Name: req.Name, Driver: req.Driver, DSN: req.DSN, ExecDSN: req.ExecDSN, Description: req.Description,
	}); err != nil {
		_ = drv.Close()
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	h.Registry.Register(req.Name, drv, driver.SourceDynamic)
	// 若配了 exec_dsn 则建写连接并注册;写连接连不上则回滚(撤销读连接 + 删库记录)。
	if req.ExecDSN != "" && h.ExecRegistry != nil {
		edrv, err := dbfactory.OpenDriver(req.Name, req.Driver, req.ExecDSN, req.Description)
		if err != nil {
			_ = h.Registry.Unregister(req.Name)
			_ = h.Store.DeleteDatabase(req.Name)
			apierror.Write(c.Writer, apierror.BadRequest("exec_connect_failed", err.Error()))
			return
		}
		h.ExecRegistry.Register(req.Name, edrv, driver.SourceDynamic)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type adminDBUpdateReq struct {
	Name        string  `json:"name"`
	DSN         *string `json:"dsn"`
	ExecDSN     *string `json:"exec_dsn"` // nil=不改;空串=清空写连接
	Description *string `json:"description"`
}

func (h *Handler) AdminDBUpdate(c *gin.Context) {
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	var req adminDBUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Name == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "name is required"))
		return
	}
	src, ok := h.Registry.Source(req.Name)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	if src == driver.SourceSeed {
		apierror.Write(c.Writer, &apierror.APIError{
			Status: http.StatusForbidden, Code: "seed_readonly", Message: "YAML seed database is read-only",
		})
		return
	}
	// 取旧记录拿 driver 与默认值。
	recs, err := h.Store.ListDatabases()
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	var old model.DatabaseRecord
	found := false
	for _, r := range recs {
		if r.Name == req.Name {
			old = r
			found = true
			break
		}
	}
	if !found {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	// 覆盖 dsn/description(空 dsn 保留旧值)。
	dsn := old.DSN
	if req.DSN != nil && *req.DSN != "" {
		dsn = *req.DSN
	}
	desc := old.Description
	if req.Description != nil {
		desc = *req.Description
	}
	// 覆盖 exec_dsn(nil=保留旧值,空串=清空写连接)。
	execDSN := old.ExecDSN
	if req.ExecDSN != nil {
		execDSN = *req.ExecDSN
	}
	drv, err := dbfactory.OpenDriver(req.Name, old.Driver, dsn, desc)
	if err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("connect_failed", err.Error()))
		return
	}
	if err := h.Store.UpsertDatabase(model.DatabaseRecord{
		Name: req.Name, Driver: old.Driver, DSN: dsn, ExecDSN: execDSN, Description: desc,
	}); err != nil {
		_ = drv.Close()
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	_ = h.Registry.Unregister(req.Name) // 关闭旧连接
	h.Registry.Register(req.Name, drv, driver.SourceDynamic)
	// 重建写连接:先注销旧的(忽略「不存在」),再按新 execDSN 重建。
	if h.ExecRegistry != nil {
		_ = h.ExecRegistry.Unregister(req.Name)
		if execDSN != "" {
			edrv, err := dbfactory.OpenDriver(req.Name, old.Driver, execDSN, desc)
			if err != nil {
				apierror.Write(c.Writer, apierror.BadRequest("exec_connect_failed", err.Error()))
				return
			}
			h.ExecRegistry.Register(req.Name, edrv, driver.SourceDynamic)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type adminDBDeleteReq struct {
	Name string `json:"name"`
}

func (h *Handler) AdminDBDelete(c *gin.Context) {
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	var req adminDBDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.Name == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "name is required"))
		return
	}
	src, ok := h.Registry.Source(req.Name)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	if src == driver.SourceSeed {
		apierror.Write(c.Writer, &apierror.APIError{
			Status: http.StatusForbidden, Code: "seed_readonly", Message: "YAML seed database is read-only",
		})
		return
	}
	if err := h.Store.DeleteDatabase(req.Name); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	_ = h.Registry.Unregister(req.Name)
	if h.ExecRegistry != nil {
		_ = h.ExecRegistry.Unregister(req.Name)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- sensitive-columns ----

type adminSensListReq struct {
	DB string `json:"db"`
}

func (h *Handler) AdminSensList(c *gin.Context) {
	var req adminSensListReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	cols, err := h.Store.ListSensitiveColumns()
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	out := make([]gin.H, 0, len(cols))
	for _, col := range cols {
		if req.DB != "" && col.DB != req.DB {
			continue
		}
		out = append(out, gin.H{"db": col.DB, "column": col.Column})
	}
	c.JSON(http.StatusOK, gin.H{"sensitive_columns": out})
}

type adminSensReq struct {
	DB     string `json:"db"`
	Column string `json:"column"`
}

func (h *Handler) AdminSensCreate(c *gin.Context) {
	var req adminSensReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" || req.Column == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db and column are required"))
		return
	}
	if err := h.Store.AddSensitiveColumn(req.DB, req.Column); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminSensDelete(c *gin.Context) {
	var req adminSensReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" || req.Column == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db and column are required"))
		return
	}
	if err := h.Store.DeleteSensitiveColumn(req.DB, req.Column); err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- query-logs ----

type adminLogListReq struct {
	DB     string `json:"db"`
	APIKey string `json:"api_key"`
	Limit  int    `json:"limit"`
	Before string `json:"before"`
}

func (h *Handler) AdminLogList(c *gin.Context) {
	var req adminLogListReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	logs, err := h.Store.ListQueryLogs(model.QueryLogFilter{
		DB: req.DB, APIKey: req.APIKey, Before: req.Before, Limit: req.Limit,
	})
	if err != nil {
		apierror.Write(c.Writer, apierror.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"query_logs": logs})
}

// ---- helpers ----

// genKey 生成带 qn_ 前缀的随机 api key(crypto/rand 24 字节 hex)。
func genKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "qn_" + hex.EncodeToString(b)
}
