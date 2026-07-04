package position

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"alphaflow/go-service/pkg/strategy"
	"github.com/redis/go-redis/v9"
)

type RedisStoreOptions struct {
	BacktestTTL time.Duration
}

type RedisStore struct {
	client  *redis.Client
	options RedisStoreOptions
}

func NewRedisStore(client *redis.Client, options RedisStoreOptions) *RedisStore {
	return &RedisStore{
		client:  client,
		options: options,
	}
}

func (s *RedisStore) GetPosition(ctx context.Context, key Key) (*strategy.Position, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	positionKey, err := RedisKey(key)
	if err != nil {
		return nil, err
	}
	payload, err := s.client.Get(ctx, positionKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get redis position: %w", err)
	}
	currentPosition, err := decodePosition(payload)
	if err != nil {
		return nil, err
	}
	return &currentPosition, nil
}

func (s *RedisStore) SavePosition(ctx context.Context, currentPosition strategy.Position) error {
	if s == nil || s.client == nil {
		return nil
	}
	positionKey, err := RedisKey(KeyFromPosition(currentPosition))
	if err != nil {
		return err
	}
	payload, err := encodePosition(currentPosition)
	if err != nil {
		return err
	}
	ttl := s.positionTTL(currentPosition.Scope)
	if currentPosition.Scope != strategy.PositionScopeBacktest {
		if err := s.client.Set(ctx, positionKey, payload, ttl).Err(); err != nil {
			return fmt.Errorf("save redis position: %w", err)
		}
		return nil
	}
	if currentPosition.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	registryKey, err := BacktestTempKeysKey(currentPosition.RunID)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.Set(ctx, positionKey, payload, ttl)
	pipe.SAdd(ctx, registryKey, positionKey)
	if ttl > 0 {
		pipe.Expire(ctx, registryKey, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("save redis backtest position: %w", err)
	}
	return nil
}

func (s *RedisStore) DeletePosition(ctx context.Context, key Key) error {
	if s == nil || s.client == nil {
		return nil
	}
	positionKey, err := RedisKey(key)
	if err != nil {
		return err
	}
	if err := s.client.Del(ctx, positionKey).Err(); err != nil {
		return fmt.Errorf("delete redis position: %w", err)
	}
	return nil
}

func (s *RedisStore) RegisterTempKey(ctx context.Context, runID string, key string) error {
	if s == nil || s.client == nil {
		return nil
	}
	registryKey, err := BacktestTempKeysKey(runID)
	if err != nil {
		return err
	}
	ttl := s.positionTTL(strategy.PositionScopeBacktest)
	pipe := s.client.Pipeline()
	pipe.SAdd(ctx, registryKey, key)
	if ttl > 0 {
		pipe.Expire(ctx, registryKey, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("register redis temp key: %w", err)
	}
	return nil
}

func (s *RedisStore) CleanupTempKeys(ctx context.Context, runID string) error {
	if s == nil || s.client == nil {
		return nil
	}
	registryKey, err := BacktestTempKeysKey(runID)
	if err != nil {
		return err
	}
	keys, err := s.client.SMembers(ctx, registryKey).Result()
	if err != nil {
		return fmt.Errorf("read redis temp keys: %w", err)
	}
	if len(keys) == 0 {
		if err := s.client.Del(ctx, registryKey).Err(); err != nil {
			return fmt.Errorf("delete redis temp registry: %w", err)
		}
		return nil
	}
	keys = append(keys, registryKey)
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("cleanup redis temp keys: %w", err)
	}
	return nil
}

func (s *RedisStore) positionTTL(scope strategy.PositionScope) time.Duration {
	if scope != strategy.PositionScopeBacktest {
		return 0
	}
	return s.options.BacktestTTL
}

func encodePosition(currentPosition strategy.Position) ([]byte, error) {
	payload, err := json.Marshal(currentPosition)
	if err != nil {
		return nil, fmt.Errorf("marshal position: %w", err)
	}
	return payload, nil
}

func decodePosition(payload []byte) (strategy.Position, error) {
	var currentPosition strategy.Position
	if err := json.Unmarshal(payload, &currentPosition); err != nil {
		return strategy.Position{}, fmt.Errorf("decode position: %w", err)
	}
	return currentPosition, nil
}
