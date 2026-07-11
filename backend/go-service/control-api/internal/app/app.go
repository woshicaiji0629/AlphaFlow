package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"alphaflow/go-service/control-api/internal/api"
	"alphaflow/go-service/control-api/internal/config"
	passwordinfra "alphaflow/go-service/control-api/internal/infrastructure/password"
	controlpostgres "alphaflow/go-service/control-api/internal/infrastructure/postgres"
	"alphaflow/go-service/control-api/internal/infrastructure/postgres/migrations"
	redisinfra "alphaflow/go-service/control-api/internal/infrastructure/redis"
	"alphaflow/go-service/control-api/internal/service"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/redisclient"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	readTimeout, err := config.ReadTimeout(cfg)
	if err != nil {
		return err
	}
	writeTimeout, err := config.WriteTimeout(cfg)
	if err != nil {
		return err
	}
	shutdownTimeout, err := config.ShutdownTimeout(cfg)
	if err != nil {
		return err
	}
	pool, err := controlpostgres.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.Postgres.AutoMigrate {
		migrationTimeout, err := config.MigrationTimeout(cfg)
		if err != nil {
			return err
		}
		migrationCtx, cancel := context.WithTimeout(ctx, migrationTimeout)
		defer cancel()
		if err := migrations.Run(migrationCtx, pool); err != nil {
			return fmt.Errorf("run postgres migrations: %w", err)
		}
	}
	idleTimeout, err := config.SessionIdleTimeout(cfg)
	if err != nil {
		return err
	}
	absoluteTimeout, err := config.SessionAbsoluteTimeout(cfg)
	if err != nil {
		return err
	}
	refreshInterval, err := config.SessionRefreshInterval(cfg)
	if err != nil {
		return err
	}
	redisClient, err := redisclient.New(ctx, redisclient.Config{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize, MinIdleConns: cfg.Redis.MinIdleConns})
	if err != nil {
		return err
	}
	defer redisClient.Close()
	limitWindow, err := config.LoginLimitWindow(cfg)
	if err != nil {
		return err
	}
	limiter, err := redisinfra.NewLoginLimiter(redisClient, limitWindow, cfg.LoginLimit.MaxEmailAttempts, cfg.LoginLimit.MaxIPAttempts)
	if err != nil {
		return err
	}
	auditStore := controlpostgres.NewAuditStore(pool)
	authService, err := service.NewAuthService(controlpostgres.NewSessionStore(pool), passwordinfra.Hasher{}, limiter, auditStore, idleTimeout, absoluteTimeout, refreshInterval)
	if err != nil {
		return err
	}
	dashboardService := service.NewDashboardService(controlpostgres.NewTradingAccountStore(pool), redisinfra.NewPositionReader(position.NewRedisStore(redisClient, position.RedisStoreOptions{})))
	strategyStore := controlpostgres.NewStrategyCatalogStore(pool)
	catalogService := service.NewStrategyCatalogService(strategyStore)
	adminStrategyService := service.NewAdminStrategyService(strategyStore)

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: api.NewRouter(cfg.HTTP.Mode, pool, api.AuthOptions{
			Service: authService, CookieName: cfg.Session.CookieName,
			CSRFCookieName: cfg.Session.CSRFCookieName, SecureCookie: cfg.Session.SecureCookie,
		}, dashboardService, catalogService, adminStrategyService, api.SecurityOptions{AllowedOrigins: cfg.HTTP.AllowedOrigins, MaxBodyBytes: cfg.HTTP.MaxBodyBytes}),
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("control-api listening", "addr", cfg.HTTP.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown control-api: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve control-api: %w", err)
	}
}
