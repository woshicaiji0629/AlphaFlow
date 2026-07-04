package execution

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildOrderIntentOpenLong(t *testing.T) {
	intent, ok, err := BuildOrderIntent(IntentRequest{
		IntentID:       "intent-1",
		IdempotencyKey: "idem-1",
		Target: strategy.Target{
			Scope:    strategy.PositionScopePaper,
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
		},
		StrategyName: "keltner",
		Plan: strategy.OrderPlan{
			Action:     strategy.PositionActionOpenLong,
			TargetSize: 1.5,
			Reason:     "open long",
		},
		ReferencePrice: "100.5",
		CreatedAt:      123,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected intent")
	}
	assertIntent(t, intent, OrderActionOpen, OrderSideBuy, "long", 1.5, false)
	if intent.Scope != "paper" {
		t.Fatalf("scope = %q, want paper", intent.Scope)
	}
	if intent.StrategyName != "keltner" {
		t.Fatalf("strategy = %q, want keltner", intent.StrategyName)
	}
	if intent.ReferencePrice != "100.5" {
		t.Fatalf("reference price = %q, want 100.5", intent.ReferencePrice)
	}
}

func TestBuildOrderIntentBuildsDefaultIDs(t *testing.T) {
	intent, ok, err := BuildOrderIntent(IntentRequest{
		Target: strategy.Target{
			Scope:    strategy.PositionScopeBacktest,
			RunID:    "run-1",
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
		},
		StrategyName: "keltner",
		Plan: strategy.OrderPlan{
			Action:     strategy.PositionActionOpenLong,
			TargetSize: 1,
		},
		BarOpenTime: 999,
		CreatedAt:   1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected intent")
	}
	want := "intent:bt:run-1::binance:um:ETHUSDT:keltner:999:open_long:buy:long"
	if intent.IdempotencyKey != want {
		t.Fatalf("idempotency key = %q, want %q", intent.IdempotencyKey, want)
	}
	if intent.IntentID != want {
		t.Fatalf("intent id = %q, want %q", intent.IntentID, want)
	}
}

func TestBuildOrderIntentCloseLongUsesPositionSize(t *testing.T) {
	intent, ok, err := BuildOrderIntent(IntentRequest{
		Target: strategy.Target{Scope: strategy.PositionScopePaper},
		Plan: strategy.OrderPlan{
			Action: strategy.PositionActionCloseLong,
			Reason: "close long",
		},
		Position: &strategy.Position{Size: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected intent")
	}
	assertIntent(t, intent, OrderActionClose, OrderSideSell, "long", 2, true)
}

func TestBuildOrderIntentReduceShortUsesExitSize(t *testing.T) {
	intent, ok, err := BuildOrderIntent(IntentRequest{
		Target: strategy.Target{Scope: strategy.PositionScopeBacktest},
		Plan: strategy.OrderPlan{
			Action:   strategy.PositionActionReduceShort,
			ExitSize: 0.5,
			Reason:   "reduce short",
		},
		Position: &strategy.Position{Size: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected intent")
	}
	assertIntent(t, intent, OrderActionReduce, OrderSideBuy, "short", 0.5, true)
}

func TestBuildOrderIntentHoldReturnsNoIntent(t *testing.T) {
	_, ok, err := BuildOrderIntent(IntentRequest{
		Plan: strategy.OrderPlan{Action: strategy.PositionActionHold},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no intent")
	}
}

func TestBuildOrderIntentRejectsMissingCloseQuantity(t *testing.T) {
	_, ok, err := BuildOrderIntent(IntentRequest{
		Plan: strategy.OrderPlan{Action: strategy.PositionActionCloseLong},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected no intent")
	}
}

func assertIntent(
	t *testing.T,
	intent OrderIntent,
	action OrderAction,
	side OrderSide,
	positionSide string,
	quantity float64,
	reduceOnly bool,
) {
	t.Helper()
	if intent.Action != action {
		t.Fatalf("action = %q, want %q", intent.Action, action)
	}
	if intent.Side != side {
		t.Fatalf("side = %q, want %q", intent.Side, side)
	}
	if intent.PositionSide != positionSide {
		t.Fatalf("position side = %q, want %q", intent.PositionSide, positionSide)
	}
	if intent.Quantity != quantity {
		t.Fatalf("quantity = %v, want %v", intent.Quantity, quantity)
	}
	if intent.ReduceOnly != reduceOnly {
		t.Fatalf("reduce only = %v, want %v", intent.ReduceOnly, reduceOnly)
	}
}
