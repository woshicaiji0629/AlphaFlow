package position

import (
	"math"
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

	refreshed := RefreshPositionExtremesAt(currentPosition, "120", 2000)

	if refreshed.HighestPrice != "120" {
		t.Fatalf("highest price = %q, want 120", refreshed.HighestPrice)
	}
	if refreshed.LowestPrice != "100" {
		t.Fatalf("lowest price = %q, want 100", refreshed.LowestPrice)
	}
	if refreshed.ExitRules[0].Metadata["reference_price"] != "120" {
		t.Fatalf("reference price = %q, want 120", refreshed.ExitRules[0].Metadata["reference_price"])
	}
	if refreshed.HighestPriceBarOpenTime != 2000 {
		t.Fatalf("highest price bar open time = %d, want 2000", refreshed.HighestPriceBarOpenTime)
	}
}

func TestRefreshPositionBarExtremesPreservesEachExtremeTime(t *testing.T) {
	currentPosition := strategy.Position{
		Side:                    strategy.PositionSideShort,
		HighestPrice:            "105",
		LowestPrice:             "95",
		HighestPriceBarOpenTime: 1000,
		LowestPriceBarOpenTime:  1000,
	}

	refreshed := RefreshPositionBarExtremesAt(currentPosition, "104", "90", 2000)

	if refreshed.HighestPrice != "105" || refreshed.HighestPriceBarOpenTime != 1000 {
		t.Fatalf("highest extreme = %s/%d, want 105/1000", refreshed.HighestPrice, refreshed.HighestPriceBarOpenTime)
	}
	if refreshed.LowestPrice != "90" || refreshed.LowestPriceBarOpenTime != 2000 {
		t.Fatalf("lowest extreme = %s/%d, want 90/2000", refreshed.LowestPrice, refreshed.LowestPriceBarOpenTime)
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

func TestManagerRiskExitBar(t *testing.T) {
	stopLong := strategy.ExitRule{Type: strategy.ExitReasonStopLoss, TriggerPrice: "90", SizePct: 1}
	stopShort := strategy.ExitRule{Type: strategy.ExitReasonStopLoss, TriggerPrice: "110", SizePct: 1}
	takeProfit := strategy.ExitRule{Type: strategy.ExitReasonTakeProfit, TriggerPrice: "110", SizePct: 1}
	trailing := strategy.ExitRule{Type: strategy.ExitReasonTrailingStop, SizePct: 1, Metadata: map[string]string{
		"trail_pct": "5", "reference_price": "100",
	}}
	tests := []struct {
		name, open, high, low, fill string
		side                        strategy.PositionSide
		rules                       []strategy.ExitRule
		reason                      strategy.ExitReasonType
	}{
		{name: "intrabar long stop", open: "100", high: "105", low: "89", fill: "90", side: strategy.PositionSideLong, rules: []strategy.ExitRule{stopLong}, reason: strategy.ExitReasonStopLoss},
		{name: "short stop gap", open: "112", high: "115", low: "108", fill: "112", side: strategy.PositionSideShort, rules: []strategy.ExitRule{stopShort}, reason: strategy.ExitReasonStopLoss},
		{name: "stop before take profit", open: "100", high: "111", low: "89", fill: "90", side: strategy.PositionSideLong, rules: []strategy.ExitRule{takeProfit, stopLong}, reason: strategy.ExitReasonStopLoss},
		{name: "same bar trailing", open: "100", high: "120", low: "113", fill: "114", side: strategy.PositionSideLong, rules: []strategy.ExitRule{trailing}, reason: strategy.ExitReasonTrailingStop},
	}
	manager := NewManager(ManagerConfig{})
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			current := &strategy.Position{Side: item.side, Size: 1, ExitRules: item.rules}
			plan, fill := manager.RiskExitBar(current, item.open, item.high, item.low)
			if plan == nil || plan.ExitReason != item.reason || fill != item.fill {
				t.Fatalf("plan/fill = %#v/%q, want %q/%q", plan, fill, item.reason, item.fill)
			}
		})
	}
}

func TestManagerGuardedTrailingWaitsForProfitActivation(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	current := &strategy.Position{
		Side:       strategy.PositionSideLong,
		Size:       1,
		EntryPrice: "100",
		ExitRules:  []strategy.ExitRule{guardedTrailingRule("100.2")},
	}

	plan, fill := manager.RiskExitBar(current, "100", "100.2", "99")
	if plan != nil || fill != "" {
		t.Fatalf("plan/fill = %#v/%q, want no guarded exit before activation", plan, fill)
	}
	if plan := manager.PlanWithPrice(testResult(strategy.SignalSideHold, 0), current, "99"); plan.Action != strategy.PositionActionHold {
		t.Fatalf("live action = %q, want hold before activation", plan.Action)
	}
}

func TestManagerGuardedTrailingLocksProfitForLongAndShort(t *testing.T) {
	tests := []struct {
		name                 string
		side                 strategy.PositionSide
		open                 string
		reference, high, low string
		wantFill             float64
		wantAction           strategy.PositionAction
	}{
		{name: "long", side: strategy.PositionSideLong, open: "100.3", reference: "100.4", high: "100.4", low: "100.1", wantFill: 100.16, wantAction: strategy.PositionActionCloseLong},
		{name: "short", side: strategy.PositionSideShort, open: "99.7", reference: "99.6", high: "99.9", low: "99.6", wantFill: 99.84, wantAction: strategy.PositionActionCloseShort},
	}
	manager := NewManager(ManagerConfig{})
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			current := &strategy.Position{
				Side:       item.side,
				Size:       1,
				EntryPrice: "100",
				ExitRules:  []strategy.ExitRule{guardedTrailingRule(item.reference)},
			}
			plan, fill := manager.RiskExitBar(current, item.open, item.high, item.low)
			if plan == nil || plan.Action != item.wantAction || plan.ExitReason != strategy.ExitReasonTrailingStop {
				t.Fatalf("plan = %#v, want guarded trailing close", plan)
			}
			assertPriceClose(t, fill, item.wantFill)
		})
	}
}

func TestManagerGuardedTrailingLetsLargeWinnerRunToPeakTrail(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	current := &strategy.Position{
		Side:       strategy.PositionSideLong,
		Size:       1,
		EntryPrice: "100",
		ExitRules:  []strategy.ExitRule{guardedTrailingRule("120")},
	}

	plan, fill := manager.RiskExitBar(current, "119.5", "120", "119.3")
	if plan == nil || plan.ExitReason != strategy.ExitReasonTrailingStop {
		t.Fatalf("plan = %#v, want peak trailing exit", plan)
	}
	assertPriceClose(t, fill, 119.4)
}

func TestAdaptiveProtectedTrailWidensRunnerWithoutLoweringActivationAnchor(t *testing.T) {
	tests := []struct {
		name      string
		side      strategy.PositionSide
		reference float64
		want      float64
	}{
		{name: "long", side: strategy.PositionSideLong, reference: 102, want: 100.98},
		{name: "short", side: strategy.PositionSideShort, reference: 98, want: 98.98},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			current := strategy.Position{Side: item.side, EntryPrice: "100"}
			trigger, active := protectedTrailingTriggerPrice(current, adaptiveTrailingRule(), item.reference, 0.5)
			if !active || math.Abs(trigger-item.want) > 1e-9 {
				t.Fatalf("trigger/active = %v/%v, want %v/true", trigger, active, item.want)
			}
		})
	}

	current := strategy.Position{Side: strategy.PositionSideLong, EntryPrice: "100"}
	trigger, active := protectedTrailingTriggerPrice(current, adaptiveTrailingRule(), 101, 0.5)
	if !active || math.Abs(trigger-100.495) > 1e-9 {
		t.Fatalf("runner activation anchor = %v/%v, want 100.495/true", trigger, active)
	}
}

func adaptiveTrailingRule() strategy.ExitRule {
	return strategy.ExitRule{
		Type: strategy.ExitReasonTrailingStop,
		Metadata: map[string]string{
			"profit_guard_activation_bps": "30",
			"profit_guard_floor_bps":      "16",
			"adaptive_trailing":           "true",
			"runner_activation_bps":       "100",
			"runner_trail_pct":            "1",
		},
	}
}

func guardedTrailingRule(reference string) strategy.ExitRule {
	return strategy.ExitRule{
		Type:    strategy.ExitReasonTrailingStop,
		Reason:  "guarded trailing stop",
		SizePct: 1,
		Metadata: map[string]string{
			"trail_pct":                   "0.5",
			"reference_price":             reference,
			"profit_guard_activation_bps": "30",
			"profit_guard_floor_bps":      "16",
		},
	}
}

func assertPriceClose(t *testing.T, value string, want float64) {
	t.Helper()
	got, ok := parseFloat(value)
	if !ok || math.Abs(got-want) > 1e-9 {
		t.Fatalf("price = %q, want %v", value, want)
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
