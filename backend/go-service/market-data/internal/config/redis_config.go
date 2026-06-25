package config

import (
	"os"
	"strings"

	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/redisclient"
)

const defaultRedisAddr = "localhost:6380"

func RedisConfigs() map[string]redisclient.Config {
	return map[string]redisclient.Config{
		constants.RedisDefaultInstance: {
			Addr:         redisAddr(),
			Password:     "",
			DB:           0,
			PoolSize:     20,
			MinIdleConns: 5,
		},
	}
}

func redisAddr() string {
	value := strings.TrimSpace(os.Getenv("ALPHAFLOW_REDIS_ADDR"))
	if value == "" {
		return defaultRedisAddr
	}
	return value
}
