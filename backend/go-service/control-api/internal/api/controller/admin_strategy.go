package controller

import (
	"errors"
	"net/http"
	"time"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/service"
	"alphaflow/go-service/pkg/strategyregistry"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type AdminStrategy struct{ service *service.AdminStrategyService }

func NewAdminStrategy(service *service.AdminStrategyService) *AdminStrategy {
	return &AdminStrategy{service: service}
}
func (a *AdminStrategy) Access(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"role": "admin"}) }
func (a *AdminStrategy) Definitions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"definitions": strategyregistry.Supported()})
}
func (a *AdminStrategy) List(c *gin.Context) {
	items, err := a.service.List(c.Request.Context())
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "strategy_list_failed", "读取策略失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategies": items})
}
func auditEvent(c *gin.Context, eventType string) domain.AuditEvent {
	s := requestcontext.Session(c)
	return domain.AuditEvent{ID: uuid.New(), UserID: &s.User.ID, EventType: eventType, Outcome: "success", IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(), RequestID: c.GetString("request_id"), CreatedAt: time.Now().UTC()}
}
func (a *AdminStrategy) Create(c *gin.Context) {
	var input service.AdminStrategyInput
	if c.ShouldBindJSON(&input) != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "请求参数格式错误")
		return
	}
	created, err := a.service.CreateDraft(c.Request.Context(), input, auditEvent(c, "admin.strategy.create"))
	if err != nil {
		writeStrategyError(c, err, "strategy_create_failed", "创建策略失败")
		return
	}
	c.JSON(http.StatusCreated, created)
}
func (a *AdminStrategy) CreateVersion(c *gin.Context) {
	id, ok := strategyID(c)
	if !ok {
		return
	}
	var input struct {
		Version string `json:"version" binding:"required"`
	}
	if c.ShouldBindJSON(&input) != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "新版本号不能为空")
		return
	}
	created, err := a.service.CreateVersion(c.Request.Context(), id, input.Version, auditEvent(c, "admin.strategy.version.create"))
	if err != nil {
		if errors.Is(err, domain.ErrStrategyNotFound) {
			apiresponse.Error(c, http.StatusNotFound, "strategy_not_found", "源策略不存在")
			return
		}
		var validation service.AdminStrategyValidationError
		if errors.As(err, &validation) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_strategy_version", validation.Error())
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			apiresponse.Error(c, http.StatusConflict, "strategy_version_conflict", "该策略版本已存在")
			return
		}
		apiresponse.Error(c, http.StatusInternalServerError, "strategy_version_create_failed", "创建策略版本失败")
		return
	}
	c.JSON(http.StatusCreated, created)
}
func (a *AdminStrategy) Update(c *gin.Context) {
	id, ok := strategyID(c)
	if !ok {
		return
	}
	var input service.AdminStrategyInput
	if c.ShouldBindJSON(&input) != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "请求参数格式错误")
		return
	}
	updated, err := a.service.UpdateDraft(c.Request.Context(), id, input, auditEvent(c, "admin.strategy.update"))
	if err != nil {
		if errors.Is(err, domain.ErrStrategyNotFound) {
			apiresponse.Error(c, http.StatusNotFound, "strategy_not_found", "策略不存在")
			return
		}
		if errors.Is(err, domain.ErrStrategyNotEditable) {
			apiresponse.Error(c, http.StatusConflict, "strategy_not_editable", "只有草稿策略可以修改")
			return
		}
		var validation service.AdminStrategyValidationError
		if errors.As(err, &validation) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_strategy", validation.Error())
			return
		}
		apiresponse.Error(c, http.StatusInternalServerError, "strategy_update_failed", "更新策略失败")
		return
	}
	c.JSON(http.StatusOK, updated)
}
func strategyID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("strategyId"))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_strategy_id", "策略 ID 格式错误")
		return uuid.Nil, false
	}
	return id, true
}
func writeStrategyError(c *gin.Context, err error, internalCode, internalMessage string) {
	var validation service.AdminStrategyValidationError
	if errors.As(err, &validation) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_strategy", validation.Error())
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		apiresponse.Error(c, http.StatusConflict, "strategy_conflict", "策略代码或版本已存在")
		return
	}
	apiresponse.Error(c, http.StatusInternalServerError, internalCode, internalMessage)
}
