package strategy

import (
	"context"
	"fmt"
)

type Strategy interface {
	Name() string
	RequiredIntervals(target Target) []string
	Evaluate(ctx context.Context, snapshot Snapshot, position *Position) (Result, error)
}

type Engine struct {
	strategies []Strategy
}

func NewEngine(strategies []Strategy) *Engine {
	copied := make([]Strategy, len(strategies))
	copy(copied, strategies)
	return &Engine{
		strategies: copied,
	}
}

func (e *Engine) Strategies() []Strategy {
	copied := make([]Strategy, len(e.strategies))
	copy(copied, e.strategies)
	return copied
}

func (e *Engine) Evaluate(ctx context.Context, input Context) (Decision, error) {
	results := make([]Result, 0, len(e.strategies))
	for _, item := range e.strategies {
		snapshot, ok := entrySnapshot(input, item)
		if !ok {
			return Decision{}, fmt.Errorf("strategy %s entry snapshot missing", item.Name())
		}
		result, err := item.Evaluate(ctx, snapshot, input.Positions[item.Name()])
		if err != nil {
			return Decision{}, fmt.Errorf("evaluate strategy %s: %w", item.Name(), err)
		}
		results = append(results, result)
	}
	return Decision{
		Target:  input.Target,
		Results: results,
	}, nil
}

func entrySnapshot(input Context, strategy Strategy) (Snapshot, bool) {
	for _, interval := range strategy.RequiredIntervals(input.Target) {
		if snapshot, ok := input.Snapshots[interval]; ok {
			return snapshot, true
		}
	}
	snapshot, ok := input.Snapshots[input.Target.Interval]
	return snapshot, ok
}
