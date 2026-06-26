package config

import (
	"os"
	"runtime"
	"strings"

	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/redisclient"
)

const defaultRedisAddr = "127.0.0.1:6380"

func defaultRedisPoolSize() int {
	size := runtime.NumCPU() * 4
	if size < 32 {
		return 32
	}
	if size > 64 {
		return 64
	}
	return size
}

func defaultRedisMinIdleConns() int {
	size := defaultRedisPoolSize() / 4
	if size < 8 {
		return 8
	}
	return size
}

func RedisConfigs() map[string]redisclient.Config {
	return map[string]redisclient.Config{
		constants.RedisDefaultInstance: {
			Addr:         redisAddr(),
			Password:     "",
			DB:           0,
			PoolSize:     defaultRedisPoolSize(),
			MinIdleConns: defaultRedisMinIdleConns(),
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
