package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestImpulseDetectorEmitsDirectionalBreakout(t *testing.T) {
	detector, err := NewImpulseDetector(ImpulseConfig{
		LookbackBars: 3, BreakoutBars: 10, MinMoveATR: 1.5, MinVolumeRatio: 1.5, CooldownBars: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 10; index++ {
		snapshot := platformSnapshot(index, 100, 100.2, 99.8, 100, "up", "up")
		snapshot.Indicator.NumericValues["volume_ratio20"] = 1
		if _, err := detector.Update(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := platformSnapshot(11, 102, 102.1, 100, 300, "up", "up")
	snapshot.Indicator.NumericValues["volume_ratio20"] = 2
	events, err := detector.Update(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Side != strategy.SignalSideBuy || events[0].Source != VolatilityImpulseSource {
		t.Fatalf("events=%#v", events)
	}
}

func TestEventGateDeduplicatesAcrossSources(t *testing.T) {
	gate, err := NewEventGate(2)
	if err != nil {
		t.Fatal(err)
	}
	if !gate.Allow(strategy.SignalSideBuy, []string{PlatformBreakoutSource}) {
		t.Fatal("first event should pass")
	}
	gate.Advance()
	if gate.Allow(strategy.SignalSideBuy, []string{VolatilityImpulseSource}) {
		t.Fatal("second source should be suppressed during cooldown")
	}
	if !gate.Allow(strategy.SignalSideSell, []string{VolatilityImpulseSource}) {
		t.Fatal("opposite side should have an independent cooldown")
	}
	gate.Advance()
	if gate.Allow(strategy.SignalSideBuy, []string{PullbackResumeSource}) {
		t.Fatal("cooldown should still apply on the second bar")
	}
	gate.Advance()
	if !gate.Allow(strategy.SignalSideBuy, []string{PullbackResumeSource}) {
		t.Fatal("event should pass after cooldown")
	}
}

func TestPullbackDetectorArmsAtEMAAndEmitsResume(t *testing.T) {
	detector, err := NewPullbackDetector(PullbackConfig{
		TouchDistancePct: 0.15, ResumeBars: 3, MaxArmedBars: 10, MinVolumeRatio: 1, CooldownBars: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	prices := []float64{100.4, 100.2, 100.0, 100.15, 100.25}
	for index, price := range prices {
		snapshot := pullbackSnapshot(index+1, price, price+0.1, price-0.1, 0.9)
		if _, err := detector.Update(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := pullbackSnapshot(6, 100.8, 100.9, 100.2, 1.2)
	events, err := detector.Update(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Side != strategy.SignalSideBuy || events[0].Source != PullbackResumeSource {
		t.Fatalf("events=%#v", events)
	}
}

func pullbackSnapshot(index int, closePrice float64, high float64, low float64, volumeRatio float64) strategy.Snapshot {
	snapshot := platformSnapshot(index, closePrice, high, low, 100, "up", "up")
	snapshot.Indicator.NumericValues["ema25"] = 100
	snapshot.Indicator.NumericValues["volume_ratio5"] = volumeRatio
	return snapshot
}
