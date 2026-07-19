package supertrend

import (
	"context"
	"fmt"
	"math"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func newTestStrategy(config Config) *Strategy {
	config.EntryThreshold = 0.01
	return New(config)
}

func TestEvaluateReturnsBuyWhenLongSetupConfirmed(t *testing.T) {
	item := newTestStrategy(Config{})
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, nil), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	if math.Abs(result.Signal.Score-0.15) > 1e-9 {
		t.Fatalf("score = %v, want 0.15 for a late fully aligned setup", result.Signal.Score)
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
	modeCheck, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
	if !ok || modeCheck.Values["mode"] != "supertrend_signal" || modeCheck.Values["trigger_sources"] != "supertrend_flip" || modeCheck.Values["trigger_source_count"] != "1" {
		t.Fatalf("buy entry mode diagnostic = %#v", modeCheck)
	}
	qualityCheck, ok := diagnostic(result.Analysis.Checks, "entry_quality", strategy.SignalSideBuy)
	if !ok || qualityCheck.Values["regime"] != "trend" || qualityCheck.Values["five_minute_state"] != "aligned" || qualityCheck.Values["momentum_confirmations"] != "4" {
		t.Fatalf("buy quality diagnostic = %#v", qualityCheck)
	}
	featureCheck, ok := diagnostic(result.Analysis.Checks, "entry_feature_snapshot", strategy.SignalSideBuy)
	if !ok || featureCheck.Status != strategy.DiagnosticStatusInfo || featureCheck.Score != 0 {
		t.Fatalf("buy entry feature diagnostic = %#v", featureCheck)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "exit_geometry", strategy.SignalSideBuy); ok {
		t.Fatalf("disabled exit geometry diagnostic = %#v, want absent", check)
	}
}

func TestEntryFeatureSnapshotCapturesAvailableValuesWithoutAffectingDecision(t *testing.T) {
	item := newTestStrategy(Config{})
	input := snapshot(strategy.SignalSideBuy, nil)
	input.Window.Values["adx14"] = strategy.NumericSeries{Latest: 27.5, Previous: 24.25}
	input.Window.Values["dynamic_swing_vwap_distance_pct"] = strategy.NumericSeries{Latest: -0.35, Previous: -0.2}
	input.Window.Values["market_strength_score"] = strategy.NumericSeries{Latest: 72.5, Previous: 68.25}
	input.Window.Values["market_risk_adjusted_strength_score"] = strategy.NumericSeries{Latest: 62.5, Previous: 58.25}
	input.Window.Values["market_directional_capability_score"] = strategy.NumericSeries{Latest: 42.5, Previous: 38.25}
	input.Window.Values["market_data_confidence"] = strategy.NumericSeries{Latest: 90.9, Previous: 86.4}
	input.Window.Signals["stc_zone"] = strategy.SignalSeries{Latest: "oversold"}
	input.Window.Signals["market_score_version"] = strategy.SignalSeries{Latest: "market-capability.v2.7"}
	input.Window.Signals["market_direction_bias"] = strategy.SignalSeries{Latest: "bull"}

	result, err := item.Evaluate(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	check, ok := diagnostic(result.Analysis.Checks, "entry_feature_snapshot", strategy.SignalSideBuy)
	if !ok {
		t.Fatal("entry feature snapshot diagnostic missing")
	}
	if check.Values["adx14"] != "27.5" || check.Values["adx14_previous"] != "24.25" {
		t.Fatalf("adx snapshot = %#v", check.Values)
	}
	if check.Values["dynamic_swing_vwap_distance_pct"] != "-0.35" || check.Values["stc_zone"] != "oversold" {
		t.Fatalf("feature snapshot = %#v", check.Values)
	}
	if check.Values["market_strength_score"] != "72.5" || check.Values["market_directional_capability_score"] != "42.5" ||
		check.Values["market_risk_adjusted_strength_score"] != "62.5" ||
		check.Values["market_directional_capability_score_previous"] != "38.25" ||
		check.Values["market_data_confidence"] != "90.9" || check.Values["market_score_version"] != "market-capability.v2.7" ||
		check.Values["market_direction_bias"] != "bull" {
		t.Fatalf("market capability snapshot = %#v", check.Values)
	}
	if _, exists := check.Values["missing_feature"]; exists {
		t.Fatalf("missing feature unexpectedly captured: %#v", check.Values)
	}
	if result.Signal.Side != strategy.SignalSideBuy || math.Abs(result.Signal.Score-0.15) > 1e-9 {
		t.Fatalf("signal changed by diagnostic: side=%q score=%v", result.Signal.Side, result.Signal.Score)
	}
}

func TestEntryQualityScoreOrdersLateAndEarlySignals(t *testing.T) {
	tests := []struct {
		name       string
		regime     string
		fiveMinute string
		momentum   string
		volatility string
		want       float64
	}{
		{name: "early reversal", regime: "macro_blocked", fiveMinute: "blocking", momentum: "1", volatility: "normal", want: 0.75},
		{name: "neutral", regime: "neutral", fiveMinute: "neutral", momentum: "2", volatility: "normal", want: 0.50},
		{name: "late crowded trend", regime: "trend", fiveMinute: "aligned", momentum: "4", volatility: "contracting", want: 0.10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, values := entryQualityScore(tt.regime, tt.fiveMinute, tt.momentum, tt.volatility)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("quality = %v values=%#v, want %v", got, values, tt.want)
			}
		})
	}
}

func TestStandardEntryTriggerSourcesRecordsAllMatchingSourcesInStableOrder(t *testing.T) {
	got := window(strategy.SignalSideBuy, map[string]string{
		"ma_window_cross_event": "golden",
		"smc_window_bos_recent": "true",
		"smc_window_bias":       "bull",
	})
	got.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", Changed: true, StableCount: 1}
	got.Values["low"] = strategy.NumericSeries{Previous: 94}
	got.Values["close"] = strategy.NumericSeries{Previous: 96}
	got.Values["supertrend"] = strategy.NumericSeries{Latest: 95, Previous: 95}
	got.Values["ma_window_cross_age"] = strategy.NumericSeries{Latest: 0}
	got.Values["smc_window_bos_age"] = strategy.NumericSeries{Latest: 0}

	sources := standardEntryTriggerSources(got, strategy.SignalSideBuy)
	if got, want := fmt.Sprint(sources), "[wick_reclaim supertrend_flip]"; got != want {
		t.Fatalf("trigger sources = %s, want %s", got, want)
	}
	values := entryTriggerValues(got, strategy.SignalSideBuy, sources)
	if values["standard_trigger_sources"] != "wick_reclaim,supertrend_flip" ||
		values["standard_trigger_count"] != "2" {
		t.Fatalf("trigger values = %#v", values)
	}
	if got, want := fmt.Sprint(supertrendEntryTriggerSources(sources)), "[wick_reclaim supertrend_flip]"; got != want {
		t.Fatalf("supertrend trigger sources = %s, want %s", got, want)
	}
}

func TestEntryIgnoresSMCBOSWithoutSupertrendSignal(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"smc_window_bos_recent": "true",
		"smc_window_bias":       "bull",
	})
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	got.Window.Values["smc_window_bos_age"] = strategy.NumericSeries{Latest: 0}
	got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: 1}
	got.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 2}

	item := newTestStrategy(Config{})
	decision := item.entry(got, strategy.SignalSideBuy)
	if !decision.blocked {
		t.Fatalf("opening decision = %#v, want blocked", decision)
	}
	check, ok := diagnostic(decision.checks, "entry_trigger", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Reason != "buy supertrend trigger missing" ||
		check.Values["standard_trigger_sources"] != "" ||
		check.Values["standard_trigger_authorized"] != "false" ||
		check.Values["supertrend_trigger_count"] != "0" {
		t.Fatalf("opening trigger diagnostic = %#v", check)
	}

	if reversal := item.reversalEntry(got, strategy.SignalSideBuy); !reversal.blocked {
		t.Fatalf("reversal decision = %#v, want blocked without supertrend signal", reversal)
	}
}

func TestSupertrendEntryTriggerSourcesRejectsAuxiliarySources(t *testing.T) {
	tests := []struct {
		name    string
		sources []string
		want    string
	}{
		{name: "none", want: "[]"},
		{name: "smc only", sources: []string{"smc_bos"}, want: "[]"},
		{name: "trend event", sources: []string{"trend_event"}, want: "[]"},
		{name: "trend and smc", sources: []string{"trend_event", "smc_bos"}, want: "[]"},
		{name: "wick reclaim", sources: []string{"wick_reclaim"}, want: "[wick_reclaim]"},
		{name: "supertrend flip", sources: []string{"supertrend_flip", "ma_cross"}, want: "[supertrend_flip]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmt.Sprint(supertrendEntryTriggerSources(tt.sources)); got != tt.want {
				t.Fatalf("supertrendEntryTriggerSources(%v) = %v, want %v", tt.sources, got, tt.want)
			}
		})
	}
}

func TestEvaluateTreatsOppositeMACDBiasAsNonBlocking(t *testing.T) {
	item := newTestStrategy(Config{})
	result, err := item.Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"macd_window_bias": "bear",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "macd", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusInfo {
		t.Fatalf("MACD diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsOppositeMACDMomentumEnumAsNonBlocking(t *testing.T) {
	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"macd_window_bias": "neutral",
		"macd_momentum":    "expanding_bear",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "macd", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusInfo {
		t.Fatalf("MACD diagnostic = %#v", check)
	}
}

func TestEvaluateRequiresSupertrendDirectionSignal(t *testing.T) {
	result, err := newTestStrategy(Config{EntryThreshold: 0.3}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
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
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "entry_trigger", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["supertrend_trigger_count"] != "0" {
		t.Fatalf("entry trigger diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsTangledMAWindowAsScoreOnly(t *testing.T) {
	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
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

func TestEvaluateTreatsInsufficientMomentumEnergyAsNonBlocking(t *testing.T) {
	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
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
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "momentum_energy", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["confirmations"] != "0" {
		t.Fatalf("momentum energy diagnostic = %#v", check)
	}
}

func TestEvaluateObservesSTCWithoutChangingDecision(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["stc"] = strategy.NumericSeries{Latest: 82, Previous: 79}
	got.Window.Signals["stc_direction"] = strategy.SignalSeries{Latest: "rising"}
	got.Window.Signals["stc_zone"] = strategy.SignalSeries{Latest: "overbought"}
	got.Window.Signals["stc_cross"] = strategy.SignalSeries{Latest: "up_75"}

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "stc", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["value"] != "82" || check.Values["direction"] != "rising" || check.Values["zone"] != "overbought" || check.Values["cross"] != "up_75" {
		t.Fatalf("stc diagnostic = %#v", check)
	}
}

func TestEvaluateVetoesDirectionalSTC25Cross(t *testing.T) {
	for _, test := range []struct {
		name  string
		side  strategy.SignalSide
		cross string
	}{
		{name: "buy up 25", side: strategy.SignalSideBuy, cross: "up_25"},
		{name: "sell down 25", side: strategy.SignalSideSell, cross: "down_25"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := snapshot(test.side, nil)
			got.Window.Values["stc"] = strategy.NumericSeries{Latest: 30, Previous: 20}
			got.Window.Signals["stc_cross"] = strategy.SignalSeries{Latest: test.cross}
			result, err := newTestStrategy(Config{EntryThreshold: 0.3}).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != strategy.SignalSideHold {
				t.Fatalf("side = %q, want hold", result.Signal.Side)
			}
			check, ok := diagnostic(result.Analysis.Checks, "stc", test.side)
			if !ok || check.Status != strategy.DiagnosticStatusBlocked || check.Values["entry_veto"] != "true" {
				t.Fatalf("stc diagnostic = %#v", check)
			}
		})
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

func TestEvaluateTreatsShortTimeframeOppositionAsNonBlocking(t *testing.T) {
	item := newTestStrategy(Config{})
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["5m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := item.Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	if check, ok := diagnostic(result.Analysis.Checks, "timeframes", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusInfo {
		t.Fatalf("timeframes diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsTenMinutePullbackAsBackground(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := newTestStrategy(Config{EntryThreshold: 0.3, MaxBlockingTimeframes: 1}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["state"] != "pullback" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateAllowsHigherTimeframeTransition(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["state"] != "transition" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateAllowsTenMinuteTrendToStabilize(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	tenMinute := got.Timeframes["10m"]
	tenMinute.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 1}
	got.Timeframes["10m"] = tenMinute

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["state"] != "stabilizing" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsUnconfirmedTrendAsScoreOnlyInContinuation(t *testing.T) {
	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
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
	if !ok || modeCheck.Values["mode"] != "supertrend_signal" {
		t.Fatalf("entry mode diagnostic = %#v", modeCheck)
	}
}

func TestEvaluateTreatsLocalTrendReversalRiskAsNonBlocking(t *testing.T) {
	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_window_reversal_risk": "true",
	}), nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	trendCheck, ok := diagnostic(result.Analysis.Checks, "trend", strategy.SignalSideBuy)
	if !ok || trendCheck.Status != strategy.DiagnosticStatusInfo || trendCheck.Score != 0 {
		t.Fatalf("trend diagnostic = %#v", trendCheck)
	}
}

func TestEvaluateRejectsStaleTrendWithoutContinuationOpportunity(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 2}
	got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "false"}
	got.Window.Signals["supertrend_direction"] = strategy.SignalSeries{Latest: "up", StableCount: 2}

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
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

func TestEvaluateTreatsCountertrendHigherTimeframesAsNonBlocking(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["30m"] = timeframe(strategy.SignalSideSell, nil)

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "higher_timeframe_regime", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["15m"] != "blocking" || check.Values["30m"] != "blocking" {
		t.Fatalf("higher timeframe diagnostic = %#v", check)
	}
}

func TestEvaluateTreatsFiveMinuteDirectionAsBackground(t *testing.T) {
	got := snapshot(strategy.SignalSideSell, nil)
	got.Timeframes["5m"] = timeframe(strategy.SignalSideBuy, nil)

	blocked, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() blocked error = %v", err)
	}
	if blocked.Signal.Side != strategy.SignalSideSell {
		t.Fatalf("blocked side = %q, want sell", blocked.Signal.Side)
	}
	check, ok := diagnostic(blocked.Analysis.Checks, "pullback_resolution", strategy.SignalSideSell)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["5m"] != "blocking" {
		t.Fatalf("pullback diagnostic = %#v", check)
	}

	got.Timeframes["5m"] = timeframe(strategy.SignalSideSell, nil)
	allowed, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
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

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
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

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideBuy {
		t.Fatalf("side = %q, want buy", result.Signal.Side)
	}
}

func TestEvaluateReturnsSellWhenShortTrendAdvances(t *testing.T) {
	item := newTestStrategy(Config{})
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

			result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, nil)
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

func TestEvaluateBuildsAdaptiveQuoteAndATRExitRules(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Window.Values["close"] = strategy.NumericSeries{Latest: 2000}
	got.Window.Values["atr_pct14"] = strategy.NumericSeries{Latest: 0.30}

	result, err := newTestStrategy(Config{ExitMode: ExitModeAdaptive}).Evaluate(context.Background(), got, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	stop, ok := exitRule(result.ExitRules, strategy.ExitReasonStopLoss)
	if !ok || stop.TriggerPrice != "1986" {
		t.Fatalf("adaptive hard stop = %#v, want 1986", stop)
	}
	trailing, ok := exitRule(result.ExitRules, strategy.ExitReasonTrailingStop)
	if !ok {
		t.Fatalf("adaptive trailing rule missing: %#v", result.ExitRules)
	}
	want := map[string]string{
		"trail_pct":                   "0.3",
		"reference_price":             "2000",
		"profit_guard_activation_bps": "50",
		"profit_guard_floor_bps":      "40",
		"adaptive_trailing":           "true",
		"runner_activation_bps":       "150",
		"runner_trail_pct":            "0.525",
		"volatility_state":            "neutral",
		"atr_pct14":                   "0.3",
	}
	for key, value := range want {
		if trailing.Metadata[key] != value {
			t.Fatalf("adaptive trailing metadata[%q] = %q, want %q; metadata=%#v", key, trailing.Metadata[key], value, trailing.Metadata)
		}
	}
	if _, ok := exitRule(result.ExitRules, strategy.ExitReasonTakeProfit); ok {
		t.Fatalf("adaptive exit rules = %#v, want no fixed take profit", result.ExitRules)
	}
}

func TestAdaptiveExitUsesMicroTargetForProtectionAndKeepsUnlimitedRunner(t *testing.T) {
	for _, state := range []string{"contracting", "normal", "expanding"} {
		t.Run(state, func(t *testing.T) {
			got := snapshot(strategy.SignalSideBuy, map[string]string{
				"volatility_window_state": state,
			})
			got.Window.Values["atr_pct14"] = strategy.NumericSeries{Latest: 0.30}
			params := newTestStrategy(Config{ExitMode: ExitModeAdaptive}).adaptiveExitParameters(got, 2000)

			if params.activationBps != 50 {
				t.Fatalf("activation bps = %v, want 50", params.activationBps)
			}
			if params.decayActivationBps != 100 {
				t.Fatalf("decay activation bps = %v, want 100", params.decayActivationBps)
			}
			if params.runnerActivationBps != 150 {
				t.Fatalf("runner activation bps = %v, want 150", params.runnerActivationBps)
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
			result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5, MaxStopLossBps: 50}).Evaluate(context.Background(), got, nil)
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
	result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(
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

			result, err := newTestStrategy(tt.config).Evaluate(context.Background(), got, nil)
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

func TestExitRulesApplyTakeProfitCostFloor(t *testing.T) {
	tests := []struct {
		name       string
		side       strategy.SignalSide
		structural float64
		want       float64
	}{
		{name: "long raises close target", side: strategy.SignalSideBuy, structural: 100.1, want: 100.2},
		{name: "long retains farther target", side: strategy.SignalSideBuy, structural: 101, want: 101},
		{name: "short lowers close target", side: strategy.SignalSideSell, structural: 99.9, want: 99.8},
		{name: "short retains farther target", side: strategy.SignalSideSell, structural: 99, want: 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshot(tt.side, nil)
			got.Window.Values["close"] = strategy.NumericSeries{Latest: 100}
			if tt.side == strategy.SignalSideBuy {
				got.Window.Values["resistance_1"] = strategy.NumericSeries{Latest: tt.structural}
			} else {
				got.Window.Values["support_1"] = strategy.NumericSeries{Latest: tt.structural}
			}
			rules := newTestStrategy(Config{TakeProfitCostFloorBps: 20}).exitRules(got, tt.side)
			price, ok := exitRulePrice(rules, strategy.ExitReasonTakeProfit)
			if !ok || math.Abs(price-tt.want) > 1e-9 {
				t.Fatalf("take profit = %v, %v; want %v", price, ok, tt.want)
			}
		})
	}
}

func TestEvaluateClosesLongOnConfirmedShortSetup(t *testing.T) {
	item := newTestStrategy(Config{})
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

			result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, position)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != strategy.SignalSideHold {
				t.Fatalf("side = %q, want hold", result.Signal.Side)
			}
		})
	}
}

func TestEvaluateTreatsTenAndFifteenMinuteReversalAsExitBackground(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, nil)
	got.Timeframes["10m"] = timeframe(strategy.SignalSideSell, nil)
	got.Timeframes["15m"] = timeframe(strategy.SignalSideSell, nil)
	position := &strategy.Position{Side: strategy.PositionSideLong, Size: 1}

	result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold without opposite supertrend trigger", result.Signal.Side)
	}
	check, ok := diagnostic(result.Analysis.Checks, "structure_invalidation", strategy.SignalSideSell)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["10m"] != "aligned" || check.Values["15m"] != "aligned" {
		t.Fatalf("structure invalidation diagnostic = %#v", check)
	}
}

func TestAdaptiveDirectionFailureDoesNotIndependentlyExit(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{
		"trend_window_bias": "bear",
		"ma_window_bias":    "bear",
	})
	got.Window.Values["close"] = strategy.NumericSeries{Latest: 1990}
	position := &strategy.Position{
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "2000",
		HighestPrice: "2000",
		LowestPrice:  "1990",
	}

	result, err := newTestStrategy(Config{ExitMode: ExitModeAdaptive}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("signal = %#v, want hold without opposite supertrend trigger", result.Signal)
	}
	check, ok := diagnostic(result.Analysis.Checks, "direction_invalidation", strategy.SignalSideBuy)
	if !ok || check.Status != strategy.DiagnosticStatusInfo || check.Values["state"] != "invalidated" || check.Values["confirmations"] != "2" {
		t.Fatalf("direction invalidation diagnostic = %#v", check)
	}
}

func TestAdaptiveExitDoesNotLeaveOnOneLocalWarning(t *testing.T) {
	got := snapshot(strategy.SignalSideBuy, map[string]string{"trend_window_reversal_risk": "true"})
	got.Window.Values["close"] = strategy.NumericSeries{Latest: 1990}
	position := &strategy.Position{
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "2000",
		HighestPrice: "2000",
		LowestPrice:  "1990",
	}

	result, err := newTestStrategy(Config{ExitMode: ExitModeAdaptive}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("side = %q, want hold on one local warning", result.Signal.Side)
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

	result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
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

func TestEvaluateTreatsProtectedProfitDecayAsExitBackground(t *testing.T) {
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

	result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Signal.Side != strategy.SignalSideHold {
		t.Fatalf("signal = %#v, want hold without opposite supertrend trigger", result.Signal)
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

	result, err := newTestStrategy(Config{ExitMode: ExitModeTrailing, TrailingStopPct: 0.5}).Evaluate(context.Background(), got, position)
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

func TestEvaluateAllowsIndependentFreshTriggerOutsideSecondContinuationBar(t *testing.T) {
	for _, stableCount := range []int{1, 3} {
		t.Run(fmt.Sprintf("stable_%d", stableCount), func(t *testing.T) {
			got := snapshot(strategy.SignalSideBuy, map[string]string{"breakout_window_quality": "confirmed"})
			got.Window.Signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true", StableCount: stableCount}
			got.Window.Values["trend_signal_age"] = strategy.NumericSeries{Latest: 0}

			result, err := newTestStrategy(Config{}).Evaluate(context.Background(), got, nil)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Signal.Side != strategy.SignalSideBuy {
				t.Fatalf("side = %q, want buy", result.Signal.Side)
			}
			check, ok := diagnostic(result.Analysis.Checks, "entry_mode", strategy.SignalSideBuy)
			if !ok || check.Status != strategy.DiagnosticStatusPass || check.Values["mode"] != "supertrend_signal" {
				t.Fatalf("entry mode diagnostic = %#v", check)
			}
		})
	}
}

func snapshot(side strategy.SignalSide, overrides map[string]string) strategy.Snapshot {
	window := window(side, overrides)
	direction := window.Signals["supertrend_direction"]
	direction.Changed = true
	direction.StableCount = 1
	window.Signals["supertrend_direction"] = direction
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
