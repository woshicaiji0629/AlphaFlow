package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

func TestCompressionBreakoutEmitsFromLockedRange(t *testing.T) {
	config := DefaultCompressionBreakoutConfig()
	detector, err := NewCompressionBreakoutDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	regime := &marketregime.Result{State: marketregime.StateChopLock, DirectionScore: 60}
	for index := 1; index <= config.WindowBars; index++ {
		snapshot := compressionBreakoutSnapshot(index, 100, 100.2, 99.8, 1)
		if _, err := detector.Update(snapshot, regime); err != nil {
			t.Fatal(err)
		}
	}
	events, err := detector.Update(compressionBreakoutSnapshot(21, 100.8, 100.9, 100.1, 1.3), regime)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Side != strategy.SignalSideBuy || events[0].Source != CompressionBreakoutSource {
		t.Fatalf("events=%#v", events)
	}
}

func TestCompressionBreakoutRejectsUnlockedRegime(t *testing.T) {
	config := DefaultCompressionBreakoutConfig()
	detector, err := NewCompressionBreakoutDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	locked := &marketregime.Result{State: marketregime.StateChopLock, DirectionScore: 60}
	for index := 1; index <= config.WindowBars; index++ {
		if _, err := detector.Update(compressionBreakoutSnapshot(index, 100, 100.2, 99.8, 1), locked); err != nil {
			t.Fatal(err)
		}
	}
	events, err := detector.Update(compressionBreakoutSnapshot(21, 100.8, 100.9, 100.1, 1.3), &marketregime.Result{State: marketregime.StateNormal, DirectionScore: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events=%#v", events)
	}
}

func TestCompressionBreakoutFreezesRangeAtLockEntry(t *testing.T) {
	config := DefaultCompressionBreakoutConfig()
	detector, err := NewCompressionBreakoutDetector(config)
	if err != nil {
		t.Fatal(err)
	}
	locked := &marketregime.Result{State: marketregime.StateChopLock, DirectionScore: 60}
	for index := 1; index <= config.WindowBars; index++ {
		if _, err := detector.Update(compressionBreakoutSnapshot(index, 100, 100.2, 99.8, 1), locked); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := detector.Update(compressionBreakoutSnapshot(21, 100.15, 100.3, 100, 1), locked); err != nil {
		t.Fatal(err)
	}
	events, err := detector.Update(compressionBreakoutSnapshot(22, 100.25, 100.4, 100.1, 1.3), locked)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%#v frozen_high=%f", events, detector.rangeHigh)
	}
}

func compressionBreakoutSnapshot(index int, closePrice float64, high float64, low float64, volumeRatio float64) strategy.Snapshot {
	snapshot := platformSnapshot(index, closePrice, high, low, 100, "up", "up")
	snapshot.Indicator.NumericValues["volume_ratio20"] = volumeRatio
	snapshot.Indicator.NumericValues["squeeze_momentum"] = 1
	snapshot.Indicator.NumericValues["squeeze_momentum_delta"] = 0.5
	return snapshot
}
