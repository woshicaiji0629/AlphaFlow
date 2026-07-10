package strategy

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeStrategy struct {
	name      string
	intervals []string
	result    Result
	err       error
}

func (s fakeStrategy) Name() string {
	return s.name
}

func (s fakeStrategy) Requirements(target Target) Requirements {
	entry := target.Interval
	confirms := s.intervals
	if len(confirms) > 0 {
		entry = confirms[0]
		confirms = confirms[1:]
	}
	return Requirements{EntryInterval: entry, ConfirmIntervals: confirms, Trigger: TriggerOnEntryClose}
}

func (s fakeStrategy) Evaluate(
	ctx context.Context,
	snapshot Snapshot,
	position *Position,
) (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	return s.result, ctx.Err()
}

func TestEngineEvaluateReturnsStrategyResultsOnly(t *testing.T) {
	engine := NewEngine([]Strategy{
		fakeStrategy{
			name: "keltner",
			result: Result{
				StrategyName: "keltner",
				Signal:       Signal{Side: SignalSideBuy, Confidence: 0.8},
			},
		},
	})

	decision, err := engine.Evaluate(context.Background(), Context{
		Target: Target{Interval: "3m"},
		Snapshots: map[string]Snapshot{
			"3m": {Target: Target{Interval: "3m"}},
		},
		Positions: map[string]*Position{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(decision.Results))
	}
	if decision.Results[0].Signal.Side != SignalSideBuy {
		t.Fatalf("signal side = %q, want %q", decision.Results[0].Signal.Side, SignalSideBuy)
	}
}

func TestEngineEvaluateRequiresEntrySnapshot(t *testing.T) {
	engine := NewEngine([]Strategy{
		fakeStrategy{name: "keltner", intervals: []string{"3m"}},
	})

	decision, err := engine.Evaluate(context.Background(), Context{
		Target:    Target{Interval: "3m"},
		Snapshots: map[string]Snapshot{},
		Positions: map[string]*Position{},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(decision.Failures) != 1 || decision.Failures[0].StrategyName != "keltner" {
		t.Fatalf("failures = %#v, want keltner failure", decision.Failures)
	}
}

func TestEngineRequiredIntervalsUnionsStrategyRequirements(t *testing.T) {
	engine := NewEngine([]Strategy{
		fakeStrategy{name: "first", intervals: []string{"3m", "5m", "15m"}},
		fakeStrategy{name: "second", intervals: []string{"3m", "5m", "30m"}},
	})
	intervals, err := engine.RequiredIntervals(Target{Interval: "3m"})
	if err != nil {
		t.Fatalf("RequiredIntervals() error = %v", err)
	}
	want := []string{"3m", "5m", "15m", "30m"}
	if !reflect.DeepEqual(intervals, want) {
		t.Fatalf("intervals = %#v, want %#v", intervals, want)
	}
}

func TestEngineRejectsFutureTimeframeData(t *testing.T) {
	engine := NewEngine([]Strategy{fakeStrategy{name: "test"}})
	decision, err := engine.Evaluate(context.Background(), Context{
		Target: Target{Interval: "3m"},
		Snapshots: map[string]Snapshot{
			"3m": {
				AsOf:   1000,
				Window: IndicatorWindowView{CloseTime: 900},
				Timeframes: map[string]TimeframeSnapshot{
					"5m": {Window: IndicatorWindowView{CloseTime: 1200}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(decision.Failures) != 1 {
		t.Fatalf("failures = %#v, want timing failure", decision.Failures)
	}
}

func TestEngineContinuesAfterStrategyFailure(t *testing.T) {
	engine := NewEngine([]Strategy{
		fakeStrategy{name: "broken", err: errors.New("boom")},
		fakeStrategy{name: "healthy", result: Result{StrategyName: "healthy"}},
	})
	decision, err := engine.Evaluate(context.Background(), Context{
		Target: Target{Interval: "3m"},
		Snapshots: map[string]Snapshot{
			"3m": {},
		},
		Positions: map[string]*Position{},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(decision.Results) != 1 || decision.Results[0].StrategyName != "healthy" {
		t.Fatalf("results = %#v, want healthy result", decision.Results)
	}
	if len(decision.Failures) != 1 || decision.Failures[0].StrategyName != "broken" || decision.Failures[0].Error != "boom" {
		t.Fatalf("failures = %#v, want broken failure", decision.Failures)
	}
}

func TestEngineStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewEngine([]Strategy{fakeStrategy{name: "test"}}).Evaluate(ctx, Context{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Evaluate() error = %v, want context canceled", err)
	}
}
