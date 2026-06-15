package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/apierror"
)

type execRequest struct {
	DB  string `json:"db"`
	SQL string `json:"sql"`
}

// Exec 处理 POST /api/v1/exec:仅 super_admin,走 exec_dsn 写连接,不加 guard,记审计。
func (h *Handler) Exec(c *gin.Context) {
	if middleware.APIKeyRole(c) != model.RoleSuperAdmin {
		apierror.Write(c.Writer, apierror.Forbidden("exec requires super_admin"))
		return
	}
	actor := middleware.APIKeyName(c)
	h.runExec(c, actor)
}

// runExec 是 API 与后台 UI 共享的 exec 核心。actor 为审计主体。
// 安全契约:本函数【不做】权限校验——调用方必须自行保证仅 super_admin 可达
// (API 侧由 Exec 前置判断,UI 侧由路由层 RequireRole(super_admin) 保证)。
func (h *Handler) runExec(c *gin.Context, actor string) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var req execRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" || req.SQL == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db and sql are required"))
		return
	}
	if h.ExecRegistry == nil {
		apierror.Write(c.Writer, apierror.BadRequest("exec_not_enabled", "no exec connection configured"))
		return
	}
	drv, ok := h.ExecRegistry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.BadRequest("exec_not_enabled", "database has no exec connection (exec_dsn not set)"))
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	start := time.Now()
	affected, err := drv.Exec(ctx, req.SQL)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			h.logQuery(actor, req.DB, req.SQL, 0, elapsed, "error", "exec timeout")
			apierror.Write(c.Writer, apierror.ErrQueryTimeout)
			return
		}
		h.logQuery(actor, req.DB, req.SQL, 0, elapsed, "error", err.Error())
		apierror.Write(c.Writer, apierror.BadRequest("exec_failed", err.Error()))
		return
	}
	h.logQuery(actor, req.DB, req.SQL, int(affected), elapsed, "ok", "")
	c.JSON(http.StatusOK, gin.H{"rows_affected": affected})
}
