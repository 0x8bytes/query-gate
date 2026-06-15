package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/pkg/apierror"
)

type tablesRequest struct {
	DB string `json:"db"`
}

func (h *Handler) Tables(c *gin.Context) {
	var req tablesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db is required"))
		return
	}
	driver, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	tables, err := driver.Tables(ctx)
	if err != nil {
		apierror.Write(c.Writer, apierror.QueryError(err.Error(), 0))
		return
	}
	c.JSON(http.StatusOK, gin.H{"db": req.DB, "tables": tables})
}
