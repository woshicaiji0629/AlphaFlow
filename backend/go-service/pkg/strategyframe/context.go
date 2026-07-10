package strategyframe

import (
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

func BuildContext(
	target strategy.Target,
	source map[string]strategy.Snapshot,
	asOf int64,
	trigger strategy.TriggerMode,
) (strategy.Context, error) {
	if strings.TrimSpace(target.Interval) == "" {
		return strategy.Context{}, fmt.Errorf("target interval cannot be empty")
	}
	if _, ok := source[target.Interval]; !ok {
		return strategy.Context{}, fmt.Errorf("entry snapshot %s missing", target.Interval)
	}
	snapshots := make(map[string]strategy.Snapshot, len(source))
	for interval, snapshot := range source {
		if strings.TrimSpace(interval) == "" {
			return strategy.Context{}, fmt.Errorf("snapshot interval cannot be empty")
		}
		if err := validateIdentity(target, interval, snapshot.Target); err != nil {
			return strategy.Context{}, err
		}
		if asOf > 0 && snapshot.Window.CloseTime > asOf {
			return strategy.Context{}, fmt.Errorf("snapshot %s window close_time=%d exceeds as_of=%d", interval, snapshot.Window.CloseTime, asOf)
		}
		if asOf > 0 && snapshot.Indicator.CloseTime > asOf {
			return strategy.Context{}, fmt.Errorf("snapshot %s indicator close_time=%d exceeds as_of=%d", interval, snapshot.Indicator.CloseTime, asOf)
		}
		snapshot.Target = target
		snapshot.Target.Interval = interval
		snapshot.AsOf = asOf
		snapshot.Trigger = trigger
		snapshots[interval] = snapshot
	}
	timeframes := make(map[string]strategy.TimeframeSnapshot, len(snapshots))
	for interval, snapshot := range snapshots {
		timeframes[interval] = strategy.TimeframeSnapshot{
			Interval:  interval,
			Indicator: snapshot.Indicator,
			Window:    snapshot.Window,
			Health:    snapshot.Health,
			UpdatedAt: snapshot.UpdatedAt,
		}
	}
	for interval, snapshot := range snapshots {
		snapshot.Timeframes = timeframes
		snapshots[interval] = snapshot
	}
	return strategy.Context{Target: target, Snapshots: snapshots}, nil
}

func validateIdentity(target strategy.Target, interval string, snapshotTarget strategy.Target) error {
	checks := []struct{ name, expected, actual string }{
		{"exchange", target.Exchange, snapshotTarget.Exchange},
		{"market", target.Market, snapshotTarget.Market},
		{"symbol", target.Symbol, snapshotTarget.Symbol},
		{"interval", interval, snapshotTarget.Interval},
	}
	for _, check := range checks {
		if check.actual != "" && !strings.EqualFold(check.expected, check.actual) {
			return fmt.Errorf("snapshot %s %s=%q does not match target %q", interval, check.name, check.actual, check.expected)
		}
	}
	return nil
}
