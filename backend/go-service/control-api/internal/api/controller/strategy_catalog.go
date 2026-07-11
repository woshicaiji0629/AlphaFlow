package controller

import (
	"errors"
	"net/http"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type StrategyCatalog struct {
	service *service.StrategyCatalogService
}

func NewStrategyCatalog(service *service.StrategyCatalogService) *StrategyCatalog {
	return &StrategyCatalog{service: service}
}
func (s *StrategyCatalog) List(c *gin.Context) {
	items, err := s.service.List(c.Request.Context(), requestcontext.Session(c).User)
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategies": items})
}
func (s *StrategyCatalog) Performance(c *gin.Context) {
	id, err := uuid.Parse(c.Param("strategyId"))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_strategy", "策略ID无效")
		return
	}
	items, err := s.service.Performance(c.Request.Context(), requestcontext.Session(c).User, id)
	if errors.Is(err, domain.ErrForbidden) {
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "无权查看该策略")
		return
	}
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
		return
	}
	c.JSON(http.StatusOK, gin.H{"performance": items})
}
