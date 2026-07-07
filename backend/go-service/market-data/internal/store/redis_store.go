package store

import (
	"context"
	"runtime"
	"time"

	"alphaflow/go-service/pkg/lcache"
	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client                 *redis.Client
	retention              Retention
	ops                    chan struct{}
	klineMaintenance       *lcache.Cache
	indicatorMaintenance   *lcache.Cache
	liquidationMaintenance *lcache.Cache
	webSocketStatusCache   *lcache.Cache
}

const (
	klineMaintenanceInterval = time.Minute
	klineMaintenanceMaxKeys  = 20000

	indicatorMaintenanceInterval = time.Minute
	indicatorMaintenanceMaxKeys  = 20000

	liquidationMaintenanceInterval = time.Minute
	liquidationMaintenanceMaxKeys  = 20000
	webSocketStatusCacheMaxKeys    = 5000
	webSocketStatusCacheTTL        = 30 * time.Second
)

type Retention struct {
	KlineLimit     int64
	KlineTTL       time.Duration
	IndicatorLimit int64
	LiquidationTTL time.Duration
	LatestTTL      time.Duration
	PollingTTL     time.Duration
}

func NewRedisStore(client *redis.Client, retention Retention) *RedisStore {
	return &RedisStore{
		client:                 client,
		retention:              retention,
		ops:                    make(chan struct{}, redisOperationLimit()),
		klineMaintenance:       lcache.MustNew(klineMaintenanceMaxKeys),
		indicatorMaintenance:   lcache.MustNew(indicatorMaintenanceMaxKeys),
		liquidationMaintenance: lcache.MustNew(liquidationMaintenanceMaxKeys),
		webSocketStatusCache:   lcache.MustNew(webSocketStatusCacheMaxKeys),
	}
}

func redisOperationLimit() int {
	limit := runtime.NumCPU() * 4
	if limit < 32 {
		return 32
	}
	if limit > 96 {
		return 96
	}
	return limit
}

func (s *RedisStore) acquire(ctx context.Context) (func(), error) {
	if s.ops == nil {
		return func() {}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.ops <- struct{}{}:
		return func() { <-s.ops }, nil
	}
}
