package supertrend

import (
	"context"
	"encoding/json"
	"fmt"

	"alphaflow/go-service/backtest-engine/internal/research/supertrend/experiments"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
)

var descriptor = experiments.Descriptor{Name: "supertrend_version_comparison", Version: "v1"}

type Config struct {
	Replay      signalresearch.SinglePositionConfig
	Pullback    signalresearch.PullbackConfig
	Diagnostics bool
	SwingReview *signalresearch.SwingReviewConfig
	StopReview  bool
}

type Summary struct {
	Versions []VersionSummary `json:"versions"`
}

type Experiment struct {
	versions      []*versionReplay
	diagnostics   bool
	swingReview   *signalresearch.SwingReviewConfig
	stopReview    bool
	reviewBars    []marketmodel.Kline
	swingSignals  []signalresearch.SwingSignal
	swingEvidence []signalresearch.SwingEvidence
}

var _ experiments.Experiment = (*Experiment)(nil)

func New(config Config) (*Experiment, error) {
	versions, err := newVersionReplays(config.Replay, config.Pullback)
	if err != nil {
		return nil, err
	}
	if err := validateSwingReviewConfig(config.SwingReview); err != nil {
		return nil, err
	}
	if config.Diagnostics || config.StopReview {
		enableDiagnostics(versions)
	}
	return &Experiment{
		versions: versions, diagnostics: config.Diagnostics,
		swingReview: config.SwingReview, stopReview: config.StopReview,
	}, nil
}

func (e *Experiment) Descriptor() experiments.Descriptor { return descriptor }

func (e *Experiment) OnFrame(ctx context.Context, frame experiments.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var regime *marketregime.Result
	if frame.HasRegime {
		regime = &frame.Regime
	}
	e.collectReviewFrame(frame, regime)
	for _, version := range e.versions {
		if err := version.advance(frame.Snapshot.Current); err != nil {
			return err
		}
		if err := version.followthrough.Advance(frame.Snapshot); err != nil {
			return fmt.Errorf("advance %s follow-through: %w", version.spec.name, err)
		}
		pullbackEvents, err := version.pullback.Update(frame.Snapshot)
		if err != nil {
			return fmt.Errorf("update %s pullback: %w", version.spec.name, err)
		}
		version.currentPullbackSide = eventSide(pullbackEvents)
		if !frame.InWindow {
			continue
		}
		if err := version.onSignalFrame(frame.Snapshot, regime); err != nil {
			return fmt.Errorf("evaluate %s: %w", version.spec.name, err)
		}
	}
	return nil
}

func (e *Experiment) Finish(context.Context) (experiments.Result, error) {
	summary := Summary{Versions: make([]VersionSummary, 0, len(e.versions))}
	for _, version := range e.versions {
		summary.Versions = append(summary.Versions, version.finish())
	}
	result := experiments.Result{Descriptor: descriptor, Summary: summary}
	if e.diagnostics {
		encoded, err := json.Marshal(buildDiagnosticsArtifact(e.versions))
		if err != nil {
			return experiments.Result{}, fmt.Errorf("marshal Supertrend diagnostics: %w", err)
		}
		result.Artifacts = []experiments.Artifact{{
			Name: "supertrend-diagnostics.json", MediaType: "application/json", Data: encoded,
		}}
	}
	if e.swingReview != nil {
		ai := e.version("ai")
		if ai == nil {
			return experiments.Result{}, fmt.Errorf("AI Supertrend version missing")
		}
		report, err := signalresearch.ReviewSwings(
			e.reviewBars, e.swingSignals, e.swingEvidence, ai.mode("flip").replay.Trades(), *e.swingReview,
		)
		if err != nil {
			return experiments.Result{}, fmt.Errorf("build Swing Review: %w", err)
		}
		encoded, err := json.Marshal(report)
		if err != nil {
			return experiments.Result{}, fmt.Errorf("marshal Swing Review: %w", err)
		}
		result.Artifacts = append(result.Artifacts, experiments.Artifact{
			Name: "supertrend-swing-review.json", MediaType: "application/json", Data: encoded,
		})
	}
	if e.stopReview {
		ai := e.version("ai")
		if ai == nil {
			return experiments.Result{}, fmt.Errorf("AI Supertrend version missing")
		}
		report, err := buildStopReview(e.reviewBars, []stopReviewSource{
			{mode: "flip", trades: ai.mode("flip").replay.Trades(), entries: ai.entryDiagnostics},
			{mode: "pullback", trades: ai.mode("pullback").replay.Trades(), entries: ai.entryDiagnostics},
		})
		if err != nil {
			return experiments.Result{}, fmt.Errorf("build Stop Review: %w", err)
		}
		encoded, err := json.Marshal(report)
		if err != nil {
			return experiments.Result{}, fmt.Errorf("marshal Stop Review: %w", err)
		}
		result.Artifacts = append(result.Artifacts, experiments.Artifact{
			Name: "supertrend-stop-review.json", MediaType: "application/json", Data: encoded,
		})
	}
	return result, nil
}

func (e *Experiment) collectReviewFrame(frame experiments.Frame, regime *marketregime.Result) {
	if frame.InAnalysisWindow && (e.swingReview != nil || e.stopReview) {
		e.reviewBars = append(e.reviewBars, frame.Snapshot.Current)
	}
	if e.swingReview == nil {
		return
	}
	if frame.InAnalysisWindow {
		if side, ok := signalSide(frame.Snapshot.Window, "ai_supertrend_direction"); ok {
			e.swingEvidence = append(e.swingEvidence, signalresearch.SwingEvidence{
				TimeMS: frame.Snapshot.Current.CloseTime, Side: side, Source: "ai_trend",
			})
		}
		for _, events := range [][]signalresearch.PlatformEvent{
			frame.Events.Platform, frame.Events.Impulse, frame.Events.Pullback, frame.Events.CompressionBreakout,
		} {
			for _, event := range events {
				e.swingEvidence = append(e.swingEvidence, signalresearch.SwingEvidence{
					TimeMS: frame.Snapshot.Current.CloseTime, Side: event.Side, Source: event.Source,
				})
			}
		}
	}
	if frame.InWindow {
		if side, ok := signalSide(frame.Snapshot.Window, "ai_supertrend_flip"); ok {
			diagnostic := buildFlipDiagnostic(frame.Snapshot, side, regime)
			e.swingSignals = append(e.swingSignals, signalresearch.SwingSignal{
				TimeMS: diagnostic.SignalTimeMS, Side: diagnostic.Side,
				Allowed: diagnostic.Allowed, Reason: diagnostic.Reason,
			})
		}
	}
}

func (e *Experiment) version(name string) *versionReplay {
	for _, version := range e.versions {
		if version.spec.name == name {
			return version
		}
	}
	return nil
}
