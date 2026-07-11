package controller

import (
	"context"
	"net/http"
	"time"

	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"github.com/gin-gonic/gin"
)

type HealthChecker interface{ Ping(context.Context) error }
type Health struct{ database HealthChecker }

func NewHealth(database HealthChecker) *Health { return &Health{database: database} }
func (h *Health) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "control-api", "time": time.Now().UTC()})
}
func (h *Health) Ready(c *gin.Context) {
	if err := h.database.Ping(c.Request.Context()); err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "not_ready", "服务依赖尚未就绪")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
