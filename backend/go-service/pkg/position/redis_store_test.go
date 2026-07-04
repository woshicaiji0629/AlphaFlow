package position

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/pkg/strategy"
)

func TestEncodeDecodePosition(t *testing.T) {
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopeBacktest,
		RunID:        "run-1",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		PositionSide: strategy.ExchangePositionSideLong,
		Side:         strategy.PositionSideLong,
		Size:         1.5,
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonTrailingStop,
			Reason:       "protect profit",
			TriggerPrice: "100",
			SizePct:      0.5,
			Metadata: map[string]string{
				"trail_pct":       "1",
				"reference_price": "120",
			},
		}},
	}

	payload, err := encodePosition(currentPosition)
	if err != nil {
		t.Fatalf("encodePosition() error = %v", err)
	}
	got, err := decodePosition(payload)
	if err != nil {
		t.Fatalf("decodePosition() error = %v", err)
	}

	if got.Scope != currentPosition.Scope ||
		got.RunID != currentPosition.RunID ||
		got.Exchange != currentPosition.Exchange ||
		got.Market != currentPosition.Market ||
		got.Symbol != currentPosition.Symbol ||
		got.StrategyName != currentPosition.StrategyName ||
		got.PositionSide != currentPosition.PositionSide ||
		got.Side != currentPosition.Side ||
		got.Size != currentPosition.Size {
		t.Fatalf("decodePosition() = %+v, want %+v", got, currentPosition)
	}
	if len(got.ExitRules) != 1 {
		t.Fatalf("len(decoded.ExitRules) = %d, want 1", len(got.ExitRules))
	}
	if got.ExitRules[0].Metadata["reference_price"] != "120" {
		t.Fatalf("decoded metadata reference_price = %q, want 120", got.ExitRules[0].Metadata["reference_price"])
	}
}

func TestDecodePositionRejectsInvalidJSON(t *testing.T) {
	_, err := decodePosition([]byte("{"))
	if err == nil {
		t.Fatal("decodePosition() error = nil, want error")
	}
}

func TestRedisStorePositionTTL(t *testing.T) {
	store := NewRedisStore(nil, RedisStoreOptions{BacktestTTL: 30 * time.Minute})

	if got := store.positionTTL(strategy.PositionScopeBacktest); got != 30*time.Minute {
		t.Fatalf("backtest ttl = %s, want 30m", got)
	}
	if got := store.positionTTL(strategy.PositionScopePaper); got != 0 {
		t.Fatalf("paper ttl = %s, want 0", got)
	}
	if got := store.positionTTL(strategy.PositionScopeLive); got != 0 {
		t.Fatalf("live ttl = %s, want 0", got)
	}
}

func TestRedisStoreNilClientNoops(t *testing.T) {
	ctx := context.Background()
	store := NewRedisStore(nil, RedisStoreOptions{})
	key := Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	}

	currentPosition, err := store.GetPosition(ctx, key)
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition != nil {
		t.Fatalf("GetPosition() = %+v, want nil", currentPosition)
	}
	if err := store.SavePosition(ctx, strategy.Position{}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	if err := store.DeletePosition(ctx, key); err != nil {
		t.Fatalf("DeletePosition() error = %v", err)
	}
	positions, err := store.ListPositions(ctx, Filter{Scope: strategy.PositionScopePaper})
	if err != nil {
		t.Fatalf("ListPositions() error = %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("ListPositions() len = %d, want 0", len(positions))
	}
	if err := store.RegisterTempKey(ctx, "run-1", "key-1"); err != nil {
		t.Fatalf("RegisterTempKey() error = %v", err)
	}
	if err := store.CleanupTempKeys(ctx, "run-1"); err != nil {
		t.Fatalf("CleanupTempKeys() error = %v", err)
	}
}
