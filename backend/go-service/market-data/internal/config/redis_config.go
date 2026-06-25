package config

import (
	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/redisclient"
)

func RedisConfigs() map[string]redisclient.Config {
	return map[string]redisclient.Config{
		constants.RedisDefaultInstance: {
			Addr:         "localhost:6379",
			Password:     "",
			DB:           0,
			PoolSize:     20,
			MinIdleConns: 5,
		},
	}
}
