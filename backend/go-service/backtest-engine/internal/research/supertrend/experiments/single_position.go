package experiments

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
)

var singlePositionDescriptor = Descriptor{Name: "single_position_scan", Version: "v1"}

type namedSinglePositionReplay struct {
	name   string
	replay *signalresearch.SinglePositionReplay
}

// SinglePositionVariantSummary is the result of one protection configuration.
type SinglePositionVariantSummary struct {
	Variant string                               `json:"variant"`
	Replay  signalresearch.SinglePositionSummary `json:"replay"`
}

// SinglePositionSummary contains the baseline and optional parameter scan.
type SinglePositionSummary struct {
	Variants []SinglePositionVariantSummary `json:"variants"`
}

// SinglePositionExperiment compares position-protection configurations over
// the same normalized entry stream.
type SinglePositionExperiment struct {
	variants []namedSinglePositionReplay
}

// NewSinglePositionExperiment always builds the baseline and optionally adds
// the established protection-parameter scan.
func NewSinglePositionExperiment(base signalresearch.SinglePositionConfig, scan bool) (*SinglePositionExperiment, error) {
	configs := []struct {
		name   string
		config signalresearch.SinglePositionConfig
	}{{name: "baseline", config: base}}
	if scan {
		add := func(name string, stop float64, breakEven float64, trailing float64, drawdown float64) {
			config := base
			config.InitialStopBps = stop
			config.BreakEvenTriggerBps = breakEven
			config.TrailingTriggerBps = trailing
			config.TrailingDrawdownBps = drawdown
			configs = append(configs, struct {
				name   string
				config signalresearch.SinglePositionConfig
			}{name: name, config: config})
		}
		add("s50-be30-t75-d30", 50, 30, 75, 30)
		add("s50-be40-t75-d30", 50, 40, 75, 30)
		add("s50-be40-t100-d30", 50, 40, 100, 30)
		add("s50-be40-t100-d40", 50, 40, 100, 40)
		add("s70-be40-t100-d40", 70, 40, 100, 40)
		add("s70-be50-t100-d40", 70, 50, 100, 40)
	}

	variants := make([]namedSinglePositionReplay, 0, len(configs))
	for _, item := range configs {
		replay, err := signalresearch.NewSinglePositionReplay(item.config)
		if err != nil {
			return nil, fmt.Errorf("build single position variant %s: %w", item.name, err)
		}
		variants = append(variants, namedSinglePositionReplay{name: item.name, replay: replay})
	}
	return &SinglePositionExperiment{variants: variants}, nil
}

func (e *SinglePositionExperiment) Descriptor() Descriptor { return singlePositionDescriptor }

func (e *SinglePositionExperiment) OnFrame(_ context.Context, frame Frame) error {
	for _, variant := range e.variants {
		if err := variant.replay.Advance(frame.Snapshot.Current); err != nil {
			return fmt.Errorf("advance variant %s: %w", variant.name, err)
		}
	}
	if !frame.InWindow {
		return nil
	}
	if len(frame.Entries) > 1 {
		for _, variant := range e.variants {
			variant.replay.SkipConflict()
		}
		return nil
	}
	if len(frame.Entries) == 0 {
		return nil
	}
	var regime *marketregime.Result
	if frame.HasRegime {
		regime = &frame.Regime
	}
	for _, variant := range e.variants {
		if _, err := variant.replay.TryEnter(frame.Snapshot, frame.Entries[0].Side, regime); err != nil {
			return fmt.Errorf("enter variant %s: %w", variant.name, err)
		}
	}
	return nil
}

func (e *SinglePositionExperiment) Finish(context.Context) (Result, error) {
	summary := SinglePositionSummary{Variants: make([]SinglePositionVariantSummary, 0, len(e.variants))}
	for _, variant := range e.variants {
		variant.replay.Finish()
		summary.Variants = append(summary.Variants, SinglePositionVariantSummary{
			Variant: variant.name, Replay: variant.replay.Summary(),
		})
	}
	return Result{Descriptor: singlePositionDescriptor, Summary: summary}, nil
}
