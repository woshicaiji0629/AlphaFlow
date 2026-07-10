package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Strategy interface {
	Name() string
	Requirements(target Target) Requirements
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

func (e *Engine) RequiredIntervals(target Target) ([]string, error) {
	intervals := []string{target.Interval}
	seen := map[string]struct{}{target.Interval: {}}
	for _, item := range e.strategies {
		requirements := item.Requirements(target)
		entryInterval := strings.TrimSpace(requirements.EntryInterval)
		if entryInterval == "" {
			entryInterval = target.Interval
		}
		if entryInterval != target.Interval {
			return nil, fmt.Errorf("strategy %s entry interval %s does not match target interval %s", item.Name(), entryInterval, target.Interval)
		}
		if requirements.Trigger != "" && requirements.Trigger != TriggerOnEntryClose {
			return nil, fmt.Errorf("strategy %s trigger %q is unsupported", item.Name(), requirements.Trigger)
		}
		for _, interval := range requirements.ConfirmIntervals {
			interval = strings.TrimSpace(interval)
			if interval == "" {
				return nil, fmt.Errorf("strategy %s confirm interval cannot be empty", item.Name())
			}
			if _, ok := seen[interval]; ok {
				continue
			}
			seen[interval] = struct{}{}
			intervals = append(intervals, interval)
		}
	}
	return intervals, nil
}

func (e *Engine) Evaluate(ctx context.Context, input Context) (Decision, error) {
	results := make([]Result, 0, len(e.strategies))
	failures := make([]StrategyFailure, 0)
	for _, item := range e.strategies {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
		startedAt := time.Now()
		snapshot, ok := entrySnapshot(input, item)
		if !ok {
			failures = append(failures, strategyFailure(item.Name(), "entry snapshot missing", startedAt))
			continue
		}
		if err := validateSnapshotTiming(snapshot); err != nil {
			failures = append(failures, strategyFailure(item.Name(), "snapshot timing invalid: "+err.Error(), startedAt))
			continue
		}
		result, err := item.Evaluate(ctx, snapshot, input.Positions[item.Name()])
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Decision{}, ctxErr
			}
			failures = append(failures, strategyFailure(item.Name(), err.Error(), startedAt))
			continue
		}
		results = append(results, result)
	}
	return Decision{
		Target:   input.Target,
		Results:  results,
		Failures: failures,
	}, nil
}

func strategyFailure(name string, message string, startedAt time.Time) StrategyFailure {
	return StrategyFailure{
		StrategyName:   name,
		Error:          message,
		DurationMillis: time.Since(startedAt).Milliseconds(),
	}
}

func validateSnapshotTiming(snapshot Snapshot) error {
	if snapshot.AsOf <= 0 {
		return nil
	}
	if snapshot.Window.CloseTime > snapshot.AsOf {
		return fmt.Errorf("entry window close_time=%d exceeds as_of=%d", snapshot.Window.CloseTime, snapshot.AsOf)
	}
	for interval, timeframe := range snapshot.Timeframes {
		if timeframe.Window.CloseTime > snapshot.AsOf {
			return fmt.Errorf("timeframe %s window close_time=%d exceeds as_of=%d", interval, timeframe.Window.CloseTime, snapshot.AsOf)
		}
		if timeframe.Indicator.CloseTime > snapshot.AsOf {
			return fmt.Errorf("timeframe %s indicator close_time=%d exceeds as_of=%d", interval, timeframe.Indicator.CloseTime, snapshot.AsOf)
		}
	}
	return nil
}

func entrySnapshot(input Context, strategy Strategy) (Snapshot, bool) {
	requirements := strategy.Requirements(input.Target)
	entryInterval := requirements.EntryInterval
	if entryInterval == "" {
		entryInterval = input.Target.Interval
	}
	snapshot, ok := input.Snapshots[entryInterval]
	if !ok {
		return Snapshot{}, false
	}
	for _, interval := range requirements.ConfirmIntervals {
		if _, exists := input.Snapshots[interval]; !exists {
			return Snapshot{}, false
		}
	}
	return snapshot, true
}
