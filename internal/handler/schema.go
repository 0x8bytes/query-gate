package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/pkg/apierror"
)

type schemaRequest struct {
	DB    string   `json:"db"`
	Names []string `json:"names"`
}

func (h *Handler) Schema(c *gin.Context) {
	var req schemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "invalid JSON body"))
		return
	}
	if req.DB == "" {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "db is required"))
		return
	}
	if len(req.Names) == 0 {
		apierror.Write(c.Writer, apierror.BadRequest("bad_request", "names is required"))
		return
	}
	if len(req.Names) > maxSchemaTables {
		apierror.Write(c.Writer, apierror.BadRequest("too_many_tables", "at most 20 tables"))
		return
	}
	driver, ok := h.Registry.Get(req.DB)
	if !ok {
		apierror.Write(c.Writer, apierror.ErrUnknownDB)
		return
	}
	ctx, cancel := contextWithTimeout(c.Request.Context(), h.QueryTimeout)
	defer cancel()
	ddl, notFound, err := driver.Schema(ctx, req.Names)
	if err != nil {
		apierror.Write(c.Writer, apierror.QueryError(err.Error(), 0))
		return
	}
	c.JSON(http.StatusOK, gin.H{"db": req.DB, "tables": ddl, "not_found": notFound})
}
