package postgres

import (
	"context"
	"fmt"

	"alphaflow/go-service/control-api/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	poolConfig.MaxConns = cfg.Postgres.MaxConns
	poolConfig.MinConns = cfg.Postgres.MinConns

	connectTimeout, err := config.PostgresConnectTimeout(cfg)
	if err != nil {
		return nil, err
	}
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
