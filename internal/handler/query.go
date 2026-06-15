package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	gomysql "github.com/go-sql-driver/mysql"

	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/apierror"
	"github.com/0x8bytes/query-gate/pkg/guard"
)

type queryRequest struct {
	DB    string `json:"db"`
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// Query 处理 POST /api/v1/query:先过 guard，再执行，全程写查询审计日志。
func (h *Handler) Query(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20) // 限制请求体最大 1MB
	var req queryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db is required"))
		return
	}
	keyName := middleware.APIKeyName(c)
	driver, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}

	// 按驱动类型选择 guard:Mongo 用占位 guard,SQL 数据库用完整 SQL guard。
	sensitive := h.sensitiveColumnsFor(req.DB)
	if driver.Info().Driver == "mongodb" {
		if err := guard.CheckMongo(req.Query, sensitive); err != nil {
			h.logQuery(keyName, req.DB, req.Query, 0, 0, "denied", err.Error())
			apierror.Write(c.Writer, apierror.Forbidden(err.Error()))
			return
		}
	} else {
		if err := guard.New(sensitive).Check(req.Query); err != nil {
			h.logQuery(keyName, req.DB, req.Query, 0, 0, "denied", err.Error())
			apierror.Write(c.Writer, apierror.Forbidden(err.Error()))
			return
		}
	}

	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	start := time.Now()
	res, err := driver.Query(ctx, req.Query, req.Limit, h.MaxRows)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			h.logQuery(keyName, req.DB, req.Query, 0, elapsed, "error", "query timeout")
			apierror.Write(c.Writer, apierror.ErrQueryTimeout)
			return
		}
		errno := 0
		var me *gomysql.MySQLError
		if errors.As(err, &me) {
			errno = int(me.Number)
		}
		h.logQuery(keyName, req.DB, req.Query, 0, elapsed, "error", err.Error())
		apierror.Write(c.Writer, apierror.QueryError(err.Error(), errno))
		return
	}
	res.ElapsedMs = elapsed
	h.logQuery(keyName, req.DB, req.Query, res.RowCount, elapsed, "ok", "")
	c.JSON(http.StatusOK, res)
}

// sensitiveColumnsFor 返回某 db 的敏感列名列表。
func (h *Handler) sensitiveColumnsFor(dbName string) []string {
	cols, err := h.Store.ListSensitiveColumnsByDB(dbName)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(cols))
	for _, col := range cols {
		out = append(out, col.Column)
	}
	return out
}

// logQuery 写一条查询审计日志（忽略写入错误，不影响主流程）。
func (h *Handler) logQuery(keyName, dbName, query string, rowCount int, elapsedMs int64, status, errMsg string) {
	_ = h.Store.InsertQueryLog(model.QueryLog{
		TS:         time.Now().UTC().Format(time.RFC3339),
		APIKeyName: keyName,
		DB:         dbName,
		Query:      query,
		RowCount:   rowCount,
		ElapsedMs:  elapsedMs,
		Status:     status,
		Error:      errMsg,
	})
}
