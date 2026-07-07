package store

import (
	"context"
	"encoding/json"
	"fmt"

	"alphaflow/go-service/market-data/internal/model"
	"github.com/redis/go-redis/v9"
)

func (s *RedisStore) AddLiquidation(
	ctx context.Context,
	liquidation model.Liquidation,
	limit int64,
) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.LiquidationKey(liquidation.Exchange, liquidation.Market, liquidation.Symbol)
	payload, err := json.Marshal(liquidation)
	if err != nil {
		return fmt.Errorf("marshal liquidation: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(liquidation.TradeTime),
		Member: payload,
	})
	s.maintainLiquidationKey(key, func() {
		pipe.ZRemRangeByRank(ctx, key, 0, -(limit + 1))
		pipe.Expire(ctx, key, s.retention.LiquidationTTL)
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("add liquidation: %w", err)
	}
	return nil
}

func (s *RedisStore) maintainLiquidationKey(key string, fn func()) {
	if s.liquidationMaintenance == nil {
		fn()
		return
	}
	s.liquidationMaintenance.FreqCall(key, liquidationMaintenanceInterval, fn)
}
