package signalresearch

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func TestReplayStopsBeforeLaterRecoveryAndKeepsEarlierProfitTiers(t *testing.T) {
	replay, err := New(Config{
		RunID: "run-1", Leverage: 100, Horizon: 12 * time.Hour,
		FixedStopMargin: []float64{70}, TakeProfitMargin: []float64{50, 150, 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := researchSnapshot("100", strategy.SignalSideBuy)
	if err := replay.AddSignal(snapshot, strategy.SignalSideBuy, []string{"supertrend_flip"}); err != nil {
		t.Fatal(err)
	}
	// 150% margin profit at 100x is a 150 bps favorable move.
	if err := replay.Advance(researchKline(2, "101.5", "100.2", "101")); err != nil {
		t.Fatal(err)
	}
	// The next bar hits the 70 bps stop. A later recovery must not be observed.
	if err := replay.Advance(researchKline(3, "101.2", "99.3", "99.5")); err != nil {
		t.Fatal(err)
	}
	if err := replay.Advance(researchKline(4, "105", "99.5", "105")); err != nil {
		t.Fatal(err)
	}
	replay.Finish()
	_, outcomes := replay.Results()
	if len(outcomes) != 3 {
		t.Fatalf("outcomes=%d, want 3", len(outcomes))
	}
	want := map[float64]string{50: "take_profit", 150: "take_profit", 500: "stop_loss"}
	for _, outcome := range outcomes {
		if outcome.Result != want[outcome.TakeProfitMarginPct] {
			t.Fatalf("tier=%v result=%q want=%q", outcome.TakeProfitMarginPct, outcome.Result, want[outcome.TakeProfitMarginPct])
		}
		if outcome.HighestTakeProfitMarginPct != 150 {
			t.Fatalf("highest tier=%v, want 150", outcome.HighestTakeProfitMarginPct)
		}
		if outcome.Result == "take_profit" && outcome.ObservedBars != 1 {
			t.Fatalf("tier=%v observed bars=%d, want 1", outcome.TakeProfitMarginPct, outcome.ObservedBars)
		}
	}
}

func TestReplayUsesConservativeStopWhenSameBarTouchesBoth(t *testing.T) {
	replay, err := New(Config{
		RunID: "run-1", Leverage: 100, Horizon: 12 * time.Hour,
		FixedStopMargin: []float64{70}, TakeProfitMargin: []float64{50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.AddSignal(researchSnapshot("100", strategy.SignalSideBuy), strategy.SignalSideBuy, []string{"wick_reclaim"}); err != nil {
		t.Fatal(err)
	}
	if err := replay.Advance(researchKline(2, "101", "99", "100")); err != nil {
		t.Fatal(err)
	}
	_, outcomes := replay.Results()
	if len(outcomes) != 1 || outcomes[0].Result != "stop_loss" || outcomes[0].HighestTakeProfitMarginPct != 0 {
		t.Fatalf("outcomes=%#v", outcomes)
	}
}

func researchSnapshot(closePrice string, _ strategy.SignalSide) strategy.Snapshot {
	return strategy.Snapshot{
		Target:    strategy.Target{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"},
		Current:   marketmodel.Kline{OpenTime: 1, CloseTime: 1, Close: closePrice},
		Indicator: strategy.IndicatorView{NumericValues: map[string]float64{"atr14": 1}},
		Window:    strategy.IndicatorWindowView{Values: map[string]strategy.NumericSeries{"atr14": {Latest: 1}}},
		AsOf:      1,
	}
}

func researchKline(closeTime int64, high string, low string, closePrice string) marketmodel.Kline {
	return marketmodel.Kline{OpenTime: closeTime - 1, CloseTime: closeTime, High: high, Low: low, Close: closePrice, IsClosed: true}
}
