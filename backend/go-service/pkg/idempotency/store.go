package idempotency

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	StatusStarted    Status = "started"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
)

const (
	processingValue = "processing"
	completedValue  = "completed"
)

type Status string

type Store interface {
	Begin(ctx context.Context, key string) (Status, error)
	Complete(ctx context.Context, key string) error
	Fail(ctx context.Context, key string) error
}

type RedisOptions struct {
	Prefix        string
	ProcessingTTL time.Duration
	CompletedTTL  time.Duration
}

type RedisStore struct {
	client        *redis.Client
	prefix        string
	processingTTL time.Duration
	completedTTL  time.Duration
}

func NewRedisStore(client *redis.Client, options RedisOptions) (*RedisStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	options.Prefix = strings.TrimSpace(options.Prefix)
	if options.Prefix == "" {
		options.Prefix = "idem"
	}
	if options.ProcessingTTL <= 0 {
		return nil, fmt.Errorf("idempotency processing ttl must be positive")
	}
	if options.CompletedTTL <= 0 {
		return nil, fmt.Errorf("idempotency completed ttl must be positive")
	}
	return &RedisStore{
		client:        client,
		prefix:        options.Prefix,
		processingTTL: options.ProcessingTTL,
		completedTTL:  options.CompletedTTL,
	}, nil
}

func (s *RedisStore) Begin(ctx context.Context, key string) (Status, error) {
	key = s.key(key)
	started, err := s.client.SetNX(ctx, key, processingValue, s.processingTTL).Result()
	if err != nil {
		return "", fmt.Errorf("begin idempotency key %s: %w", key, err)
	}
	if started {
		return StatusStarted, nil
	}
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return StatusProcessing, nil
	}
	if err != nil {
		return "", fmt.Errorf("read idempotency key %s: %w", key, err)
	}
	switch value {
	case completedValue:
		return StatusCompleted, nil
	default:
		return StatusProcessing, nil
	}
}

func (s *RedisStore) Complete(ctx context.Context, key string) error {
	key = s.key(key)
	if err := s.client.Set(ctx, key, completedValue, s.completedTTL).Err(); err != nil {
		return fmt.Errorf("complete idempotency key %s: %w", key, err)
	}
	return nil
}

func (s *RedisStore) Fail(ctx context.Context, key string) error {
	key = s.key(key)
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("release idempotency key %s: %w", key, err)
	}
	return nil
}

func (s *RedisStore) key(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, s.prefix+":") {
		return key
	}
	return s.prefix + ":" + key
}
