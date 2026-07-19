package signalresearch

import (
	"encoding/json"
	"fmt"
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func TestPlatformDetectorEmitsLongBreakoutAndAppliesCooldown(t *testing.T) {
	detector := newTestPlatformDetector(t)
	for index := 1; index <= 12; index++ {
		events, err := detector.Update(platformSnapshot(index, 100, 100.2, 99.8, 100, "up", "up"))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("warmup events=%#v", events)
		}
	}
	events, err := detector.Update(platformSnapshot(13, 100.7, 100.8, 100.1, 200, "up", "up"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Side != strategy.SignalSideBuy || events[0].Source != PlatformBreakoutSource {
		t.Fatalf("events=%#v", events)
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(events[0].MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["phase"] != "breakout" || metadata["entry_direction"] != "up" || metadata["confirmation_direction"] != "up" {
		t.Fatalf("metadata=%#v", metadata)
	}
	events, err = detector.Update(platformSnapshot(14, 101, 101.1, 100.5, 300, "up", "up"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("cooldown events=%#v", events)
	}
}

func TestPlatformDetectorEmitsShortBreakout(t *testing.T) {
	detector := newTestPlatformDetector(t)
	for index := 1; index <= 12; index++ {
		if _, err := detector.Update(platformSnapshot(index, 100, 100.2, 99.8, 100, "down", "down")); err != nil {
			t.Fatal(err)
		}
	}
	events, err := detector.Update(platformSnapshot(13, 99.3, 99.9, 99.2, 200, "down", "down"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Side != strategy.SignalSideSell {
		t.Fatalf("events=%#v", events)
	}
}

func TestPlatformDetectorRejectsOpposedFiveMinuteTrend(t *testing.T) {
	detector := newTestPlatformDetector(t)
	for index := 1; index <= 12; index++ {
		if _, err := detector.Update(platformSnapshot(index, 100, 100.2, 99.8, 100, "up", "up")); err != nil {
			t.Fatal(err)
		}
	}
	events, err := detector.Update(platformSnapshot(13, 100.7, 100.8, 100.1, 200, "up", "down"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events=%#v", events)
	}
}

func TestPlatformDetectorAcceptsZeroVolumeWithoutConfirmingBreakout(t *testing.T) {
	detector := newTestPlatformDetector(t)
	for index := 1; index <= 12; index++ {
		if _, err := detector.Update(platformSnapshot(index, 100, 100.2, 99.8, 0, "up", "up")); err != nil {
			t.Fatal(err)
		}
	}
	events, err := detector.Update(platformSnapshot(13, 100.7, 100.8, 100.1, 0, "up", "up"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("zero-volume breakout events=%#v", events)
	}
}

func newTestPlatformDetector(t *testing.T) *PlatformDetector {
	t.Helper()
	detector, err := NewPlatformDetector(PlatformConfig{WindowBars: 12, MaxRangePct: 0.6, MinVolumeRatio: 1.5, CooldownBars: 20})
	if err != nil {
		t.Fatal(err)
	}
	return detector
}

func platformSnapshot(index int, closePrice float64, high float64, low float64, volume float64, entryDirection string, confirmationDirection string) strategy.Snapshot {
	entryLevel := 99.0
	confirmationLevel := 99.5
	if entryDirection == "down" {
		entryLevel = 101
	}
	if confirmationDirection == "down" {
		confirmationLevel = 100.5
	}
	indicator := strategy.IndicatorView{
		NumericValues: map[string]float64{"supertrend": entryLevel, "atr14": 1},
		Signals:       map[string]string{"supertrend_direction": entryDirection},
	}
	confirmation := strategy.TimeframeSnapshot{Indicator: strategy.IndicatorView{
		NumericValues: map[string]float64{"supertrend": confirmationLevel},
		Signals:       map[string]string{"supertrend_direction": confirmationDirection},
	}}
	return strategy.Snapshot{
		Target: strategy.Target{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"},
		Current: marketmodel.Kline{
			OpenTime: int64(index), CloseTime: int64(index), Open: fmt.Sprint(closePrice),
			High: fmt.Sprint(high), Low: fmt.Sprint(low), Close: fmt.Sprint(closePrice), Volume: fmt.Sprint(volume), IsClosed: true,
		},
		Indicator:  indicator,
		Timeframes: map[string]strategy.TimeframeSnapshot{"5m": confirmation},
	}
}
