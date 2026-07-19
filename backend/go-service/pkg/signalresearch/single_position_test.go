package signalresearch

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

func singlePositionTestConfig() SinglePositionConfig {
	return SinglePositionConfig{
		InitialEquity: 10000, MarginQuote: 100, Leverage: 100,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: 12 * time.Hour, CooldownBars: 2, FeeRate: 0.0006, SlippageBps: 2,
	}
}

func TestSinglePositionReplayAppliesProtectionOnFollowingBar(t *testing.T) {
	replay, err := NewSinglePositionReplay(singlePositionTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	regime := &marketregime.Result{AllowLong: true}
	if entered, err := replay.TryEnter(researchSnapshot("100", strategy.SignalSideBuy), strategy.SignalSideBuy, regime); err != nil || !entered {
		t.Fatalf("entered=%t err=%v", entered, err)
	}
	// Reaches 100 bps profit; the resulting 70 bps trailing stop is active on
	// the next bar, not retroactively inside this bar.
	if err := replay.Advance(researchKline(2, "101", "99.9", "100.8")); err != nil {
		t.Fatal(err)
	}
	if replay.position == nil || replay.position.stopBps != 70 {
		t.Fatalf("position=%#v", replay.position)
	}
	if err := replay.Advance(researchKline(3, "100.9", "100.6", "100.7")); err != nil {
		t.Fatal(err)
	}
	summary := replay.Summary()
	if summary.Trades != 1 || summary.NetPnL != 54 {
		t.Fatalf("summary=%#v", summary)
	}
	if summary.AverageHoldingMin <= 0 {
		t.Fatalf("average holding minutes=%v", summary.AverageHoldingMin)
	}
	if summary.TrailingStopExits != 1 || len(replay.Trades()) != 1 || replay.Trades()[0].MFEBps != 100 {
		t.Fatalf("diagnostics summary=%#v trades=%#v", summary, replay.Trades())
	}
}

func TestSinglePositionReplayClassifiesSameBarAmbiguity(t *testing.T) {
	replay, err := NewSinglePositionReplay(singlePositionTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	regime := &marketregime.Result{AllowLong: true}
	if entered, err := replay.TryEnter(researchSnapshot("100", strategy.SignalSideBuy), strategy.SignalSideBuy, regime); err != nil || !entered {
		t.Fatalf("entered=%t err=%v", entered, err)
	}
	if err := replay.Advance(researchKline(2, "100.6", "99.4", "100")); err != nil {
		t.Fatal(err)
	}
	summary := replay.Summary()
	if summary.AmbiguousStopExits != 1 || summary.LossGiveback != 0 {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []float64{40, 10, 30, 20}
	if got := percentile(values, 0.50); got != 20 {
		t.Fatalf("p50=%v", got)
	}
	if got := percentile(values, 0.90); got != 40 {
		t.Fatalf("p90=%v", got)
	}
}

func TestSinglePositionReplayUsesRegimeAndCooldown(t *testing.T) {
	replay, err := NewSinglePositionReplay(singlePositionTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := researchSnapshot("100", strategy.SignalSideBuy)
	longOnly := &marketregime.Result{AllowLong: true, AllowShort: false}
	if entered, err := replay.TryEnter(snapshot, strategy.SignalSideSell, longOnly); err != nil || entered {
		t.Fatalf("countertrend entered=%t err=%v", entered, err)
	}
	if entered, err := replay.TryEnter(snapshot, strategy.SignalSideBuy, longOnly); err != nil || !entered {
		t.Fatalf("long entered=%t err=%v", entered, err)
	}
	if err := replay.Advance(researchKline(2, "100", "99.4", "99.5")); err != nil {
		t.Fatal(err)
	}
	if entered, _ := replay.TryEnter(snapshot, strategy.SignalSideBuy, longOnly); entered {
		t.Fatal("entered during cooldown")
	}
	if replay.summary.SkippedByRegime != 1 || replay.summary.SkippedCooldown != 1 {
		t.Fatalf("summary=%#v", replay.summary)
	}
}

func TestSinglePositionReplayCountsV4SkipReasons(t *testing.T) {
	replay, err := NewSinglePositionReplay(singlePositionTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	regime := &marketregime.Result{Reasons: []string{"ribbon_bullish", "v4_weak_direction"}}
	if entered, err := replay.TryEnter(researchSnapshot("100", strategy.SignalSideBuy), strategy.SignalSideBuy, regime); err != nil || entered {
		t.Fatalf("entered=%t err=%v", entered, err)
	}
	if replay.summary.RegimeSkipReasons["v4_weak_direction"] != 1 {
		t.Fatalf("reasons=%v", replay.summary.RegimeSkipReasons)
	}
}

func TestSinglePositionReplayClassifiesCountertrendV4Signal(t *testing.T) {
	replay, err := NewSinglePositionReplay(singlePositionTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	regime := &marketregime.Result{AllowShort: true, Reasons: []string{"v4_trend_permitted", "v4_permitted"}}
	if entered, err := replay.TryEnter(researchSnapshot("100", strategy.SignalSideBuy), strategy.SignalSideBuy, regime); err != nil || entered {
		t.Fatalf("entered=%t err=%v", entered, err)
	}
	if replay.summary.RegimeSkipReasons["v4_countertrend_signal"] != 1 {
		t.Fatalf("reasons=%v", replay.summary.RegimeSkipReasons)
	}
}
