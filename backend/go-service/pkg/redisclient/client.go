package redisclient

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
}

func New(ctx context.Context, cfg Config) (*redis.Client, error) {
	options := &redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     positiveOrDefault(cfg.PoolSize, 20),
		MinIdleConns: positiveOrDefault(cfg.MinIdleConns, 5),
	}

	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

func Close(client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}

func positiveOrDefault(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
