package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client    *redis.Client
	retention Retention
}

type Retention struct {
	KlineLimit int64
	LatestTTL  time.Duration
	PollingTTL time.Duration
}

func NewRedisStore(client *redis.Client, retention Retention) *RedisStore {
	return &RedisStore{
		client:    client,
		retention: retention,
	}
}

func (s *RedisStore) LastOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	key := model.RedisKey(exchange, market, symbol, interval)
	values, err := s.client.ZRevRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return 0, false, fmt.Errorf("read latest kline: %w", err)
	}
	if len(values) == 0 {
		return 0, false, nil
	}
	return int64(values[0].Score), true, nil
}

func (s *RedisStore) UpsertKline(ctx context.Context, kline model.Kline) error {
	key := model.RedisKey(kline.Exchange, kline.Market, kline.Symbol, kline.Interval)
	payload, err := json.Marshal(kline)
	if err != nil {
		return fmt.Errorf("marshal kline: %w", err)
	}

	score := strconv.FormatInt(kline.OpenTime, 10)
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, score, score)
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(kline.OpenTime),
		Member: payload,
	})
	pipe.ZRemRangeByRank(ctx, key, 0, -(s.retention.KlineLimit + 1))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("upsert kline: %w", err)
	}
	return nil
}

func (s *RedisStore) SetLastPrice(ctx context.Context, price model.LastPrice) error {
	key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal last price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set last price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetMarkPrice(ctx context.Context, price model.MarkPrice) error {
	key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal mark price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set mark price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetBookTicker(ctx context.Context, ticker model.BookTicker) error {
	key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
	payload, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("marshal book ticker: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set book ticker: %w", err)
	}
	return nil
}

func (s *RedisStore) SetOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
	payload, err := json.Marshal(interest)
	if err != nil {
		return fmt.Errorf("marshal open interest: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.PollingTTL).Err(); err != nil {
		return fmt.Errorf("set open interest: %w", err)
	}
	return nil
}

func (s *RedisStore) AddLiquidation(
	ctx context.Context,
	liquidation model.Liquidation,
	limit int64,
) error {
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
	pipe.ZRemRangeByRank(ctx, key, 0, -(limit + 1))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("add liquidation: %w", err)
	}
	return nil
}
