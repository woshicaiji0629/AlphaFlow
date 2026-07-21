package experiments

import (
	"context"
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

var breakoutDescriptor = Descriptor{Name: "compression_breakout_comparison", Version: "v1"}

// BreakoutModeSummary is one independently replayed breakout entry mode.
type BreakoutModeSummary struct {
	EntryMode  string                               `json:"entry_mode"`
	RawSignals int                                  `json:"raw_signals"`
	Replay     signalresearch.SinglePositionSummary `json:"replay"`
}

// BreakoutSummary contains all entry modes owned by BreakoutExperiment.
type BreakoutSummary struct {
	Modes []BreakoutModeSummary `json:"modes"`
}

// BreakoutExperiment compares platform, compression, and combined entries.
type BreakoutExperiment struct {
	platformSignals    int
	compressionSignals int
	combinedSignals    int
	platformReplay     *signalresearch.SinglePositionReplay
	compressionReplay  *signalresearch.SinglePositionReplay
	combinedReplay     *signalresearch.SinglePositionReplay
}

// NewBreakoutExperiment builds all replays owned by the experiment.
func NewBreakoutExperiment(config signalresearch.SinglePositionConfig) (*BreakoutExperiment, error) {
	platform, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return nil, fmt.Errorf("build platform breakout replay: %w", err)
	}
	compression, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return nil, fmt.Errorf("build compression breakout replay: %w", err)
	}
	combined, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return nil, fmt.Errorf("build combined breakout replay: %w", err)
	}
	return &BreakoutExperiment{
		platformReplay: platform, compressionReplay: compression, combinedReplay: combined,
	}, nil
}

func (e *BreakoutExperiment) Descriptor() Descriptor { return breakoutDescriptor }

// OnFrame advances every replay before evaluating entries for the current bar.
func (e *BreakoutExperiment) OnFrame(_ context.Context, frame Frame) error {
	for _, item := range []struct {
		name   string
		replay *signalresearch.SinglePositionReplay
	}{
		{name: "platform", replay: e.platformReplay},
		{name: "compression", replay: e.compressionReplay},
		{name: "flip_compression", replay: e.combinedReplay},
	} {
		if err := item.replay.Advance(frame.Snapshot.Current); err != nil {
			return fmt.Errorf("advance %s replay: %w", item.name, err)
		}
	}
	if !frame.InWindow {
		return nil
	}

	var regime *marketregime.Result
	if frame.HasRegime {
		regime = &frame.Regime
	}
	platformSide := eventSide(frame.Events.Platform)
	compressionSide := eventSide(frame.Events.CompressionBreakout)
	if platformSide != strategy.SignalSideHold {
		e.platformSignals++
		if _, err := e.platformReplay.TryEnter(frame.Snapshot, platformSide, eventEntryRegime(regime, platformSide)); err != nil {
			return fmt.Errorf("enter platform replay: %w", err)
		}
	}
	if compressionSide != strategy.SignalSideHold {
		e.compressionSignals++
		if _, err := e.compressionReplay.TryEnter(frame.Snapshot, compressionSide, eventEntryRegime(regime, compressionSide)); err != nil {
			return fmt.Errorf("enter compression replay: %w", err)
		}
	}

	flipSide, hasFlip := breakoutFlipSide(frame.Snapshot)
	combinedSide, conflict := breakoutCombinedSide(flipSide, hasFlip, compressionSide)
	if conflict {
		e.combinedReplay.SkipConflict()
		return nil
	}
	if combinedSide == strategy.SignalSideHold {
		return nil
	}
	combinedRegime := regime
	if compressionSide != strategy.SignalSideHold {
		combinedRegime = eventEntryRegime(regime, combinedSide)
	}
	e.combinedSignals++
	if _, err := e.combinedReplay.TryEnter(frame.Snapshot, combinedSide, combinedRegime); err != nil {
		return fmt.Errorf("enter combined replay: %w", err)
	}
	return nil
}

func (e *BreakoutExperiment) Finish(context.Context) (Result, error) {
	e.platformReplay.Finish()
	e.compressionReplay.Finish()
	e.combinedReplay.Finish()
	return Result{Descriptor: breakoutDescriptor, Summary: BreakoutSummary{Modes: []BreakoutModeSummary{
		{EntryMode: "platform", RawSignals: e.platformSignals, Replay: e.platformReplay.Summary()},
		{EntryMode: "compression", RawSignals: e.compressionSignals, Replay: e.compressionReplay.Summary()},
		{EntryMode: "flip_compression", RawSignals: e.combinedSignals, Replay: e.combinedReplay.Summary()},
	}}}, nil
}

func eventSide(events []signalresearch.PlatformEvent) strategy.SignalSide {
	if len(events) != 1 {
		return strategy.SignalSideHold
	}
	return events[0].Side
}

func breakoutFlipSide(snapshot strategy.Snapshot) (strategy.SignalSide, bool) {
	series, ok := snapshot.Window.Signal("supertrend_flip")
	if !ok {
		return strategy.SignalSideHold, false
	}
	switch strings.ToLower(strings.TrimSpace(series.Latest)) {
	case "up", "bull", "buy", "long":
		return strategy.SignalSideBuy, true
	case "down", "bear", "sell", "short":
		return strategy.SignalSideSell, true
	default:
		return strategy.SignalSideHold, false
	}
}

func breakoutCombinedSide(flipSide strategy.SignalSide, hasFlip bool, compressionSide strategy.SignalSide) (strategy.SignalSide, bool) {
	if !hasFlip {
		return compressionSide, false
	}
	if compressionSide == strategy.SignalSideHold || compressionSide == "" || compressionSide == flipSide {
		return flipSide, false
	}
	return strategy.SignalSideHold, true
}

func eventEntryRegime(current *marketregime.Result, side strategy.SignalSide) *marketregime.Result {
	result := marketregime.Result{State: marketregime.StateTrendArmed, AllowNewPosition: true}
	if current != nil {
		result = *current
		result.State = marketregime.StateTrendArmed
		result.AllowNewPosition = true
	}
	result.Direction = marketregime.DirectionLong
	result.AllowLong, result.AllowShort = true, false
	if side == strategy.SignalSideSell {
		result.Direction = marketregime.DirectionShort
		result.AllowLong, result.AllowShort = false, true
	}
	return &result
}
