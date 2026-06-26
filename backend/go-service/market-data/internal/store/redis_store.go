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
	KlineLimit     int64
	KlineTTL       time.Duration
	LiquidationTTL time.Duration
	LatestTTL      time.Duration
	PollingTTL     time.Duration
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

func (s *RedisStore) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]model.Kline, error) {
	key := model.RedisKey(exchange, market, symbol, interval)
	values, err := s.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(start, 10),
		Max: strconv.FormatInt(end, 10),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read klines: %w", err)
	}

	klines := make([]model.Kline, 0, len(values))
	for _, value := range values {
		var kline model.Kline
		if err := json.Unmarshal([]byte(value), &kline); err != nil {
			return nil, fmt.Errorf("decode kline: %w", err)
		}
		klines = append(klines, kline)
	}
	return klines, nil
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
	pipe.Expire(ctx, key, s.retention.KlineTTL)
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

func (s *RedisStore) SetLatestBatch(
	ctx context.Context,
	lastPrices []model.LastPrice,
	markPrices []model.MarkPrice,
	bookTickers []model.BookTicker,
) error {
	if len(lastPrices) == 0 && len(markPrices) == 0 && len(bookTickers) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for _, price := range lastPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal last price: %w", err)
		}
		key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
	}
	for _, price := range markPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal mark price: %w", err)
		}
		key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
	}
	for _, ticker := range bookTickers {
		payload, err := json.Marshal(ticker)
		if err != nil {
			return fmt.Errorf("marshal book ticker: %w", err)
		}
		key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set latest batch: %w", err)
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
	pipe.Expire(ctx, key, s.retention.LiquidationTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("add liquidation: %w", err)
	}
	return nil
}

func (s *RedisStore) SetMarketStatus(ctx context.Context, status model.MarketStatus) error {
	key := model.MarketStatusKey(status.Exchange, status.Market)
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal market status: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, 0).Err(); err != nil {
		return fmt.Errorf("set market status: %w", err)
	}
	return nil
}

func (s *RedisStore) SetWebSocketStatus(ctx context.Context, status model.WebSocketStatus) error {
	key := model.WebSocketStatusKey(status.Exchange, status.Market)
	if status.Shard != "" {
		key = model.WebSocketShardStatusKey(status.Exchange, status.Market, status.Shard)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal websocket status: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set websocket status: %w", err)
	}
	return nil
}

func (s *RedisStore) IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error) {
	key := model.MarketStatusKey(exchange, market)
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read market status: %w", err)
	}

	var status model.MarketStatus
	if err := json.Unmarshal([]byte(value), &status); err != nil {
		return false, fmt.Errorf("decode market status: %w", err)
	}
	return status.Available, nil
}

func (s *RedisStore) SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	key := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal indicator: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set indicator: %w", err)
	}
	return nil
}

func (s *RedisStore) LastIndicatorOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	key := model.IndicatorLastKey(exchange, market, symbol, interval)
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read last indicator open time: %w", err)
	}
	openTime, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse last indicator open time: %w", err)
	}
	return openTime, true, nil
}

func (s *RedisStore) MarkIndicatorOpenTime(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	key := model.IndicatorLastKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	value := strconv.FormatInt(snapshot.OpenTime, 10)
	if err := s.client.Set(ctx, key, value, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set last indicator open time: %w", err)
	}
	return nil
}
