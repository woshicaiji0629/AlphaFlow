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
	size := runtime.NumCPU() * 8
	if size < 64 {
		return 64
	}
	if size > 128 {
		return 128
	}
	return size
}

func defaultRedisMinIdleConns() int {
	size := defaultRedisPoolSize() / 2
	if size < 32 {
		return 32
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
