package idempotency

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestNewRedisStoreDefaultsPrefix(t *testing.T) {
	store, err := NewRedisStore(&redis.Client{}, RedisOptions{
		ProcessingTTL: time.Minute,
		CompletedTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("NewRedisStore() error = %v", err)
	}
	if store.key("message:1-0") != "idem:message:1-0" {
		t.Fatalf("key = %q, want idem:message:1-0", store.key("message:1-0"))
	}
}

func TestNewRedisStoreRejectsInvalidTTL(t *testing.T) {
	_, err := NewRedisStore(&redis.Client{}, RedisOptions{
		ProcessingTTL: time.Minute,
	})
	if err == nil {
		t.Fatal("NewRedisStore() error = nil, want completed ttl error")
	}
}

func TestRedisStoreKeyKeepsExistingPrefix(t *testing.T) {
	store, err := NewRedisStore(&redis.Client{}, RedisOptions{
		Prefix:        "position:decision:idempotency",
		ProcessingTTL: time.Minute,
		CompletedTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("NewRedisStore() error = %v", err)
	}
	key := "position:decision:idempotency:message:1-0"
	if store.key(key) != key {
		t.Fatalf("key = %q, want %q", store.key(key), key)
	}
}
