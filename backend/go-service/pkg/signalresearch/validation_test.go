package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestValidationReplayCapturesEarlyFollowThrough(t *testing.T) {
	replay, err := NewValidationReplay(ValidationConfig{ObservationBars: []int{3, 5}})
	if err != nil {
		t.Fatal(err)
	}
	entry := counterTrendSnapshot(1, 100, "up", "up", "up", 1)
	if err := replay.AddSignal("run", entry, strategy.SignalSideBuy, []string{"supertrend_flip"}); err != nil {
		t.Fatal(err)
	}
	for index, closePrice := range []float64{100.1, 100.3, 100.5, 100.4, 100.6} {
		snapshot := counterTrendSnapshot(index+2, closePrice, "up", "up", "up", 1)
		if err := replay.Advance(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	results := replay.Results()
	if len(results) != 2 {
		t.Fatalf("results=%#v", results)
	}
	if results[0].ObservationBars != 3 || results[0].MaxFavorableBps <= 0 || !results[0].SignalStructureHeld || results[0].Confirmation5M != "up" {
		t.Fatalf("first observation=%#v", results[0])
	}
}

func TestValidationReplayMarksSignalBarStructureFailure(t *testing.T) {
	replay, err := NewValidationReplay(ValidationConfig{ObservationBars: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	entry := counterTrendSnapshot(1, 100, "down", "down", "down", 1)
	if err := replay.AddSignal("run", entry, strategy.SignalSideSell, []string{"supertrend_flip"}); err != nil {
		t.Fatal(err)
	}
	for index, closePrice := range []float64{100.2, 100.1, 100} {
		snapshot := counterTrendSnapshot(index+2, closePrice, "down", "down", "up", 1)
		if err := replay.Advance(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	result := replay.Results()[0]
	if result.SignalStructureHeld {
		t.Fatalf("observation=%#v", result)
	}
}
