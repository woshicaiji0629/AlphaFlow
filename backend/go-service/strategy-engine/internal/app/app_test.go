package app

import (
	"context"
	"reflect"
	"testing"

	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/strategy-engine/internal/config"
	"alphaflow/go-service/strategy-engine/internal/reader"
)

func TestBuildRuntimeUsesConfiguredStrategy(t *testing.T) {
	cfg := config.Config{
		Position: config.PositionConfig{Scope: "paper"},
		Targets: []config.TargetConfig{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		}},
	}
	runtime, err := buildRuntime(cfg, fakeHashReader{}, nil, position.NewMemoryStore(), nil, nil)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if runtime.reader == nil {
		t.Fatal("reader = nil")
	}
	if runtime.runner == nil {
		t.Fatal("runner = nil")
	}
}

func TestBuildRuntimeAcceptsEventStore(t *testing.T) {
	cfg := config.Config{
		Position: config.PositionConfig{Scope: "paper"},
		Targets: []config.TargetConfig{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		}},
	}
	store := position.NewMemoryStore()
	runtime, err := buildRuntime(cfg, fakeHashReader{}, nil, store, store, nil)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if runtime.runner == nil {
		t.Fatal("runner = nil")
	}
}

func TestBuildEventStoreDisabled(t *testing.T) {
	eventStore, closeStore, err := buildEventStore(context.Background(), config.Config{})
	if err != nil {
		t.Fatalf("buildEventStore() error = %v", err)
	}
	if eventStore != nil {
		t.Fatal("event store != nil")
	}
	closeStore()
}

func TestBrokerForScopeOnlyEnablesPaper(t *testing.T) {
	if brokerForScope(strategy.PositionScopePaper) == nil {
		t.Fatal("paper broker = nil")
	}
	if brokerForScope(strategy.PositionScopeBacktest) != nil {
		t.Fatal("backtest broker != nil")
	}
}

func TestMarketSnapshotLogAttrs(t *testing.T) {
	message := marketbus.SnapshotMessage{
		ID: "42",
		Envelope: marketbus.SnapshotEnvelope{
			Type:      marketbus.SnapshotTypeRealtime,
			TraceID:   "trace-1",
			Target:    marketbus.SnapshotTarget{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"},
			CreatedAt: 900,
			ExpiresAt: 1200,
		},
		DeliveryCount: 2,
	}
	want := []any{
		"message_id", "42",
		"snapshot_type", marketbus.SnapshotTypeRealtime,
		"trace_id", "trace-1",
		"target", message.Envelope.Target,
		"lag_ms", int64(100),
		"expires_in_ms", int64(200),
		"delivery_count", int64(2),
	}
	if got := marketSnapshotLogAttrs(message, 1000); !reflect.DeepEqual(got, want) {
		t.Fatalf("marketSnapshotLogAttrs() = %#v, want %#v", got, want)
	}
}

type fakeHashReader struct{}

func (r fakeHashReader) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

var _ reader.HashReader = fakeHashReader{}
