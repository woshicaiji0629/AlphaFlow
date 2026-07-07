package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/lcache"
	"github.com/redis/go-redis/v9"
)

func (s *RedisStore) SetMarketStatus(ctx context.Context, status model.MarketStatus) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

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
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.WebSocketStatusKey(status.Exchange, status.Market)
	if status.Shard != "" {
		key = model.WebSocketShardStatusKey(status.Exchange, status.Market, status.Shard)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal websocket status: %w", err)
	}
	if s.shouldSkipWebSocketStatusWrite(key, payload) {
		return nil
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set websocket status: %w", err)
	}
	return nil
}

func (s *RedisStore) IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()

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

func (s *RedisStore) SetDataHealth(ctx context.Context, health model.DataHealth) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.DataHealthKey(health.Exchange, health.Market, health.Symbol, health.Interval)
	payload, err := json.Marshal(health)
	if err != nil {
		return fmt.Errorf("marshal data health: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set data health: %w", err)
	}
	return nil
}

func (s *RedisStore) shouldSkipWebSocketStatusWrite(key string, payload []byte) bool {
	return shouldSkipCachedPayloadWrite(s.webSocketStatusCache, key, payload, webSocketStatusCacheTTL)
}

func shouldSkipCachedPayloadWrite(cache *lcache.Cache, key string, payload []byte, exp time.Duration) bool {
	if cache == nil {
		return false
	}
	if cached, ok := cache.Get(key); ok {
		if cachedPayload, ok := cached.(string); ok && cachedPayload == string(payload) {
			return true
		}
	}
	cache.SetEx(key, string(payload), exp)
	return false
}
