package supertrend

import (
	"context"
	"encoding/json"
	"testing"

	"alphaflow/go-service/backtest-engine/cmd/supertrend-signal-research/experiments"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func TestExperimentImplementsLifecycle(t *testing.T) {
	experiment, err := New(Config{Replay: replayConfigForTest(), Pullback: pullbackConfigForTest()})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := experiments.NewRegistry(experiment)
	if err != nil {
		t.Fatal(err)
	}
	frame := signalFrame()
	if err := registry.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	results, err := registry.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Descriptor != descriptor {
		t.Fatalf("results=%+v", results)
	}
	summary := results[0].Summary.(Summary)
	if len(summary.Versions) != len(versionSpecs) {
		t.Fatalf("versions=%d, want %d", len(summary.Versions), len(versionSpecs))
	}
	standard := summary.Versions[0]
	for _, modeName := range []string{
		"flip", "flip_volume_loose", "flip_volume_strong", "deferred",
		"exhaust_adx30_di8", "exhaust_adx35_di8", "10m_15m_veto",
		"exhaust_deferred_reacceleration", "combined",
	} {
		mode := summaryMode(standard, modeName)
		if mode.RawSignals != 1 || mode.Replay.Trades != 1 {
			t.Fatalf("mode %s summary=%+v", modeName, mode)
		}
	}
}

func TestExperimentReturnsOptionalDiagnosticsArtifact(t *testing.T) {
	experiment, err := New(Config{
		Replay: replayConfigForTest(), Pullback: pullbackConfigForTest(), Diagnostics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := experiment.OnFrame(context.Background(), signalFrame()); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Name != "supertrend-diagnostics.json" || result.Artifacts[0].MediaType != "application/json" {
		t.Fatalf("artifacts=%+v", result.Artifacts)
	}
	var artifact diagnosticsArtifact
	if err := json.Unmarshal(result.Artifacts[0].Data, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Versions) != 4 || len(artifact.Versions[0].Flips) != 1 || len(artifact.Versions[0].Entries) != 1 {
		t.Fatalf("artifact=%+v", artifact)
	}
}

func TestExperimentReturnsOptionalReviewArtifacts(t *testing.T) {
	swingConfig := signalresearch.SwingReviewConfig{
		MinimumMovePoints: 30, ReversalPoints: 10, LeadWindowMS: 45 * 60 * 1000,
	}
	experiment, err := New(Config{
		Replay: replayConfigForTest(), Pullback: pullbackConfigForTest(),
		SwingReview: &swingConfig, StopReview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := signalFrame()
	frame.InAnalysisWindow = true
	frame.Snapshot.Window.Signals["ai_supertrend_flip"] = strategy.SignalSeries{Latest: "up"}
	frame.Snapshot.Window.Signals["ai_supertrend_direction"] = strategy.SignalSeries{Latest: "up"}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("artifacts=%+v", result.Artifacts)
	}
	if result.Artifacts[0].Name != "supertrend-swing-review.json" || result.Artifacts[1].Name != "supertrend-stop-review.json" {
		t.Fatalf("artifacts=%+v", result.Artifacts)
	}
	for _, artifact := range result.Artifacts {
		var decoded map[string]any
		if err := json.Unmarshal(artifact.Data, &decoded); err != nil {
			t.Fatalf("decode %s: %v", artifact.Name, err)
		}
	}
}

func TestNewRejectsInvalidSwingReviewConfig(t *testing.T) {
	invalid := signalresearch.SwingReviewConfig{MinimumMovePoints: 10, ReversalPoints: 10}
	_, err := New(Config{
		Replay: replayConfigForTest(), Pullback: pullbackConfigForTest(), SwingReview: &invalid,
	})
	if err == nil {
		t.Fatal("expected invalid Swing Review config error")
	}
}

func TestExperimentDoesNotEnterOutsideWindow(t *testing.T) {
	experiment, err := New(Config{Replay: replayConfigForTest(), Pullback: pullbackConfigForTest()})
	if err != nil {
		t.Fatal(err)
	}
	frame := signalFrame()
	frame.InWindow = false
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(Summary)
	for _, version := range summary.Versions {
		for _, mode := range version.Modes {
			if mode.RawSignals != 0 || mode.Replay.Trades != 0 {
				t.Fatalf("version=%s mode=%s summary=%+v", version.Version, mode.EntryMode, mode)
			}
		}
	}
}

func signalFrame() experiments.Frame {
	return experiments.Frame{
		Snapshot: strategy.Snapshot{
			Current: marketmodel.Kline{
				OpenTime: 1, CloseTime: 2, Open: "99", High: "101", Low: "98", Close: "100", Volume: "1000",
			},
			Indicator: strategy.IndicatorView{NumericValues: map[string]float64{
				"atr14": 2, "volume_ratio20": 1.2,
			}},
			Window: strategy.IndicatorWindowView{Signals: map[string]strategy.SignalSeries{
				"supertrend_flip": {Latest: "up"},
			}},
		},
		Regime: marketregime.Result{
			State: marketregime.StateTrendArmed, Direction: marketregime.DirectionLong,
			AllowNewPosition: true, AllowLong: true,
		},
		HasRegime: true,
		InWindow:  true,
	}
}

func summaryMode(version VersionSummary, name string) ModeSummary {
	for _, mode := range version.Modes {
		if mode.EntryMode == name {
			return mode
		}
	}
	return ModeSummary{}
}
