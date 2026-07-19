package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestEfficiencyRatioSeparatesTrendAndChop(t *testing.T) {
	trend := []platformBar{{close: 1}, {close: 2}, {close: 3}, {close: 4}}
	chop := []platformBar{{close: 1}, {close: 2}, {close: 1}, {close: 2}, {close: 1}}
	if got := efficiencyRatio(trend); got != 1 {
		t.Fatalf("trend efficiency=%v", got)
	}
	if got := efficiencyRatio(chop); got != 0 {
		t.Fatalf("chop efficiency=%v", got)
	}
}

func TestTrendFlipCount(t *testing.T) {
	samples := []chopTrendSample{{direction: "up"}, {direction: "down"}, {direction: "down"}, {direction: "up"}}
	if got := trendFlipCount(samples); got != 2 {
		t.Fatalf("flips=%d", got)
	}
}

func TestChopConfirmedExitsAfterEvidenceDisappears(t *testing.T) {
	detector := &ChopDetector{
		config: ChopConfig{MinVotes: 3, ExitBars: 2, BreakoutVolumeRatio: 1.5},
		state:  "chop_confirmed",
	}
	snapshot := strategy.Snapshot{Indicator: strategy.IndicatorView{NumericValues: map[string]float64{"volume_ratio20": 1}}}
	detector.transition(snapshot, 100, 110, 90, 1)
	if detector.state != "chop_confirmed" {
		t.Fatalf("state after first weak bar=%q", detector.state)
	}
	detector.transition(snapshot, 100, 110, 90, 1)
	if detector.state != "normal" {
		t.Fatalf("state after exit confirmation=%q", detector.state)
	}
}
