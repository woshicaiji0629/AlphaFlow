package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestCounterTrendGateAllowsHigherTimeframeAlignedSignal(t *testing.T) {
	gate := newCounterTrendGate(t)
	snapshot := counterTrendSnapshot(1, 100, "up", "down", "down", 1)
	decision, err := gate.Evaluate(snapshot, strategy.SignalSideBuy, []string{"supertrend_flip"})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allow || decision.CounterTrend {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestCounterTrendGateRequiresWaitConfirmationStructureAndVolume(t *testing.T) {
	gate := newCounterTrendGate(t)
	first := counterTrendSnapshot(1, 100, "down", "down", "down", 1)
	decision, err := gate.Evaluate(first, strategy.SignalSideBuy, []string{"supertrend_flip"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allow || !decision.CounterTrend {
		t.Fatalf("first decision=%#v", decision)
	}
	for index := 2; index <= 3; index++ {
		snapshot := counterTrendSnapshot(index, 100, "down", "down", "up", 2)
		decision, err = gate.Evaluate(snapshot, strategy.SignalSideBuy, []string{VolatilityImpulseSource})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allow {
			t.Fatalf("bar %d should still be waiting", index)
		}
	}
	snapshot := counterTrendSnapshot(4, 101, "down", "down", "up", 2)
	decision, err = gate.Evaluate(snapshot, strategy.SignalSideBuy, []string{VolatilityImpulseSource})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allow || !decision.CounterTrend {
		t.Fatalf("confirmed decision=%#v", decision)
	}
	snapshot = counterTrendSnapshot(5, 102, "down", "down", "up", 2)
	decision, err = gate.Evaluate(snapshot, strategy.SignalSideBuy, []string{VolatilityImpulseSource})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allow {
		t.Fatal("only one counter-trend attempt should pass in the same regime")
	}
}

func TestCounterTrendGateRejectsMissingHigherTimeframe(t *testing.T) {
	gate := newCounterTrendGate(t)
	snapshot := counterTrendSnapshot(1, 100, "", "down", "up", 2)
	delete(snapshot.Timeframes, "15m")
	decision, err := gate.Evaluate(snapshot, strategy.SignalSideBuy, []string{"supertrend_flip"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allow {
		t.Fatal("missing higher timeframe must not authorize counter-trend signal")
	}
}

func newCounterTrendGate(t *testing.T) *CounterTrendGate {
	t.Helper()
	gate, err := NewCounterTrendGate(CounterTrendConfig{WaitBars: 3, StructureBars: 3, MinVolumeRatio: 1.5, SizeFactor: 0.25})
	if err != nil {
		t.Fatal(err)
	}
	return gate
}

func counterTrendSnapshot(index int, closePrice float64, direction15 string, direction30 string, direction5 string, volumeRatio float64) strategy.Snapshot {
	snapshot := platformSnapshot(index, closePrice, closePrice+0.1, closePrice-0.1, 100, direction5, direction5)
	snapshot.Indicator.NumericValues["volume_ratio20"] = volumeRatio
	snapshot.Timeframes["15m"] = strategy.TimeframeSnapshot{Indicator: strategy.IndicatorView{Signals: map[string]string{"supertrend_direction": direction15}}}
	snapshot.Timeframes["30m"] = strategy.TimeframeSnapshot{Indicator: strategy.IndicatorView{Signals: map[string]string{"supertrend_direction": direction30}}}
	return snapshot
}
