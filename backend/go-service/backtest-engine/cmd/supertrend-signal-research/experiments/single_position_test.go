package experiments

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func TestSinglePositionExperimentBuildsBaselineAndScan(t *testing.T) {
	baseline, err := NewSinglePositionExperiment(breakoutConfigForTest(), false)
	if err != nil {
		t.Fatal(err)
	}
	scanned, err := NewSinglePositionExperiment(breakoutConfigForTest(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.variants) != 1 {
		t.Fatalf("baseline variants=%d, want 1", len(baseline.variants))
	}
	if len(scanned.variants) != 7 {
		t.Fatalf("scanned variants=%d, want 7", len(scanned.variants))
	}
}

func TestSinglePositionExperimentConsumesNormalizedEntry(t *testing.T) {
	experiment, err := NewSinglePositionExperiment(breakoutConfigForTest(), false)
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.Entries = []EntryCandidate{{
		Side: strategy.SignalSideBuy, Sources: []string{"supertrend_flip"}, MetadataJSON: `{}`,
	}}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(SinglePositionSummary)
	baseline := summary.Variants[0]
	if baseline.Variant != "baseline" || baseline.Replay.Trades != 1 || baseline.Replay.DatasetEndExits != 1 {
		t.Fatalf("baseline=%+v", baseline)
	}
}

func TestSinglePositionExperimentRecordsConflictingEntries(t *testing.T) {
	experiment, err := NewSinglePositionExperiment(breakoutConfigForTest(), false)
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.Entries = []EntryCandidate{
		{Side: strategy.SignalSideBuy},
		{Side: strategy.SignalSideSell},
	}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(SinglePositionSummary)
	if got := summary.Variants[0].Replay.SkippedConflict; got != 1 {
		t.Fatalf("skipped conflicts=%d, want 1", got)
	}
}

func TestSinglePositionExperimentAdvancesOutsideWindowWithoutEntry(t *testing.T) {
	experiment, err := NewSinglePositionExperiment(breakoutConfigForTest(), false)
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.InWindow = false
	frame.Entries = []EntryCandidate{{Side: strategy.SignalSideBuy}}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(SinglePositionSummary)
	if got := summary.Variants[0].Replay.Trades; got != 0 {
		t.Fatalf("trades=%d, want 0", got)
	}
}

func TestSinglePositionExperimentRejectsInvalidConfig(t *testing.T) {
	_, err := NewSinglePositionExperiment(signalresearch.SinglePositionConfig{}, false)
	if err == nil {
		t.Fatal("expected invalid config error")
	}
}
