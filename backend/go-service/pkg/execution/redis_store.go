package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

type RedisIntentStore struct {
	client *redis.Client
	prefix string
}

func NewRedisIntentStore(client *redis.Client, prefix string) *RedisIntentStore {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "execution:intent"
	}
	return &RedisIntentStore{client: client, prefix: prefix}
}

func (s *RedisIntentStore) GetIntent(ctx context.Context, intentID string) (*IntentRecord, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	payload, err := s.client.Get(ctx, s.key(intentID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get execution intent %s: %w", intentID, err)
	}
	var record IntentRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, fmt.Errorf("decode execution intent %s: %w", intentID, err)
	}
	return &record, nil
}

func (s *RedisIntentStore) SaveIntent(ctx context.Context, record IntentRecord) error {
	if s == nil || s.client == nil {
		return nil
	}
	if strings.TrimSpace(record.Intent.IntentID) == "" {
		return fmt.Errorf("intent_id is required")
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode execution intent %s: %w", record.Intent.IntentID, err)
	}
	if err := s.client.Set(ctx, s.key(record.Intent.IntentID), payload, 0).Err(); err != nil {
		return fmt.Errorf("save execution intent %s: %w", record.Intent.IntentID, err)
	}
	return nil
}

func (s *RedisIntentStore) key(intentID string) string {
	return s.prefix + ":" + intentID
}
