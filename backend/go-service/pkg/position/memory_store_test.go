package position

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestMemoryStoreSavesAndGetsPosition(t *testing.T) {
	store := NewMemoryStore()
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "keltner",
		Side:         strategy.PositionSideLong,
		Size:         1,
		ExitRules: []strategy.ExitRule{
			{Type: strategy.ExitReasonTakeProfit, Metadata: map[string]string{"source": "test"}},
		},
	}

	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPosition(context.Background(), KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("position missing")
	}
	if got.Side != strategy.PositionSideLong {
		t.Fatalf("side = %q, want %q", got.Side, strategy.PositionSideLong)
	}

	got.ExitRules[0].Metadata["source"] = "mutated"
	again, err := store.GetPosition(context.Background(), KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatal(err)
	}
	if again.ExitRules[0].Metadata["source"] != "test" {
		t.Fatalf("metadata was mutated: %#v", again.ExitRules[0].Metadata)
	}
}

func TestMemoryStoreCleanupTempKeysDeletesBacktestPositions(t *testing.T) {
	store := NewMemoryStore()
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopeBacktest,
		RunID:        "run-1",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "keltner",
		Side:         strategy.PositionSideLong,
		Size:         1,
	}
	key, err := RedisKey(KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterTempKey(context.Background(), "run-1", key); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupTempKeys(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetPosition(context.Background(), KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("position still exists: %#v", got)
	}
}

func TestMemoryStoreAppendsEventsAndSavesSummary(t *testing.T) {
	store := NewMemoryStore()
	event := strategy.StrategyEvent{
		EventID:   "event-1",
		EventType: strategy.EventTypeSignalGenerated,
		Metadata:  map[string]string{"reason": "test"},
	}
	summary := strategy.BacktestRunSummary{
		RunID:    "run-1",
		Status:   strategy.BacktestRunStatusCompleted,
		Symbols:  []string{"ETHUSDT"},
		Metadata: map[string]string{"source": "test"},
	}

	if err := store.AppendEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBacktestRunSummary(context.Background(), summary); err != nil {
		t.Fatal(err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	events[0].Metadata["reason"] = "mutated"
	if store.Events()[0].Metadata["reason"] != "test" {
		t.Fatal("event metadata was mutated")
	}

	got, ok := store.BacktestRunSummary("run-1")
	if !ok {
		t.Fatal("summary missing")
	}
	got.Metadata["source"] = "mutated"
	again, ok := store.BacktestRunSummary("run-1")
	if !ok {
		t.Fatal("summary missing")
	}
	if again.Metadata["source"] != "test" {
		t.Fatal("summary metadata was mutated")
	}
}
