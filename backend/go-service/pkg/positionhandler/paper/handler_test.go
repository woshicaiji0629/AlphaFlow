package paper

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
)

func TestHandlerOpensPaperPositionAndAppendsEvents(t *testing.T) {
	store := position.NewMemoryStore()
	handler, err := New(Options{
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("101", func() int64 { return 2000 }),
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	input := strategy.Context{
		Target: strategy.Target{
			Scope:    strategy.PositionScopePaper,
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		},
		Snapshots: map[string]strategy.Snapshot{
			"3m": {Current: marketmodel.Kline{Close: "100"}},
		},
		Positions: map[string]*strategy.Position{"supertrend": nil},
	}
	result := strategy.Result{
		StrategyName: "supertrend",
		Signal: strategy.Signal{
			Strategy:   "supertrend",
			Side:       strategy.SignalSideBuy,
			Confidence: 0.9,
			Reason:     "trend up",
			OpenTime:   1000,
		},
	}

	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkPaper}); err != nil {
		t.Fatalf("HandleResult() error = %v", err)
	}
	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want opened paper position")
	}
	if currentPosition.EntryPrice != "101" {
		t.Fatalf("entry price = %q, want 101", currentPosition.EntryPrice)
	}
	events := store.Events()
	if got, want := len(events), 3; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	if events[0].EventType != strategy.EventTypeSignalGenerated {
		t.Fatalf("first event = %q, want signal_generated", events[0].EventType)
	}
}
