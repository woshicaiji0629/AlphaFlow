package experiments

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func TestBreakoutExperimentOwnsReplayLifecycle(t *testing.T) {
	experiment, err := NewBreakoutExperiment(breakoutConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.Events.Platform = []signalresearch.PlatformEvent{{Side: strategy.SignalSideBuy}}
	frame.Events.CompressionBreakout = []signalresearch.PlatformEvent{{Side: strategy.SignalSideBuy}}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(BreakoutSummary)
	for _, mode := range summary.Modes {
		if mode.RawSignals != 1 || mode.Replay.Trades != 1 || mode.Replay.DatasetEndExits != 1 {
			t.Fatalf("mode=%s summary=%+v", mode.EntryMode, mode)
		}
	}
}

func TestBreakoutExperimentAdvancesOutsideEntryWindow(t *testing.T) {
	experiment, err := NewBreakoutExperiment(breakoutConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.InWindow = false
	frame.Events.Platform = []signalresearch.PlatformEvent{{Side: strategy.SignalSideBuy}}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(BreakoutSummary)
	for _, mode := range summary.Modes {
		if mode.RawSignals != 0 || mode.Replay.Trades != 0 {
			t.Fatalf("mode=%s summary=%+v", mode.EntryMode, mode)
		}
	}
}

func TestBreakoutExperimentRecordsCombinedConflict(t *testing.T) {
	experiment, err := NewBreakoutExperiment(breakoutConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	frame := breakoutFrame()
	frame.Events.CompressionBreakout = []signalresearch.PlatformEvent{{Side: strategy.SignalSideSell}}
	if err := experiment.OnFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	result, err := experiment.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary.(BreakoutSummary)
	combined := summary.Modes[2]
	if combined.RawSignals != 0 || combined.Replay.SkippedConflict != 1 {
		t.Fatalf("combined=%+v", combined)
	}
}

func breakoutConfigForTest() signalresearch.SinglePositionConfig {
	return signalresearch.SinglePositionConfig{
		InitialEquity: 10000, MarginQuote: 100, Leverage: 100,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: 12 * time.Hour, CooldownBars: 2, FeeRate: 0.0006, SlippageBps: 2,
	}
}

func breakoutFrame() Frame {
	return Frame{
		Snapshot: strategy.Snapshot{
			Current: marketmodel.Kline{OpenTime: 1, CloseTime: 2, Open: "100", High: "101", Low: "99", Close: "100"},
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
