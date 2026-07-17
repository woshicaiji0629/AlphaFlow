package supertrend

import (
	"context"
	"fmt"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestEvaluateReturnsBuyWhenLongSetupConfirmed(t *testing.T) {
	item := New(Config{})
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, nil), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	if result.Signal.Score < 0.72 {
		t.Fatalf("score = %v, want >= 0.72", result.Signal.Score)
	}
	if len(result.ExitRules) != 2 {
		t.Fatalf("exit rules = %d, want 2", len(result.ExitRules))
	}
	for _, rule := range result.ExitRules {
		if rule.SizePct != 1 {
			t.Fatalf("exit rule %#v size pct = %v, want 1", rule.Type, rule.SizePct)
		}
		if rule.Type == strategy.ExitReasonTrailingStop {
			t.Fatalf("default exit rule = %#v, want structure exits only", rule)
		}
	}
	if check, ok := diagnostic(result.Analysis.Checks, "entry_threshold", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusPass {
		t.Fatalf("buy threshold diagnostic = %#v", check)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "exit_geometry", strategy.SignalSideBuy); ok {
		t.Fatalf("disabled exit geometry diagnostic = %#v, want absent", check)
	}
}

func TestEvaluateBlocksOppositeMACDBias(t *testing.T) {
	item := New(Config{})
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"macd_window_bias": "bear",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "macd", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusBlocked {
		t.Fatalf("MACD diagnostic = %#v", check)
	}
}

func TestEvaluateBlocksOppositeMACDMomentumEnum(t *testing.T) {
	result, err := New(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"macd_window_bias": "neutral",
		"macd_momentum":    "expanding_bear",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "macd", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusBlocked {
		t.Fatalf("MACD diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsMissingAlignmentsAsScoreOnly(t *testing.T) {
	result, err := New(Config{EntryThreshold: 0.3}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_window_bias":    "neutral",
		"supertrend_direction": "neutral",
		"ma_ribbon_state":      "neutral",
		"ma_window_bias":       "neutral",
		"ema_alignment":        "neutral",
		"macd_window_bias":     "neutral",
		"macd_momentum":        "flat",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	for _, name := range []string{"trend", "moving_average", "macd"} {
		if check, ok := diagnostic(result.Analysis.Checks, name, strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusPass {
			t.Fatalf("%s diagnostic = %#v", name, check)
		}
	}
}

func TestEvaluateTreatsTangledMAWindowAsScoreOnly(t *testing.T) {
	result, err := New(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"ma_window_tangled": "true",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "moving_average", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusPass || check.Score != 0 || check.Reason != "ma window tangled; score withheld" {
		t.Fatalf("moving average diagnostic = %#v", check)
	}
}

func TestEvaluateBlocksWhenMomentumEnergyIsInsufficient(t *testing.T) {
	result, err := New(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"ma_window_spread_state":    "flat",
		"ma_window_slope_level":     "flat",
		"macd_window_confirmed":     "false",
		"macd_window_acceleration":  "flat",
		"macd_momentum":             "fading_bull",
		"price_volume_confirmation": "neutral",
		"volume_window_state":       "normal",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "momentum_energy", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["confirmations"] != "0" {
		t.Fatalf("momentum energy diagnostic = %#v", check)
	}
}

func diagnostic(checks []strategy.DiagnosticCheck, name string, side strategy.SignalSide) (strategy.DiagnosticCheck, bool) {
	for _, check := range checks {
		if check.Name == name && check.Side == side {
			return check, true
		}
	}
	return strategy.DiagnosticCheck{}, false
}

func exitRule(rules []strategy.ExitRule, ruleType strategy.ExitReasonType) (strategy.ExitRule, bool) {
	for _, rule := range rules {
		if rule.Type == ruleType {
			return rule, true
		}
	}
	return strategy.ExitRule{}, false
}

func TestLatestSignalStableCountSupportsDenseReplayWindow(t *testing.T) {
	tests := []struct {
		name        string
		signalCount int
		denseCount  float64
		want        int
	}{
		{name: "signal series takes precedence", signalCount: 2, denseCount: 9, want: 2},
		{name: "dense replay fallback", denseCount: 9, want: 9},
		{name: "missing count", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window := strategy.IndicatorWindowView{
				Values: map[string]strategy.NumericSeries{},
				Signals: map[string]strategy.SignalSeries{
					"supertrend_direction": {Latest: "up", StableCount: tt.signalCount},
				},
			}
			if tt.denseCount > 0 {
				window.Values["supertrend_direction_win_stable_count"] = strategy.NumericSeries{Latest: tt.denseCount}
			}
			if got := latestSignalStableCount(window, "supertrend_direction"); got != tt.want {
				t.Fatalf("latestSignalStableCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEvaluateBlocksWhenShortTimeframesOppose(t *testing.T) {
	item := New(Config{})
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["5m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := item.Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
}

func TestEvaluateBlocksUntilTenMinutePullbackResolves(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := New(Config{EntryThreshold: 0.3, MaxBlockingTimeframes: 1}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["state"] != "pullback" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateWaitsDuringTenMinuteTransition(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["state"] != "transition" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateWaitsForTenMinuteTrendToStabilize(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	tenMinute := got.Timeframes["10m"]
	tenMinute.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 1}
	got.Timeframes["10m"] = tenMinute

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["state"] != "stabilizing" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateRejectsShockBreakoutUntilAllHigherTimeframesAlign(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
	})
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	tenMinute := got.Timeframes["10m"]
	tenMinute.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 1}
	got.Timeframes["10m"] = tenMinute
	got.Timeframes["15m"] = neutralTimeframe(strategy.SignalSideBuy)

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["shock_authorized"] != "false" {
		t.Fatalf("entry mode diagnostic = %#v", check)
	}
	for key, want := range map[string]string{
		"shock_structure":                 "true",
		"shock_impulse":                   "true",
		"shock_impulse_stable_count":      "2",
		"shock_price_volume":              "true",
		"shock_volume_expanded":           "true",
		"shock_3m":                        "aligned",
		"shock_5m":                        "aligned",
		"shock_10m":                       "aligned",
		"shock_15m":                       "neutral",
		"shock_30m":                       "aligned",
		"shock_higher_timeframes_clear":   "true",
		"shock_higher_timeframes_aligned": "false",
	} {
		if got := check.Values[key]; got != want {
			t.Fatalf("entry mode diagnostic %s = %q, want %q", key, got, want)
		}
	}
	regime, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || regime.Status != strategy.DiagnosticStatusBlocked || regime.Values["state"] != "neutral" {
		t.Fatalf("higher timeframe diagnostic = %#v", regime)
	}
}

func TestEvaluateRejectsShockBreakoutAgainstFifteenMinuteOpposition(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
	})
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["shock_authorized"] != "false" ||
		check.Values["shock_15m"] != "blocking" || check.Values["shock_higher_timeframes_clear"] != "false" {
		t.Fatalf("entry mode diagnostic = %#v", check)
	}
}

func TestEvaluateRejectsShockBreakoutWithHighFakeRisk(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
		"ma_window_tangled":       "true",
		"pump_window_fake_risk":   "high",
	})

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	maCheck, ok := diagnostic(result.Analysis.Checks, "moving_average", strategy.SignalSideBuy)
	if !ok || maCheck.Status != strategy.DiagnosticStatusPass || maCheck.Score != 0 || maCheck.Reason != "ma window tangled; score withheld" {
		t.Fatalf("moving average diagnostic = %#v", maCheck)
	}
	fakeCheck, ok := diagnostic(result.Analysis.Checks, "fake_signal_risk", strategy.SignalSideBuy)
	if !ok || fakeCheck.Status != strategy.DiagnosticStatusBlocked || fakeCheck.Reason != "high fake signal risk blocked" || fakeCheck.Values["risk"] != "high" {
		t.Fatalf("fake signal diagnostic = %#v", fakeCheck)
	}
}

func TestEvaluateRejectsIndependentShockBreakoutWithMediumFakeRisk(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
		"pump_window_fake_risk":   "medium",
	})
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["shock_authorized"] != "false" ||
		check.Values["shock_fake_risk"] != "medium" || check.Values["shock_fake_risk_low"] != "false" {
		t.Fatalf("entry mode diagnostic = %#v", check)
	}
}

func TestEvaluateAllowsNormalTriggerWhenShockBreakoutHasSoftLocalRisk(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
		"ma_window_tangled":       "true",
		"pump_window_fake_risk":   "medium",
		"trend_price_progress":    "stalling",
	})

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	modeCheck, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || modeCheck.Status != strategy.DiagnosticStatusPass || modeCheck.Values["mode"] != "trend_continuation" ||
		modeCheck.Values["shock_authorized"] != "false" {
		t.Fatalf("entry mode diagnostic = %#v", modeCheck)
	}
	trendCheck, ok := diagnostic(result.Analysis.Checks, "trend", strategy.SignalSideBuy)
	if !ok || trendCheck.Status != strategy.DiagnosticStatusPass || trendCheck.Score != 0 || trendCheck.Reason != "trend progress not advancing; score withheld" {
		t.Fatalf("trend diagnostic = %#v", trendCheck)
	}
	maCheck, ok := diagnostic(result.Analysis.Checks, "moving_average", strategy.SignalSideBuy)
	if !ok || maCheck.Status != strategy.DiagnosticStatusPass || maCheck.Score != 0 || maCheck.Reason != "ma window tangled; score withheld" {
		t.Fatalf("moving average diagnostic = %#v", maCheck)
	}
	fakeCheck, ok := diagnostic(result.Analysis.Checks, "fake_signal_risk", strategy.SignalSideBuy)
	if !ok || fakeCheck.Status != strategy.DiagnosticStatusPass || fakeCheck.Values["risk"] != "medium" {
		t.Fatalf("fake signal diagnostic = %#v", fakeCheck)
	}
}

func TestEvaluateRejectsShockBreakoutWithoutThreeMomentumConfirmations(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality":  "confirmed",
		"ma_window_tangled":        "true",
		"macd_window_bias":         "bear",
		"macd_momentum":            "contracting",
		"macd_window_acceleration": "falling",
	})
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["shock_authorized"] != "false" ||
		check.Values["shock_momentum_confirmations"] != "2" || check.Values["shock_momentum_required"] != "3" {
		t.Fatalf("entry mode diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsGenericMomentumOppositionAsScoreOnlyForShockBreakout(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality": "confirmed",
		"macd_window_bias":        "bear",
		"macd_momentum":           "bear",
	})

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "macd", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Reason != "macd blocked; shock breakout score only" {
		t.Fatalf("macd diagnostic = %#v", check)
	}
}

func TestEvaluateRejectsShockBreakoutWithTrendReversalRisk(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"breakout_window_quality":    "confirmed",
		"trend_window_reversal_risk": "true",
	})

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	trendCheck, ok := diagnostic(result.Analysis.Checks, "trend", strategy.SignalSideBuy)
	if !ok || trendCheck.Status != strategy.DiagnosticStatusBlocked || trendCheck.Reason != "trend reversal risk blocked" {
		t.Fatalf("trend diagnostic = %#v", trendCheck)
	}
}

func TestEvaluateTreatsUnconfirmedTrendAsScoreOnlyInContinuation(t *testing.T) {
	result, err := New(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_valid":   "false",
		"trend_quality": "weak",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	trendCheck, ok := diagnostic(result.Analysis.Checks, "trend", strategy.SignalSideBuy)
	if !ok || trendCheck.Status != strategy.DiagnosticStatusPass || trendCheck.Score != 0 {
		t.Fatalf("trend diagnostic = %#v", trendCheck)
	}
	modeCheck, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || modeCheck.Values["mode"] != "trend_continuation" {
		t.Fatalf("entry mode diagnostic = %#v", modeCheck)
	}
}

func TestEvaluateRejectsStaleTrendWithoutContinuationOpportunity(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "false"}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "entry_trigger", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusBlocked {
		t.Fatalf("entry trigger diagnostic = %#v", check)
	}
}

func TestEvaluateRejectsSecondContinuationBarWithoutBreakoutStructure(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["shock_structure"] != "false" {
		t.Fatalf("entry mode diagnostic = %#v", check)
	}
}

func TestEvaluateBlocksCountertrendEntryAgainstHigherTimeframes(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["30m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["15m"] != "blocking" || check.Values["30m"] != "blocking" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateWaitsForFiveMinutePullbackResolution(t *testing.T) {
	got := snapshot(strategy.SignalSideSell, nil)
	got.Timeframes["5m"] = timeframe(strategy.SignalSideBuy, nil)

	blocked, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() blocked error = %v", err)
	}
	if blocked.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("blocked side = %q, want hold", blocked.Signal.Side)
	}
	check, ok := diagnostic(blocked.Analysis.Checks, "pullback_resolution", strategy.SignalSideSell)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["5m"] != "blocking" {
		t.Fatalf("pullback diagnostic = %#v", check)
	}

	got.Timeframes["5m"] = timeframe(strategy.SignalSideSell, nil)
	allowed, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() allowed error = %v", err)
	}
	if allowed.Signal.Side != strategy.SignalSideSell {
		t.Fatalf("allowed side = %q, want sell", allowed.Signal.Side)
	}
}

func TestEvaluateAcceptsPreviousBarWickReclaim(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "false"}
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	got.Window.Values["close"] = strategy.NumericSeries{Latest: 100, Previous: 96}
	got.Window.Values["low"] = strategy.NumericSeries{Latest: 99, Previous: 94}
	got.Window.Values["supertrend"] = strategy.NumericSeries{Latest: 95, Previous: 95}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
}

func TestEvaluateAcceptsFreshSupertrendLeg(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	got.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 1}

	result, err := New(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
}

func TestEvaluateReturnsSellWhenShortTrendAdvances(t *testing.T) {
	item := New(Config{})
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideSell, nil), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideSell {
		t.Fatalf("side = %q, want sell", result.Signal.Side)
	}
}

func TestEvaluateBuildsFullPositionTrailingExitRules(t *testing.T) {
	for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
		t.Run(string(side), func(t *testing.T) {
			got := snapshot(side, nil)
			got.Window.Values["close"] = strategy.NumericSeries{Latest: 100}
			if side == strategy.SignalSideSell {
				got.Window.Values["supertrend"] = strategy.NumericSeries{Latest: 105}
			}

			result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != side {
				t.Fatalf("side = %q, want %q", result.Signal.Side, side)
			}
			if len(result.ExitRules) != 1 {
				t.Fatalf("exit rules = %#v, want trailing stop only when hard risk stop is disabled", result.ExitRules)
			}
			if _, ok := exitRule(result.ExitRules, strategy.ExitReasonTakeProfit); ok {
				t.Fatalf("exit rules = %#v, want no fixed take profit", result.ExitRules)
			}
			if stop, ok := exitRule(result.ExitRules, strategy.ExitReasonStopLoss); ok {
				t.Fatalf("stop loss = %#v, want no intrabar structure stop", stop)
			}
			trailing, ok := exitRule(result.ExitRules, strategy.ExitReasonTrailingStop)
			if !ok || trailing.SizePct != 1 {
				t.Fatalf("trailing stop = %#v, want full-position rule", trailing)
			}
			if trailing.Metadata["trail_pct"] != "0.5" || trailing.Metadata["reference_price"] != "100" {
				t.Fatalf("trailing stop metadata = %#v", trailing.Metadata)
			}
			if trailing.Metadata["profit_guard_activation_bps"] != "150" || trailing.Metadata["profit_guard_floor_bps"] != "100" {
				t.Fatalf("profit guard metadata = %#v", trailing.Metadata)
			}
		})
	}
}

func TestEvaluateCapsFixedStopLossDistance(t *testing.T) {
	for _, item := range []struct {
		side strategy.SignalSide
		stop float64
	}{
		{side: strategy.SignalSideBuy, stop: 99.5},
		{side: strategy.SignalSideSell, stop: 100.5},
	} {
		t.Run(string(item.side), func(t *testing.T) {
			got := snapshot(item.side, nil)
			got.Window.Values["close"] = strategy.NumericSeries{Latest: 100}
			if item.side == strategy.SignalSideSell {
				got.Window.Values["supertrend"] = strategy.NumericSeries{Latest: 105}
			}
			result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5, MaxStopLossBps: 50}).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			stop, ok := exitRule(result.ExitRules, strategy.ExitReasonStopLoss)
			if !ok || stop.TriggerPrice != formatFloat(item.stop) {
				t.Fatalf("stop loss = %#v, want %v", stop, item.stop)
			}
		})
	}
}

func TestEvaluateBlocksTrailingEntryWithoutReferencePrice(t *testing.T) {
	result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(
		context.Background(),
		snapshot(strategy.SignalSideSell, nil),
		nil,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "exit_rules", strategy.SignalSideSell)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Reason != "trailing stop reference price missing" {
		t.Fatalf("exit rules diagnostic = %#v", check)
	}
	if len(result.ExitRules) != 0 {
		t.Fatalf("exit rules = %#v, want none while entry is blocked", result.ExitRules)
	}
}

func TestEvaluateAppliesExitGeometryGuard(t *testing.T) {
	tests := []struct {
		name       string
		side       strategy.SignalSide
		config     Config
		entry      float64
		takeProfit float64
		stopLoss   float64
		wantSide   strategy.SignalSide
		wantStatus strategy.DiagnosticStatus
		wantReason string
	}{
		{
			name:       "long accepted",
			side:       strategy.SignalSideBuy,
			config:     Config{MinTakeProfitBps: 26, MinRewardRiskRatio: 1.25},
			entry:      100,
			takeProfit: 110,
			stopLoss:   95,
			wantSide:   strategy.SignalSideBuy,
			wantStatus: strategy.DiagnosticStatusPass,
			wantReason: "exit geometry accepted",
		},
		{
			name:       "short accepted",
			side:       strategy.SignalSideSell,
			config:     Config{MinTakeProfitBps: 26, MinRewardRiskRatio: 1.25},
			entry:      100,
			takeProfit: 90,
			stopLoss:   105,
			wantSide:   strategy.SignalSideSell,
			wantStatus: strategy.DiagnosticStatusPass,
			wantReason: "exit geometry accepted",
		},
		{
			name:       "take profit on wrong side",
			side:       strategy.SignalSideBuy,
			config:     Config{MinTakeProfitBps: 26, MinRewardRiskRatio: 1.25},
			entry:      100,
			takeProfit: 99,
			stopLoss:   95,
			wantSide:   strategy.SignalSideHold,
			wantStatus: strategy.DiagnosticStatusBlocked,
			wantReason: "take profit or stop loss is on the wrong side of entry",
		},
		{
			name:       "take profit below minimum",
			side:       strategy.SignalSideBuy,
			config:     Config{MinTakeProfitBps: 26},
			entry:      100,
			takeProfit: 100.2,
			stopLoss:   99,
			wantSide:   strategy.SignalSideHold,
			wantStatus: strategy.DiagnosticStatusBlocked,
			wantReason: "take profit distance below minimum",
		},
		{
			name:       "reward risk ratio below minimum",
			side:       strategy.SignalSideBuy,
			config:     Config{MinRewardRiskRatio: 1.25},
			entry:      100,
			takeProfit: 101,
			stopLoss:   99,
			wantSide:   strategy.SignalSideHold,
			wantStatus: strategy.DiagnosticStatusBlocked,
			wantReason: "reward-risk ratio below minimum",
		},
		{
			name:       "entry close missing",
			side:       strategy.SignalSideBuy,
			config:     Config{MinTakeProfitBps: 26, MinRewardRiskRatio: 1.25},
			takeProfit: 110,
			stopLoss:   95,
			wantSide:   strategy.SignalSideHold,
			wantStatus: strategy.DiagnosticStatusBlocked,
			wantReason: "exit geometry inputs missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshot(tt.side, nil)
			if tt.entry > 0 {
				got.Window.Values["close"] = strategy.NumericSeries{Latest: tt.entry}
			}
			if tt.side == strategy.SignalSideBuy {
				got.Window.Values["resistance_1"] = strategy.NumericSeries{Latest: tt.takeProfit}
			} else {
				got.Window.Values["support_1"] = strategy.NumericSeries{Latest: tt.takeProfit}
			}
			got.Window.Values["supertrend"] = strategy.NumericSeries{Latest: tt.stopLoss}

			result, err := New(tt.config).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != tt.wantSide {
				t.Fatalf("side = %q, want %q", result.Signal.Side, tt.wantSide)
			}
			check, ok := diagnostic(result.Analysis.Checks, "exit_geometry", tt.side)
			if !ok || check.Status != tt.wantStatus || check.Reason != tt.wantReason {
				t.Fatalf("exit geometry diagnostic = %#v, want status=%q reason=%q", check, tt.wantStatus, tt.wantReason)
			}
			if check.Values["min_take_profit_bps"] != formatFloat(tt.config.MinTakeProfitBps) ||
				check.Values["min_reward_risk_ratio"] != formatFloat(tt.config.MinRewardRiskRatio) {
				t.Fatalf("exit geometry thresholds = %#v", check.Values)
			}
			if tt.wantStatus == strategy.DiagnosticStatusPass && len(result.ExitRules) != 2 {
				t.Fatalf("exit rules = %d, want 2", len(result.ExitRules))
			}
			if tt.wantStatus == strategy.DiagnosticStatusBlocked && len(result.ExitRules) != 0 {
				t.Fatalf("exit rules = %d, want 0", len(result.ExitRules))
			}
		})
	}
}

func TestEvaluateClosesLongOnConfirmedShortSetup(t *testing.T) {
	item := New(Config{})
	currentPosition := &strategy.Position{
		Side: strategy.PositionSideLong,
		Size: 1,
	}
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideSell, nil), currentPosition)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideSell {
		t.Fatalf("side = %q, want sell", result.Signal.Side)
	}
}

func TestEvaluateDoesNotExitOnSingleThreeMinuteSupertrendCross(t *testing.T) {
	tests := []struct {
		name         string
		trendSide    strategy.SignalSide
		positionSide strategy.PositionSide
		close        float64
	}{
		{
			name:      "long close crosses below",
			trendSide: strategy.SignalSideBuy, positionSide: strategy.PositionSideLong,
			close: 94,
		},
		{
			name:      "short close crosses above",
			trendSide: strategy.SignalSideSell, positionSide: strategy.PositionSideShort,
			close: 106,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshot(tt.trendSide, nil)
			got.Window.Values["supertrend"] = strategy.NumericSeries{Latest: 95}
			got.Window.Values["close"] = strategy.NumericSeries{Latest: tt.close}
			position := &strategy.Position{Side: tt.positionSide, Size: 1}

			result, err := New(Config{}).Evaluate(context.Background(), got, position)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != strategy.SignalSideHold {
				t.Fatalf("side = %q, want hold", result.Signal.Side)
			}
		})
	}
}

func TestEvaluateExitsWhenTenAndFifteenMinuteTrendsReverse(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)
	position := &strategy.Position{Side: strategy.PositionSideLong, Size: 1}

	result, err := New(Config{}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideSell {
		t.Fatalf("side = %q, want sell", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "structure_invalidation", strategy.SignalSideSell)
	if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["10m"] != "aligned" || check.Values["15m"] != "aligned" {
		t.Fatalf("structure invalidation diagnostic = %#v", check)
	}
}

func TestEvaluateLetsStrongProtectedProfitRun(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	position := &strategy.Position{
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "2000",
		HighestPrice: "2200",
		LowestPrice:  "2000",
	}

	result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold for strong runner", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "profit_protection", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["state"] != "strong_runner" {
		t.Fatalf("profit protection diagnostic = %#v", check)
	}
}

func TestEvaluateExitsProtectedProfitWhenMomentumDecays(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_price_progress":   "stalled",
		"ma_window_spread_state": "flat",
	})
	got.Timeframes["5m"] = neutralTimeframe(strategy.SignalSideBuy)
	position := &strategy.Position{
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "2000",
		HighestPrice: "2030",
		LowestPrice:  "2000",
	}

	result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideSell || result.Signal.Reason != "protected profit momentum decayed" {
		t.Fatalf("signal = %#v, want protected-profit sell", result.Signal)
	}
	check, ok := diagnostic(result.Analysis.Checks, "profit_protection", strategy.SignalSideBuy)
	if !ok || check.Values["state"] != "weak_exit" || check.Values["decay_confirmations"] != "2" {
		t.Fatalf("profit protection diagnostic = %#v", check)
	}
}

func TestEvaluateDoesNotUseProfitDecayBeforeActivation(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_price_progress":   "stalled",
		"ma_window_spread_state": "flat",
	})
	got.Timeframes["5m"] = neutralTimeframe(strategy.SignalSideBuy)
	position := &strategy.Position{
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "2000",
		HighestPrice: "2008",
		LowestPrice:  "2000",
	}

	result, err := New(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold before profit activation", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "profit_protection", strategy.SignalSideBuy)
	if !ok || check.Values["state"] != "inactive" {
		t.Fatalf("profit protection diagnostic = %#v", check)
	}
}

func TestEvaluateRequiresSecondContinuationBarForShockBreakout(t *testing.T) {
	first := snapshot(strategy.SignalSideBuy, map[string]string{"breakout_window_quality": "confirmed"})
	first.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 1}
	first.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}

	result, err := New(Config{}).Evaluate(context.Background(), first, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("first continuation side = %q, want hold", result.Signal.Side)
	}
	firstCheck, ok := diagnostic(result.Analysis.Checks, "entry_trigger", strategy.SignalSideBuy)
	if !ok || firstCheck.Status != strategy.DiagnosticStatusBlocked ||
		firstCheck.Values["shock_impulse_signal"] != "true" ||
		firstCheck.Values["shock_impulse_stable_count"] != "1" ||
		firstCheck.Values["shock_second_bar_confirmed"] != "false" {
		t.Fatalf("first continuation diagnostic = %#v", firstCheck)
	}

	confirmed := snapshot(strategy.SignalSideBuy, map[string]string{"breakout_window_quality": "confirmed"})
	confirmed.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 2}
	confirmed.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	result, err = New(Config{}).Evaluate(context.Background(), confirmed, nil)
	if err != nil {
		t.Fatalf("Evaluate() confirmed error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("confirmed continuation side = %q, want buy", result.Signal.Side)
	}
	confirmedCheck, ok := diagnostic(result.Analysis.Checks, "entry_trigger", strategy.SignalSideBuy)
	if !ok || confirmedCheck.Status != strategy.DiagnosticStatusPass ||
		confirmedCheck.Values["shock_impulse_stable_count"] != "2" ||
		confirmedCheck.Values["shock_second_bar_confirmed"] != "true" {
		t.Fatalf("confirmed continuation diagnostic = %#v", confirmedCheck)
	}
	modeCheck, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || modeCheck.Status != strategy.DiagnosticStatusPass || modeCheck.Values["mode"] != "shock_breakout" ||
		modeCheck.Values["shock_higher_timeframes_aligned"] != "true" ||
		modeCheck.Values["shock_fake_risk_low"] != "true" ||
		modeCheck.Values["shock_momentum_confirmations"] != "4" {
		t.Fatalf("confirmed entry mode diagnostic = %#v", modeCheck)
	}

	stale := snapshot(strategy.SignalSideBuy, map[string]string{"breakout_window_quality": "confirmed"})
	stale.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 3}
	stale.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	result, err = New(Config{}).Evaluate(context.Background(), stale, nil)
	if err != nil {
		t.Fatalf("Evaluate() stale error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("stale continuation side = %q, want hold", result.Signal.Side)
	}
	staleCheck, ok := diagnostic(result.Analysis.Checks, "entry_trigger", strategy.SignalSideBuy)
	if !ok || staleCheck.Status != strategy.DiagnosticStatusBlocked ||
		staleCheck.Values["shock_impulse_stable_count"] != "3" ||
		staleCheck.Values["shock_second_bar_confirmed"] != "false" {
		t.Fatalf("stale continuation diagnostic = %#v", staleCheck)
	}
}

func TestEvaluateAllowsIndependentFreshTriggerOutsideSecondContinuationBar(t *testing.T) {
	for _, stableCount := range []int{1, 3} {
		t.Run(fmt.Sprintf("stable_%d", stableCount), func(t *testing.T) {
			got := snapshot(strategy.SignalSideBuy, map[string]string{"breakout_window_quality": "confirmed"})
			got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: stableCount}
			got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 0}

			result, err := New(Config{}).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != strategy.SignalSideBuy {
				t.Fatalf("side = %q, want buy", result.Signal.Side)
			}
			check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
			if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["mode"] != "trend_continuation" {
				t.Fatalf("entry mode diagnostic = %#v", check)
			}
		})
	}
}

func snapshot(side strategy.SignalSide, overrides map[string]string) strategy.Snapshot {
	window := window(side, overrides)
	return strategy.Snapshot{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		},
		Window: window,
		Indicator: strategy.IndicatorView{
			Values: map[string]string{},
		},
		Timeframes: map[string]strategy.TimeframeSnapshot{
			"3m":  timeframe(side, overrides),
			"5m":  timeframe(side, nil),
			"10m": timeframe(side, nil),
			"15m": timeframe(side, nil),
			"30m": timeframe(side, nil),
		},
		Health:    strategy.HealthView{OK: true},
		UpdatedAt: 2000,
	}
}

func timeframe(side strategy.SignalSide, overrides map[string]string) strategy.TimeframeSnapshot {
	return strategy.TimeframeSnapshot{
		Interval: "3m",
		Window:   window(side, overrides),
		Health:   strategy.HealthView{OK: true},
	}
}

func neutralTimeframe(side strategy.SignalSide) strategy.TimeframeSnapshot {
	return timeframe(side, map[string]string{
		"trend_window_bias":    "neutral",
		"supertrend_direction": "neutral",
		"ma_ribbon_state":      "neutral",
		"ma_window_bias":       "neutral",
		"ema_alignment":        "neutral",
		"macd_window_bias":     "neutral",
		"macd_momentum":        "flat",
	})
}

func window(side strategy.SignalSide, overrides map[string]string) strategy.IndicatorWindowView {
	signals := map[string]strategy.SignalSeries{
		"data_quality":              {Latest: "ok"},
		"trend_valid":               {Latest: "true"},
		"trend_quality":             {Latest: "strong"},
		"ma_ribbon_state":           {Latest: maRibbonStateForSide(side)},
		"ma_ribbon_phase":           {Latest: "trend"},
		"ma_window_bias":            {Latest: biasForSide(side)},
		"ma_window_tangled":         {Latest: "false"},
		"ma_window_spread_state":    {Latest: "rising"},
		"ma_window_slope_level":     {Latest: maSlopeForSide(side)},
		"ema_alignment":             {Latest: biasForSide(side)},
		"macd_window_bias":          {Latest: biasForSide(side)},
		"macd_momentum":             {Latest: biasForSide(side)},
		"macd_window_confirmed":     {Latest: "true"},
		"macd_window_acceleration":  {Latest: macdAccelerationForSide(side)},
		"price_volume_confirmation": {Latest: volumeForSide(side)},
		"volume_window_state":       {Latest: "expanding"},
		"supertrend_direction":      {Latest: directionForSide(side), StableCount: 2},
		"trend_signal_event":        {Latest: eventForSide(side)},
		"trend_window_bias":         {Latest: biasForSide(side)},
		"trend_price_progress":      {Latest: "advancing"},
	}
	if side == strategy.SignalSideBuy {
		signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 2}
		signals["pump_window_fake_risk"] = strategy.SignalSeries{Latest: "low"}
	} else {
		signals["dump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 2}
		signals["dump_window_fake_risk"] = strategy.SignalSeries{Latest: "low"}
	}
	for key, value := range overrides {
		signals[key] = strategy.SignalSeries{Latest: value}
	}
	return strategy.IndicatorWindowView{
		OpenTime: 1000,
		Values: map[string]strategy.NumericSeries{
			"resistance_1":     {Latest: 110},
			"support_1":        {Latest: 90},
			"supertrend":       {Latest: 95},
			"trend_signal_age": {Latest: 0},
		},
		Signals: signals,
	}
}

func volumeForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "confirm_up"
	}
	return "confirm_down"
}

func maRibbonStateForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "bullish_fan"
	}
	return "bearish_fan"
}

func maSlopeForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "rising"
	}
	return "falling"
}

func macdAccelerationForSide(side strategy.SignalSide) string {
	if side == strategy.SignalSideBuy {
		return "rising"
	}
	return "falling"
}
