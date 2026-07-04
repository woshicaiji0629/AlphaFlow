package strategy

import (
	"context"
	"testing"
)

type fakeStrategy struct {
	name      string
	intervals []string
	result    Result
}

func (s fakeStrategy) Name() string {
	return s.name
}

func (s fakeStrategy) RequiredIntervals(target Target) []string {
	if len(s.intervals) == 0 {
		return []string{target.Interval}
	}
	return s.intervals
}

func (s fakeStrategy) Evaluate(
	ctx context.Context,
	snapshot Snapshot,
	position *Position,
) (Result, error) {
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

	_, err := engine.Evaluate(context.Background(), Context{
		Target:    Target{Interval: "3m"},
		Snapshots: map[string]Snapshot{},
		Positions: map[string]*Position{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
