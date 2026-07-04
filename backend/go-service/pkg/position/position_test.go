package position

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestManagerOpensLong(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	result := testResult(strategy.SignalSideBuy, 0.8)

	plan := manager.Plan(result, nil)

	if plan.Action != strategy.PositionActionOpenLong {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionOpenLong)
	}
	if plan.TargetSide != strategy.PositionSideLong {
		t.Fatalf("target side = %q, want %q", plan.TargetSide, strategy.PositionSideLong)
	}
	if plan.TargetSize != defaultMaxPositionSize {
		t.Fatalf("target size = %v, want %v", plan.TargetSize, defaultMaxPositionSize)
	}
}

func TestManagerClosesOppositePosition(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	result := testResult(strategy.SignalSideSell, 0.8)
	currentPosition := &strategy.Position{Side: strategy.PositionSideLong, Size: 1}

	plan := manager.Plan(result, currentPosition)

	if plan.Action != strategy.PositionActionCloseLong {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionCloseLong)
	}
	if plan.ExitReason != strategy.ExitReasonStrategy {
		t.Fatalf("exit reason = %q, want %q", plan.ExitReason, strategy.ExitReasonStrategy)
	}
}

func TestManagerHoldsWhenConfidenceIsLow(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	result := testResult(strategy.SignalSideBuy, 0.2)

	plan := manager.Plan(result, nil)

	if plan.Action != strategy.PositionActionHold {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionHold)
	}
	if plan.Reason != "signal confidence below position threshold" {
		t.Fatalf("reason = %q", plan.Reason)
	}
}

func TestManagerBlocksShortWhenDisabled(t *testing.T) {
	manager := NewManager(ManagerConfig{
		DisableShortExposure: true,
	})
	result := testResult(strategy.SignalSideSell, 0.8)

	plan := manager.Plan(result, nil)

	if plan.Action != strategy.PositionActionHold {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionHold)
	}
	if plan.Reason != "short exposure disabled" {
		t.Fatalf("reason = %q", plan.Reason)
	}
}

func TestManagerClosesLongOnTakeProfit(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	currentPosition := &strategy.Position{
		Side: strategy.PositionSideLong,
		Size: 1,
		ExitRules: []strategy.ExitRule{
			{Type: strategy.ExitReasonTakeProfit, Reason: "take profit", TriggerPrice: "110"},
		},
	}

	plan := manager.PlanWithPrice(testResult(strategy.SignalSideHold, 0), currentPosition, "111")

	if plan.Action != strategy.PositionActionCloseLong {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionCloseLong)
	}
	if plan.ExitReason != strategy.ExitReasonTakeProfit {
		t.Fatalf("exit reason = %q, want %q", plan.ExitReason, strategy.ExitReasonTakeProfit)
	}
	if plan.ExitSize != 1 {
		t.Fatalf("exit size = %v, want 1", plan.ExitSize)
	}
}

func TestManagerReducesShortOnPartialTakeProfit(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	currentPosition := &strategy.Position{
		Side: strategy.PositionSideShort,
		Size: 2,
		ExitRules: []strategy.ExitRule{
			{
				Type:         strategy.ExitReasonTakeProfit,
				Reason:       "partial take profit",
				TriggerPrice: "90",
				SizePct:      0.5,
			},
		},
	}

	plan := manager.PlanWithPrice(testResult(strategy.SignalSideHold, 0), currentPosition, "89")

	if plan.Action != strategy.PositionActionReduceShort {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionReduceShort)
	}
	if plan.ExitSize != 1 {
		t.Fatalf("exit size = %v, want 1", plan.ExitSize)
	}
}

func TestRefreshPositionExtremesUpdatesTrailingReference(t *testing.T) {
	currentPosition := strategy.Position{
		Side:         strategy.PositionSideLong,
		HighestPrice: "100",
		LowestPrice:  "100",
		ExitRules: []strategy.ExitRule{
			{
				Type: strategy.ExitReasonTrailingStop,
				Metadata: map[string]string{
					"trail_pct":       "5",
					"reference_price": "100",
				},
			},
		},
	}

	refreshed := RefreshPositionExtremes(currentPosition, "120")

	if refreshed.HighestPrice != "120" {
		t.Fatalf("highest price = %q, want 120", refreshed.HighestPrice)
	}
	if refreshed.LowestPrice != "100" {
		t.Fatalf("lowest price = %q, want 100", refreshed.LowestPrice)
	}
	if refreshed.ExitRules[0].Metadata["reference_price"] != "120" {
		t.Fatalf("reference price = %q, want 120", refreshed.ExitRules[0].Metadata["reference_price"])
	}
}

func TestManagerClosesLongOnTrailingStop(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	currentPosition := &strategy.Position{
		Side: strategy.PositionSideLong,
		Size: 1,
		ExitRules: []strategy.ExitRule{
			{
				Type:   strategy.ExitReasonTrailingStop,
				Reason: "trailing stop",
				Metadata: map[string]string{
					"trail_pct":       "5",
					"reference_price": "120",
				},
			},
		},
	}

	plan := manager.PlanWithPrice(testResult(strategy.SignalSideHold, 0), currentPosition, "113")

	if plan.Action != strategy.PositionActionCloseLong {
		t.Fatalf("action = %q, want %q", plan.Action, strategy.PositionActionCloseLong)
	}
	if plan.ExitReason != strategy.ExitReasonTrailingStop {
		t.Fatalf("exit reason = %q, want %q", plan.ExitReason, strategy.ExitReasonTrailingStop)
	}
}

func testResult(side strategy.SignalSide, confidence float64) strategy.Result {
	return strategy.Result{
		StrategyName: "test",
		Signal: strategy.Signal{
			Side:       side,
			Confidence: confidence,
			Reason:     "test signal",
		},
	}
}
