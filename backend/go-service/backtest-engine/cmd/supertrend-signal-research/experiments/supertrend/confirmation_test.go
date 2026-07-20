package supertrend

import (
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

func TestExhaustionAndMacroVeto(t *testing.T) {
	snapshot := strategy.Snapshot{Timeframes: map[string]strategy.TimeframeSnapshot{
		"10m": {Indicator: strategy.IndicatorView{Signals: map[string]string{"supertrend_direction": "down"}}},
		"15m": {Indicator: strategy.IndicatorView{NumericValues: map[string]float64{
			"adx14": 36, "di_plus14": 30, "di_minus14": 20, "macd_hist_delta": -0.2,
		}}},
	}}
	if !exhaustionBlocked(snapshot, strategy.SignalSideBuy, 35, 8) || !macroMomentumBlocked(snapshot, strategy.SignalSideBuy) {
		t.Fatal("expected long signal to be blocked")
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
	invalid := strategy.Snapshot{
		Current:   marketmodel.Kline{Close: "99"},
		Indicator: strategy.IndicatorView{Signals: map[string]string{"structure_event": "choch_down"}},
	}
	allowed, expired, err = pending.exhaustReaccelerationAllows(invalid)
	if err != nil || allowed || !expired {
		t.Fatalf("invalid allowed=%t expired=%t err=%v", allowed, expired, err)
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
