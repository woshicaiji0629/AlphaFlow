package api

import (
	"alphaflow/go-service/control-api/internal/api/controller"
	"alphaflow/go-service/control-api/internal/service"
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type HealthChecker interface {
	Ping(context.Context) error
}

func NewRouter(mode string, database HealthChecker, authOptions AuthOptions, dashboard *service.DashboardService, catalog *service.StrategyCatalogService, adminStrategies *service.AdminStrategyService, security SecurityOptions) http.Handler {
	gin.SetMode(mode)
	router := gin.New()
	router.Use(requestIDMiddleware(), accessLogMiddleware(), recoveryMiddleware(), securityHeadersMiddleware(), requestBodyLimitMiddleware(security.MaxBodyBytes), originMiddleware(security.AllowedOrigins))
	authController := controller.NewAuth(authOptions.Service, authOptions.CookieName, authOptions.CSRFCookieName, authOptions.SecureCookie)
	auth := router.Group("/api/v1/auth")
	auth.POST("/login", authController.Login)
	auth.GET("/me", sessionMiddleware(authOptions), authController.Me)
	auth.POST("/logout", sessionMiddleware(authOptions), csrfMiddleware(authOptions), authController.Logout)
	healthController := controller.NewHealth(database)
	dashboardController := controller.NewDashboard(dashboard)
	strategyController := controller.NewStrategyCatalog(catalog)
	adminController := controller.NewAdminStrategy(adminStrategies)
	router.GET("/healthz", healthController.Health)
	router.GET("/readyz", healthController.Ready)
	protected := router.Group("/api/v1")
	protected.Use(sessionMiddleware(authOptions))
	protected.GET("/dashboard", dashboardController.Get)
	protected.GET("/strategies", strategyController.List)
	protected.GET("/strategies/:strategyId/performance", strategyController.Performance)
	admin := protected.Group("/admin")
	admin.Use(adminMiddleware())
	admin.GET("/access", adminController.Access)
	admin.GET("/strategy-definitions", adminController.Definitions)
	admin.GET("/strategies", adminController.List)
	admin.POST("/strategies", csrfMiddleware(authOptions), adminController.Create)
	admin.PATCH("/strategies/:strategyId", csrfMiddleware(authOptions), adminController.Update)
	admin.POST("/strategies/:strategyId/versions", csrfMiddleware(authOptions), adminController.CreateVersion)

	router.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "not_found", "接口不存在")
	})
	return router
}
