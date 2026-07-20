// Package experiments defines the lifecycle contract for independently
// pluggable signal-research experiments.
package experiments

import (
	"context"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

// Descriptor identifies an experiment and the implementation version that
// produced its result.
type Descriptor struct {
	Name    string
	Version string
}

// EventSet contains the detector outputs available for one base-timeframe bar.
// Experiments must treat the events and their nested values as read-only.
type EventSet struct {
	Platform            []signalresearch.PlatformEvent
	CompressionBreakout []signalresearch.PlatformEvent
	Impulse             []signalresearch.PlatformEvent
	Pullback            []signalresearch.PlatformEvent
}

// EntryCandidate is a normalized signal that has passed the runner-owned
// counter-trend and cooldown gates. Sources must be treated as read-only.
type EntryCandidate struct {
	Side         strategy.SignalSide
	Sources      []string
	MetadataJSON string
}

// Frame is the complete, read-only input for one experiment step.
// One call represents one base-timeframe bar; an experiment owns the ordering
// of its internal state advance, signal evaluation, and simulated entry.
type Frame struct {
	Snapshot  strategy.Snapshot
	Regime    marketregime.Result
	HasRegime bool
	Events    EventSet
	Entries   []EntryCandidate
	RunID     string
	InWindow  bool
	// InAnalysisWindow excludes warmup and post-range bars from review artifacts.
	InAnalysisWindow bool
}

// Artifact is an in-memory output that the runner may persist or display.
// Experiments do not write files, databases, or logs directly.
type Artifact struct {
	Name      string
	MediaType string
	Data      []byte
}

// Result contains the final output of one experiment. Summary should be a
// concrete experiment-owned type; any is used only at the serialization edge.
type Result struct {
	Descriptor Descriptor
	Summary    any
	Artifacts  []Artifact
}

// Experiment consumes the shared frame stream and returns independently
// serializable results. Finish is called exactly once by Registry.
type Experiment interface {
	Descriptor() Descriptor
	OnFrame(context.Context, Frame) error
	Finish(context.Context) (Result, error)
}
