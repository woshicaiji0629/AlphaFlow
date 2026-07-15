package app

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
)

func TestEvaluateOpenPositionsTriggersExitRuleWithoutExecuting(t *testing.T) {
	store := position.NewMemoryStore()
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Account:      "paper-default",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "100",
		HighestPrice: "105",
		LowestPrice:  "100",
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "stop loss",
			TriggerPrice: "95",
		}},
	}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatal(err)
	}

	results, err := evaluateOpenPositions(
		context.Background(),
		store,
		fakePriceReader{price: strategy.PriceView{LastPrice: "94"}},
		position.NewManager(position.ManagerConfig{}),
		position.Filter{Scope: strategy.PositionScopePaper},
	)
	if err != nil {
		t.Fatalf("evaluateOpenPositions() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Plan.Action != strategy.PositionActionCloseLong {
		t.Fatalf("action = %q, want close_long", results[0].Plan.Action)
	}
	if results[0].Plan.TriggeredRule == nil || results[0].Plan.TriggeredRule.Type != strategy.ExitReasonStopLoss {
		t.Fatalf("triggered rule = %#v, want stop loss", results[0].Plan.TriggeredRule)
	}

	got, err := store.GetPosition(context.Background(), position.KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("position missing after evaluation")
	}
	if got.LowestPrice != "94" {
		t.Fatalf("lowest price = %q, want 94", got.LowestPrice)
	}
}

func TestEvaluateOpenPositionsSkipsWhenPriceMissing(t *testing.T) {
	store := position.NewMemoryStore()
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         1,
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonStopLoss,
			TriggerPrice: "95",
		}},
	}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatal(err)
	}

	results, err := evaluateOpenPositions(
		context.Background(),
		store,
		fakePriceReader{},
		position.NewManager(position.ManagerConfig{}),
		position.Filter{Scope: strategy.PositionScopePaper},
	)
	if err != nil {
		t.Fatalf("evaluateOpenPositions() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results len = %d, want 0", len(results))
	}
}

func TestEvaluateOpenPositionsSupportsLiveAccountPositions(t *testing.T) {
	store := position.NewMemoryStore()
	currentPosition := strategy.Position{
		Scope:        strategy.PositionScopeLive,
		Account:      "account-1",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		PositionSide: strategy.ExchangePositionSideLong,
		Side:         strategy.PositionSideLong,
		Size:         1,
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonTakeProfit,
			Reason:       "take profit",
			TriggerPrice: "110",
		}},
	}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatal(err)
	}

	results, err := evaluateOpenPositions(
		context.Background(),
		store,
		fakePriceReader{price: strategy.PriceView{MarkPrice: "111"}},
		position.NewManager(position.ManagerConfig{}),
		position.Filter{Scope: strategy.PositionScopeLive},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Plan.Action != strategy.PositionActionCloseLong {
		t.Fatalf("results=%#v", results)
	}
}
