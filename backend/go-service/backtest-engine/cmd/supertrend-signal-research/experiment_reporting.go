package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"alphaflow/go-service/backtest-engine/cmd/supertrend-signal-research/experiments"
	supertrendexperiment "alphaflow/go-service/backtest-engine/cmd/supertrend-signal-research/experiments/supertrend"
)

func reportSupertrendExperiment(result experiments.Result, runID, regimeVersion, swingReviewPath, stopReviewPath string) error {
	summary, ok := result.Summary.(supertrendexperiment.Summary)
	if !ok {
		return fmt.Errorf("Supertrend experiment returned summary type %T", result.Summary)
	}
	for _, version := range summary.Versions {
		for _, mode := range version.Modes {
			encoded, err := json.Marshal(mode.Replay)
			if err != nil {
				return fmt.Errorf("marshal Supertrend %s/%s summary: %w", version.Version, mode.EntryMode, err)
			}
			slog.Info("supertrend continuation comparison", "run_id", runID, "regime_version", regimeVersion,
				"supertrend_version", version.Version, "entry_mode", mode.EntryMode,
				"raw_signals", mode.RawSignals, "summary", string(encoded))
		}
	}

	wroteSwingReview := false
	wroteStopReview := false
	for _, artifact := range result.Artifacts {
		switch artifact.Name {
		case "supertrend-diagnostics.json":
			slog.Info("supertrend diagnostics", "detail", string(artifact.Data))
		case "supertrend-swing-review.json":
			if swingReviewPath == "" {
				return fmt.Errorf("Supertrend Swing Review artifact has no output path")
			}
			if err := writeJSONArtifact(swingReviewPath, artifact.Data); err != nil {
				return fmt.Errorf("write Swing Review: %w", err)
			}
			wroteSwingReview = true
			slog.Info("swing review written", "path", swingReviewPath)
		case "supertrend-stop-review.json":
			if stopReviewPath == "" {
				return fmt.Errorf("Supertrend Stop Review artifact has no output path")
			}
			if err := writeJSONArtifact(stopReviewPath, artifact.Data); err != nil {
				return fmt.Errorf("write Stop Review: %w", err)
			}
			wroteStopReview = true
			slog.Info("stop review written", "path", stopReviewPath)
		default:
			slog.Info("Supertrend experiment artifact", "name", artifact.Name, "media_type", artifact.MediaType, "bytes", len(artifact.Data))
		}
	}
	if swingReviewPath != "" && !wroteSwingReview {
		return fmt.Errorf("Supertrend experiment did not produce Swing Review artifact")
	}
	if stopReviewPath != "" && !wroteStopReview {
		return fmt.Errorf("Supertrend experiment did not produce Stop Review artifact")
	}
	return nil
}

func writeJSONArtifact(path string, data []byte) error {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", "  "); err != nil {
		return fmt.Errorf("format JSON artifact: %w", err)
	}
	return os.WriteFile(path, formatted.Bytes(), 0o644)
}
