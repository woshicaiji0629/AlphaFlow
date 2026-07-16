package supertrend

import (
	"context"
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
	if check, ok := diagnostic(result.Analysis.Checks, "entry_threshold", strategy.SignalSideBuy); !ok || check.Status != strategy.DiagnosticStatusPass {
		t.Fatalf("buy threshold diagnostic = %#v", check)
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

func diagnostic(checks []strategy.DiagnosticCheck, name string, side strategy.SignalSide) (strategy.DiagnosticCheck, bool) {
	for _, check := range checks {
		if check.Name == name && check.Side == side {
			return check, true
		}
	}
	return strategy.DiagnosticCheck{}, false
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

func window(side strategy.SignalSide, overrides map[string]string) strategy.IndicatorWindowView {
	signals := map[string]strategy.SignalSeries{
		"data_quality":              {Latest: "ok"},
		"trend_valid":               {Latest: "true"},
		"trend_quality":             {Latest: "strong"},
		"ma_ribbon_state":           {Latest: maRibbonStateForSide(side)},
		"ma_ribbon_phase":           {Latest: "trend"},
		"ema_alignment":             {Latest: biasForSide(side)},
		"macd_window_bias":          {Latest: biasForSide(side)},
		"macd_momentum":             {Latest: biasForSide(side)},
		"price_volume_confirmation": {Latest: volumeForSide(side)},
		"volume_window_state":       {Latest: "expanding"},
		"supertrend_direction":      {Latest: directionForSide(side), StableCount: 2},
		"trend_window_bias":         {Latest: biasForSide(side)},
		"trend_price_progress":      {Latest: "advancing"},
	}
	if side == strategy.SignalSideBuy {
		signals["pump_window_signal"] = strategy.SignalSeries{Latest: "true"}
		signals["pump_window_fake_risk"] = strategy.SignalSeries{Latest: "low"}
	} else {
		signals["dump_window_signal"] = strategy.SignalSeries{Latest: "true"}
		signals["dump_window_fake_risk"] = strategy.SignalSeries{Latest: "low"}
	}
	for key, value := range overrides {
		signals[key] = strategy.SignalSeries{Latest: value}
	}
	return strategy.IndicatorWindowView{
		OpenTime: 1000,
		Values: map[string]strategy.NumericSeries{
			"resistance_1": {Latest: 110},
			"support_1":    {Latest: 90},
			"supertrend":   {Latest: 95},
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
