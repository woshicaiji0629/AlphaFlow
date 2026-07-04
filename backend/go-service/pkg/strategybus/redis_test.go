package strategybus

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/strategy"
	"github.com/redis/go-redis/v9"
)

func TestNewRedisBusDefaultsRetryOptions(t *testing.T) {
	bus, err := NewRedisBus(&redis.Client{}, RedisOptions{
		Group:    "position-engine",
		Consumer: "test",
	})
	if err != nil {
		t.Fatalf("NewRedisBus() error = %v", err)
	}
	if bus.options.PendingIdle <= 0 {
		t.Fatalf("pending idle = %s, want positive", bus.options.PendingIdle)
	}
	if bus.options.DeadLetterStream != DefaultDecisionStream+":dead" {
		t.Fatalf("dead letter stream = %q, want default", bus.options.DeadLetterStream)
	}
	if bus.options.MaxDeliveries != 5 {
		t.Fatalf("max deliveries = %d, want 5", bus.options.MaxDeliveries)
	}
}

func TestDecodeMessagesKeepsDeliveryCount(t *testing.T) {
	payload, err := EncodeDecision(DecisionEnvelope{
		Target: strategy.Target{Symbol: "ETHUSDT"},
	})
	if err != nil {
		t.Fatalf("EncodeDecision() error = %v", err)
	}
	messages, err := decodeMessages([]redis.XMessage{{
		ID: "1-0",
		Values: map[string]any{
			StreamPayloadField: payload,
		},
	}}, map[string]int64{"1-0": 3})
	if err != nil {
		t.Fatalf("decodeMessages() error = %v", err)
	}
	if messages[0].DeliveryCount != 3 {
		t.Fatalf("delivery count = %d, want 3", messages[0].DeliveryCount)
	}
}

func TestNewRedisBusPreservesRetryOptions(t *testing.T) {
	bus, err := NewRedisBus(&redis.Client{}, RedisOptions{
		Group:            "position-engine",
		Consumer:         "test",
		PendingIdle:      time.Minute,
		DeadLetterStream: "dead",
		MaxDeliveries:    9,
	})
	if err != nil {
		t.Fatalf("NewRedisBus() error = %v", err)
	}
	if bus.options.PendingIdle != time.Minute {
		t.Fatalf("pending idle = %s, want 1m", bus.options.PendingIdle)
	}
	if bus.options.DeadLetterStream != "dead" {
		t.Fatalf("dead letter stream = %q, want dead", bus.options.DeadLetterStream)
	}
	if bus.options.MaxDeliveries != 9 {
		t.Fatalf("max deliveries = %d, want 9", bus.options.MaxDeliveries)
	}
}
