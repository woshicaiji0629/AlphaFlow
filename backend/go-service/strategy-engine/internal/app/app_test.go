package app

import (
	"context"
	"testing"

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
	runtime, err := buildRuntime(cfg, fakeHashReader{}, position.NewMemoryStore(), nil, nil)
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
	runtime, err := buildRuntime(cfg, fakeHashReader{}, store, store, nil)
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

type fakeHashReader struct{}

func (r fakeHashReader) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

var _ reader.HashReader = fakeHashReader{}
