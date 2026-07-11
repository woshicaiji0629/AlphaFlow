package controller

import (
	"net/http"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"alphaflow/go-service/control-api/internal/service"
	"github.com/gin-gonic/gin"
)

type Dashboard struct{ service *service.DashboardService }

func NewDashboard(service *service.DashboardService) *Dashboard { return &Dashboard{service: service} }
func (d *Dashboard) Get(c *gin.Context) {
	result, err := d.service.Get(c.Request.Context(), requestcontext.Session(c).User.ID)
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
		return
	}
	c.JSON(http.StatusOK, result)
}
