package fill

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"context"
	"testing"
)

func TestApplierOpensAndClosesPosition(t *testing.T) {
	store := position.NewMemoryStore()
	a, _ := NewApplier(store)
	intent := execution.OrderIntent{IntentID: "i1", Scope: "paper", Exchange: "binance", Market: "um", Symbol: "ETHUSDT", StrategyName: "supertrend", PositionSide: "long", Action: execution.OrderActionOpen, ExitRules: []strategy.ExitRule{{Type: strategy.ExitReasonStopLoss, TriggerPrice: "90"}}}
	report := execution.ExecutionReport{IntentID: "i1", Status: execution.ExecutionStatusFilled, FilledQuantity: 2, AveragePrice: "100", UpdatedAt: 10}
	if err := a.Apply(context.Background(), intent, report); err != nil {
		t.Fatal(err)
	}
	key := position.Key{Scope: strategy.PositionScopePaper, Exchange: "binance", Market: "um", Symbol: "ETHUSDT", StrategyName: "supertrend", PositionSide: strategy.ExchangePositionSideNet}
	got, _ := store.GetPosition(context.Background(), key)
	if got == nil || got.Size != 2 || len(got.ExitRules) != 1 {
		t.Fatalf("position=%#v", got)
	}
	intent.Action = execution.OrderActionClose
	if err := a.Apply(context.Background(), intent, report); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetPosition(context.Background(), key)
	if got != nil {
		t.Fatalf("position=%#v, want nil", got)
	}
}
func TestApplierIgnoresRejectedReport(t *testing.T) {
	store := position.NewMemoryStore()
	a, _ := NewApplier(store)
	if err := a.Apply(context.Background(), execution.OrderIntent{}, execution.ExecutionReport{Status: execution.ExecutionStatusRejected}); err != nil {
		t.Fatal(err)
	}
}
