package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Databases(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"databases": h.Registry.List()})
}
