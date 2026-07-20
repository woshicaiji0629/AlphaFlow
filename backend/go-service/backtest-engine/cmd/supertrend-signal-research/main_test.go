package main

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func singlePositionConfigForTest() signalresearch.SinglePositionConfig {
	return signalresearch.SinglePositionConfig{
		InitialEquity: 10000, MarginQuote: 100, Leverage: 100,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: 12 * time.Hour, CooldownBars: 2, FeeRate: 0.0006, SlippageBps: 2,
	}
}

func TestSupertrendFlipSide(t *testing.T) {
	window := strategy.IndicatorWindowView{Signals: map[string]strategy.SignalSeries{
		"supertrend_flip":          {Latest: "up"},
		"sma_atr_supertrend_flip":  {Latest: "up"},
		"adaptive_supertrend_flip": {Latest: "down"},
		"ai_supertrend_flip":       {Latest: "none"},
	}}
	tests := []struct {
		key  string
		want strategy.SignalSide
		ok   bool
	}{
		{key: "supertrend_flip", want: strategy.SignalSideBuy, ok: true},
		{key: "sma_atr_supertrend_flip", want: strategy.SignalSideBuy, ok: true},
		{key: "adaptive_supertrend_flip", want: strategy.SignalSideSell, ok: true},
		{key: "ai_supertrend_flip", want: strategy.SignalSideHold, ok: false},
		{key: "missing", want: strategy.SignalSideHold, ok: false},
	}
	for _, test := range tests {
		got, ok := supertrendFlipSide(window, test.key)
		if got != test.want || ok != test.ok {
			t.Fatalf("key=%s side=%s ok=%t, want side=%s ok=%t", test.key, got, ok, test.want, test.ok)
		}
	}
}

func TestBuildSupertrendComparisonReplays(t *testing.T) {
	pullbackConfig := signalresearch.PullbackConfig{
		TouchDistancePct: 0.15, ResumeBars: 3, MaxArmedBars: 10, MinVolumeRatio: 1, CooldownBars: 20,
	}
	items, err := buildSupertrendComparisonReplays(singlePositionConfigForTest(), pullbackConfig, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[0].name != "standard" || items[1].name != "sma_atr" || items[2].name != "adaptive" || items[3].name != "ai" {
		t.Fatalf("items=%#v", items)
	}
	for _, item := range items {
		if item.pullbackDetector == nil || item.flipReplay == nil || item.volumeLooseReplay == nil || item.volumeStrongReplay == nil || item.pullbackReplay == nil || item.combinedReplay == nil || item.deferredReplay == nil || item.exhaustLooseReplay == nil || item.exhaustStrictReplay == nil || item.macroVetoReplay == nil || item.waitOneReplay == nil || item.retestReplay == nil || item.exhaustDeferredReplay == nil || item.followthroughReplay == nil {
			t.Fatalf("incomplete comparison item=%#v", item)
		}
	}
}

func TestExhaustionAndMacroVeto(t *testing.T) {
	snapshot := strategy.Snapshot{Timeframes: map[string]strategy.TimeframeSnapshot{
		"10m": {Indicator: strategy.IndicatorView{Signals: map[string]string{"supertrend_direction": "down"}}},
		"15m": {Indicator: strategy.IndicatorView{NumericValues: map[string]float64{
			"adx14": 36, "di_plus14": 30, "di_minus14": 20, "macd_hist_delta": -0.2,
		}}},
	}}
	if !exhaustionBlocked(snapshot, strategy.SignalSideBuy, 35, 8) || !macroMomentumBlocked(snapshot, strategy.SignalSideBuy) {
		t.Fatal("expected long signal to be blocked by exhaustion and macro momentum")
	}
	if exhaustionBlocked(snapshot, strategy.SignalSideSell, 35, 8) || macroMomentumBlocked(snapshot, strategy.SignalSideSell) {
		t.Fatal("short signal should not be blocked")
	}
}

func TestConfirmationPendingModes(t *testing.T) {
	signal := strategy.Snapshot{Current: marketmodel.Kline{Open: "99", High: "101", Low: "98", Close: "100"}}
	pending, err := newConfirmationPending(signal, strategy.SignalSideBuy)
	if err != nil {
		t.Fatal(err)
	}
	pending.age = 1
	next := strategy.Snapshot{
		Current: marketmodel.Kline{High: "101.5", Low: "99", Close: "100.5"},
		Indicator: strategy.IndicatorView{NumericValues: map[string]float64{
			"macd_hist": 0.1, "macd_hist_delta": 0.1, "squeeze_momentum": 1, "squeeze_momentum_delta": 0.1,
		}},
	}
	allowed, expired, err := pending.waitOneAllows(next)
	if err != nil || !allowed || !expired {
		t.Fatalf("wait allowed=%t expired=%t err=%v", allowed, expired, err)
	}
	retest := pending
	retest.retested = false
	allowed, expired, err = retest.retestAllows(next)
	if err != nil || !allowed || expired {
		t.Fatalf("retest allowed=%t expired=%t err=%v", allowed, expired, err)
	}
}

func TestExhaustReaccelerationPending(t *testing.T) {
	signal := strategy.Snapshot{Current: marketmodel.Kline{Open: "99", High: "101", Low: "98", Close: "100"}}
	pending, err := newConfirmationPending(signal, strategy.SignalSideBuy)
	if err != nil {
		t.Fatal(err)
	}
	pending.age = 1
	macdResume := strategy.Snapshot{
		Current: marketmodel.Kline{Close: "100"},
		Timeframes: map[string]strategy.TimeframeSnapshot{"15m": {
			Indicator: strategy.IndicatorView{NumericValues: map[string]float64{"macd_hist_delta": 0.1}},
		}},
	}
	allowed, expired, err := pending.exhaustReaccelerationAllows(macdResume)
	if err != nil || !allowed || expired {
		t.Fatalf("MACD resume allowed=%t expired=%t err=%v", allowed, expired, err)
	}
	breakout := strategy.Snapshot{
		Current: marketmodel.Kline{Close: "102"},
		Indicator: strategy.IndicatorView{NumericValues: map[string]float64{
			"squeeze_momentum": 1, "squeeze_momentum_delta": 0.1,
		}},
	}
	allowed, expired, err = pending.exhaustReaccelerationAllows(breakout)
	if err != nil || !allowed || expired {
		t.Fatalf("breakout allowed=%t expired=%t err=%v", allowed, expired, err)
	}
	invalid := strategy.Snapshot{
		Current:   marketmodel.Kline{Close: "99"},
		Indicator: strategy.IndicatorView{Signals: map[string]string{"structure_event": "choch_down"}},
	}
	allowed, expired, err = pending.exhaustReaccelerationAllows(invalid)
	if err != nil || allowed || !expired {
		t.Fatalf("invalid allowed=%t expired=%t err=%v", allowed, expired, err)
	}
	pending.age = 10
	allowed, expired, err = pending.exhaustReaccelerationAllows(strategy.Snapshot{Current: marketmodel.Kline{Close: "100"}})
	if err != nil || allowed || !expired {
		t.Fatalf("timeout allowed=%t expired=%t err=%v", allowed, expired, err)
	}
}

func TestVolumeAllowsFlip(t *testing.T) {
	tests := []struct {
		name             string
		side             strategy.SignalSide
		ratio            float64
		confirmation     string
		minRatio         float64
		allowDirectional bool
		want             bool
	}{
		{name: "loose ratio", side: strategy.SignalSideBuy, ratio: 1.0, minRatio: 1.0, allowDirectional: true, want: true},
		{name: "loose directional", side: strategy.SignalSideBuy, ratio: 0.8, confirmation: "confirm_up", minRatio: 1.0, allowDirectional: true, want: true},
		{name: "strong rejects directional only", side: strategy.SignalSideBuy, ratio: 1.1, confirmation: "confirm_up", minRatio: 1.2},
		{name: "strong ratio", side: strategy.SignalSideSell, ratio: 1.2, minRatio: 1.2, want: true},
		{name: "buy divergence blocks volume", side: strategy.SignalSideBuy, ratio: 2, confirmation: "divergence_bear", minRatio: 1.0, allowDirectional: true},
		{name: "sell divergence blocks volume", side: strategy.SignalSideSell, ratio: 2, confirmation: "divergence_bull", minRatio: 1.0, allowDirectional: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := strategy.Snapshot{Indicator: strategy.IndicatorView{
				NumericValues: map[string]float64{"volume_ratio20": test.ratio},
				Signals:       map[string]string{"price_volume_confirmation": test.confirmation},
			}}
			if got := volumeAllowsFlip(snapshot, test.side, test.minRatio, test.allowDirectional); got != test.want {
				t.Fatalf("volumeAllowsFlip()=%t want=%t", got, test.want)
			}
		})
	}
}

func TestCombinedEntrySide(t *testing.T) {
	tests := []struct {
		name         string
		flipSide     strategy.SignalSide
		hasFlip      bool
		pullbackSide strategy.SignalSide
		want         strategy.SignalSide
		conflict     bool
	}{
		{name: "pullback only", pullbackSide: strategy.SignalSideBuy, want: strategy.SignalSideBuy},
		{name: "flip only", flipSide: strategy.SignalSideSell, hasFlip: true, want: strategy.SignalSideSell},
		{name: "same side deduplicated", flipSide: strategy.SignalSideBuy, hasFlip: true, pullbackSide: strategy.SignalSideBuy, want: strategy.SignalSideBuy},
		{name: "opposite conflict", flipSide: strategy.SignalSideBuy, hasFlip: true, pullbackSide: strategy.SignalSideSell, want: strategy.SignalSideHold, conflict: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, conflict := combinedEntrySide(test.flipSide, test.hasFlip, test.pullbackSide)
			if got != test.want || conflict != test.conflict {
				t.Fatalf("side=%s conflict=%t, want side=%s conflict=%t", got, conflict, test.want, test.conflict)
			}
		})
	}
}

func TestRegimeDecisionReason(t *testing.T) {
	if got := regimeDecisionReason(strategy.SignalSideBuy, marketregime.Result{AllowLong: true}); got != "permitted" {
		t.Fatalf("permitted reason=%q", got)
	}
	if got := regimeDecisionReason(strategy.SignalSideBuy, marketregime.Result{AllowShort: true}); got != "v4_countertrend_signal" {
		t.Fatalf("countertrend reason=%q", got)
	}
	regime := marketregime.Result{Reasons: []string{"v4_compression_locked", "v4_state_pending"}}
	if got := regimeDecisionReason(strategy.SignalSideBuy, regime); got != "v4_state_pending" {
		t.Fatalf("blocked reason=%q", got)
	}
	v5Regime := marketregime.Result{Reasons: []string{"v5_breakout_volume_weak"}}
	if got := regimeDecisionReason(strategy.SignalSideBuy, v5Regime); got != "v5_breakout_volume_weak" {
		t.Fatalf("v5 blocked reason=%q", got)
	}
	v6Regime := marketregime.Result{Reasons: []string{"v6_breakout_width_weak"}}
	if got := regimeDecisionReason(strategy.SignalSideBuy, v6Regime); got != "v6_breakout_width_weak" {
		t.Fatalf("v6 blocked reason=%q", got)
	}
}

func TestCompressionBlocked(t *testing.T) {
	if !compressionBlocked(&marketregime.Result{State: marketregime.StateChopLock}) {
		t.Fatal("chop lock should arm a deferred flip")
	}
	if !compressionBlocked(&marketregime.Result{Reasons: []string{"v6_breakout_width_weak"}}) {
		t.Fatal("weak breakout width should arm a deferred flip")
	}
	if compressionBlocked(&marketregime.Result{Reasons: []string{"v4_direction_unclear"}}) {
		t.Fatal("unrelated block should not arm a deferred flip")
	}
}

func TestBuildBreakoutComparison(t *testing.T) {
	comparison, err := buildBreakoutComparison(singlePositionConfigForTest(), true)
	if err != nil {
		t.Fatal(err)
	}
	if comparison == nil || comparison.platformReplay == nil || comparison.compressionReplay == nil || comparison.combinedReplay == nil {
		t.Fatalf("comparison=%#v", comparison)
	}
}

func TestResearchEntryRegimePermitsOnlyEventSide(t *testing.T) {
	regime := researchEntryRegime(&marketregime.Result{Version: marketregime.VersionV6, State: marketregime.StateChopLock}, strategy.SignalSideSell)
	if regime.State != marketregime.StateTrendArmed || regime.Direction != marketregime.DirectionShort || regime.AllowLong || !regime.AllowShort {
		t.Fatalf("regime=%#v", regime)
	}
}

func TestBuildFlipDiagnosticIncludesVolumeDecision(t *testing.T) {
	snapshot := strategy.Snapshot{
		Current: marketmodel.Kline{CloseTime: 123, Open: "99", High: "101", Low: "98", Close: "100"},
		Indicator: strategy.IndicatorView{
			NumericValues: map[string]float64{"volume_ratio20": 1.1, "atr14": 2, "macd_hist": 0.5},
			Signals:       map[string]string{"price_volume_confirmation": "confirm_up", "structure_event": "bos_up"},
		},
		Timeframes: map[string]strategy.TimeframeSnapshot{"5m": {
			Indicator: strategy.IndicatorView{
				NumericValues: map[string]float64{"adx14": 25, "di_plus14": 30, "di_minus14": 10},
				Signals:       map[string]string{"supertrend_direction": "up", "macd_momentum": "expanding_bull"},
			},
		}},
	}
	diagnostic := buildFlipDiagnostic(snapshot, strategy.SignalSideBuy, &marketregime.Result{AllowLong: true})
	fiveMinute := diagnostic.HigherTimeframes["5m"]
	if !diagnostic.Allowed || !diagnostic.VolumeRatioReady || diagnostic.VolumeRatio20 != 1.1 || !diagnostic.VolumeLooseAllowed || diagnostic.VolumeStrongAllowed || diagnostic.BodyATR != 0.5 || diagnostic.StructureEvent != "bos_up" || diagnostic.Direction5M != "up" || !fiveMinute.Available || fiveMinute.ADX == nil || *fiveMinute.ADX != 25 || fiveMinute.MACDMomentum != "expanding_bull" || diagnostic.HigherTimeframes["30m"].Available {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
}
