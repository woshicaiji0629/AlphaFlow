package marketstate

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
)

type Options struct {
	Now               func() int64
	MaxMessageAge     time.Duration
	RealtimeStaleAge  time.Duration
	ClosedStaleFactor int64
}

type Store struct {
	mu             sync.RWMutex
	now            func() int64
	options        Options
	items          map[string]intervalState
	windowBuilders map[string]*lockedWindowViewBuilder
}

type lockedWindowViewBuilder struct {
	mu      sync.Mutex
	builder *strategyframe.WindowViewBuilder
}

type intervalState struct {
	target            strategy.Target
	current           marketmodel.Kline
	closedIndicator   strategy.IndicatorView
	realtimeIndicator strategy.IndicatorView
	window            strategy.IndicatorWindowView
	health            strategy.HealthView
	updatedAt         int64
}

func New(options Options) *Store {
	if options.Now == nil {
		options.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if options.MaxMessageAge <= 0 {
		options.MaxMessageAge = 10 * time.Second
	}
	if options.RealtimeStaleAge <= 0 {
		options.RealtimeStaleAge = 15 * time.Second
	}
	if options.ClosedStaleFactor <= 0 {
		options.ClosedStaleFactor = 2
	}
	return &Store{
		now:            options.Now,
		options:        options,
		items:          map[string]intervalState{},
		windowBuilders: map[string]*lockedWindowViewBuilder{},
	}
}

func (s *Store) Seed(input strategy.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for interval, snapshot := range input.Snapshots {
		target := snapshot.Target
		if target.Interval == "" {
			target = input.Target
			target.Interval = interval
		}
		s.items[stateKey(target)] = intervalState{
			target:            target,
			current:           snapshot.Current,
			closedIndicator:   snapshot.Indicator,
			realtimeIndicator: snapshot.Indicator,
			window:            snapshot.Window,
			health:            snapshot.Health,
			updatedAt:         snapshot.UpdatedAt,
		}
	}
}

func (s *Store) Apply(envelope marketbus.SnapshotEnvelope) (bool, error) {
	if err := s.validateFreshMessage(envelope); err != nil {
		return false, err
	}
	target := strategy.Target{
		Exchange: envelope.Target.Exchange,
		Market:   envelope.Target.Market,
		Symbol:   envelope.Target.Symbol,
		Interval: envelope.Target.Interval,
	}
	key := stateKey(target)
	var builder *strategyframe.WindowViewBuilder
	if envelope.Window != nil {
		lockedBuilder := s.windowViewBuilder(key)
		lockedBuilder.mu.Lock()
		defer lockedBuilder.mu.Unlock()
		builder = lockedBuilder.builder
	}
	next, err := stateFromEnvelope(envelope, builder)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.items[key]
	if ok && staleUpdate(current, next, envelope.Type) {
		return false, nil
	}
	merged := mergeState(current, next)
	s.items[key] = merged
	return true, nil
}

func (s *Store) windowViewBuilder(key string) *lockedWindowViewBuilder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if builder := s.windowBuilders[key]; builder != nil {
		return builder
	}
	builder := &lockedWindowViewBuilder{builder: strategyframe.NewWindowViewBuilder()}
	s.windowBuilders[key] = builder
	return builder
}

func (s *Store) BuildContext(target strategy.Target, intervals []string) (strategy.Context, bool, string, error) {
	intervals = normalizeIntervals(target.Interval, intervals)
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make(map[string]strategy.Snapshot, len(intervals))
	degraded := false
	reasons := []string{}
	for _, interval := range intervals {
		itemTarget := target
		itemTarget.Interval = interval
		item, ok := s.items[stateKey(itemTarget)]
		if !ok {
			return strategy.Context{}, false, "", fmt.Errorf("market snapshot missing for %s/%s/%s/%s",
				itemTarget.Exchange,
				itemTarget.Market,
				itemTarget.Symbol,
				itemTarget.Interval,
			)
		}
		if item.window.UpdatedAt == 0 {
			return strategy.Context{}, false, "", fmt.Errorf("closed window missing for %s/%s/%s/%s",
				itemTarget.Exchange,
				itemTarget.Market,
				itemTarget.Symbol,
				itemTarget.Interval,
			)
		}
		if reason := s.degradedReason(item, interval == target.Interval); reason != "" {
			degraded = true
			reasons = append(reasons, reason)
		}
		item.target = itemTarget
		snapshots[interval] = snapshotFromState(item, interval == target.Interval)
	}
	context, err := strategyframe.BuildContext(
		target,
		snapshots,
		snapshots[target.Interval].Window.CloseTime,
		strategy.TriggerOnEntryClose,
	)
	if err != nil {
		return strategy.Context{}, false, "", err
	}
	return context, degraded, strings.Join(reasons, "; "), nil
}

func (s *Store) validateFreshMessage(envelope marketbus.SnapshotEnvelope) error {
	now := s.now()
	if envelope.ExpiresAt > 0 && envelope.ExpiresAt < now {
		return fmt.Errorf("market snapshot expired: trace_id=%s expires_at=%d now=%d", envelope.TraceID, envelope.ExpiresAt, now)
	}
	if envelope.CreatedAt > 0 && envelope.CreatedAt+s.options.MaxMessageAge.Milliseconds() < now {
		return fmt.Errorf("market snapshot too old: trace_id=%s created_at=%d max_age_ms=%d now=%d",
			envelope.TraceID,
			envelope.CreatedAt,
			s.options.MaxMessageAge.Milliseconds(),
			now,
		)
	}
	return nil
}

func (s *Store) degradedReason(item intervalState, requireRealtime bool) string {
	now := s.now()
	if requireRealtime && item.realtimeIndicator.UpdatedAt > 0 &&
		item.realtimeIndicator.UpdatedAt+s.options.RealtimeStaleAge.Milliseconds() < now {
		return fmt.Sprintf("realtime stale for %s/%s/%s/%s",
			item.target.Exchange,
			item.target.Market,
			item.target.Symbol,
			item.target.Interval,
		)
	}
	intervalMillis, err := marketmodel.IntervalMillis(item.target.Interval)
	if err != nil {
		return err.Error()
	}
	closedAgeLimit := intervalMillis * s.options.ClosedStaleFactor
	if item.window.UpdatedAt+closedAgeLimit < now {
		return fmt.Sprintf("closed window stale for %s/%s/%s/%s",
			item.target.Exchange,
			item.target.Market,
			item.target.Symbol,
			item.target.Interval,
		)
	}
	return ""
}

func stateFromEnvelope(envelope marketbus.SnapshotEnvelope, builder *strategyframe.WindowViewBuilder) (intervalState, error) {
	target := strategy.Target{
		Exchange: envelope.Target.Exchange,
		Market:   envelope.Target.Market,
		Symbol:   envelope.Target.Symbol,
		Interval: envelope.Target.Interval,
	}
	state := intervalState{
		target: target,
		health: strategy.HealthView{
			OK:        envelope.Health.OK,
			Reason:    envelope.Health.Reason,
			UpdatedAt: envelope.Health.UpdatedAt,
		},
	}
	if envelope.Indicator != nil {
		indicator := strategyframe.IndicatorView(*envelope.Indicator)
		if envelope.Type == marketbus.SnapshotTypeRealtime {
			state.realtimeIndicator = indicator
		} else {
			state.closedIndicator = indicator
		}
		state.updatedAt = maxInt64(state.updatedAt, indicator.UpdatedAt)
	}
	if envelope.Window != nil {
		var window strategy.IndicatorWindowView
		var err error
		if builder == nil {
			window, err = strategyframe.WindowView(*envelope.Window)
		} else {
			window, err = builder.FromSnapshot(*envelope.Window)
		}
		if err != nil {
			return intervalState{}, err
		}
		state.window = window
		state.updatedAt = maxInt64(state.updatedAt, state.window.UpdatedAt)
	}
	if envelope.Kline != nil {
		state.current = *envelope.Kline
		state.updatedAt = maxInt64(state.updatedAt, envelope.Kline.EventTime)
	}
	return state, nil
}

func staleUpdate(current intervalState, next intervalState, snapshotType string) bool {
	if snapshotType == marketbus.SnapshotTypeRealtime {
		return next.realtimeIndicator.OpenTime < current.realtimeIndicator.OpenTime ||
			(next.realtimeIndicator.OpenTime == current.realtimeIndicator.OpenTime && next.realtimeIndicator.UpdatedAt <= current.realtimeIndicator.UpdatedAt)
	}
	return next.window.OpenTime < current.window.OpenTime ||
		(next.window.OpenTime == current.window.OpenTime && next.window.UpdatedAt <= current.window.UpdatedAt)
}

func mergeState(current intervalState, next intervalState) intervalState {
	if current.target.Exchange == "" {
		return next
	}
	current.target = mergeTarget(current.target, next.target)
	if next.current.Symbol != "" {
		current.current = next.current
	}
	if next.closedIndicator.UpdatedAt > 0 {
		current.closedIndicator = next.closedIndicator
	}
	if next.realtimeIndicator.UpdatedAt > 0 {
		current.realtimeIndicator = next.realtimeIndicator
	}
	if next.window.UpdatedAt > 0 {
		current.window = next.window
	}
	if next.health.UpdatedAt > 0 || next.health.Reason != "" || next.health.OK {
		current.health = next.health
	}
	current.updatedAt = maxInt64(current.updatedAt, next.updatedAt)
	return current
}

func snapshotFromState(item intervalState, includeRealtime bool) strategy.Snapshot {
	current := marketmodel.Kline{}
	price := strategy.PriceView{}
	var realtime *strategy.RealtimeView
	if includeRealtime {
		current = item.current
		price = strategyframe.PriceView(item.realtimeIndicator, current)
		realtime = &strategy.RealtimeView{Current: current, Indicator: item.realtimeIndicator, Price: price}
	}
	return strategy.Snapshot{
		Target:    item.target,
		Current:   current,
		Indicator: item.closedIndicator,
		Window:    item.window,
		Price:     price,
		Health:    healthWithDefault(item.health),
		Realtime:  realtime,
		AsOf:      item.window.CloseTime,
		Trigger:   strategy.TriggerOnEntryClose,
		UpdatedAt: maxInt64(item.closedIndicator.UpdatedAt, item.window.UpdatedAt),
	}
}

func normalizeIntervals(entryInterval string, intervals []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(intervals)+1)
	if entryInterval != "" {
		normalized = append(normalized, entryInterval)
		seen[entryInterval] = true
	}
	for _, interval := range intervals {
		interval = strings.TrimSpace(interval)
		if interval == "" || seen[interval] {
			continue
		}
		normalized = append(normalized, interval)
		seen[interval] = true
	}
	return normalized
}

func mergeTarget(current strategy.Target, next strategy.Target) strategy.Target {
	current.Exchange = valueOr(current.Exchange, next.Exchange)
	current.Market = valueOr(current.Market, next.Market)
	current.Symbol = valueOr(current.Symbol, next.Symbol)
	current.Interval = valueOr(current.Interval, next.Interval)
	return current
}

func healthWithDefault(health strategy.HealthView) strategy.HealthView {
	if health.UpdatedAt == 0 && health.Reason == "" && !health.OK {
		health.OK = true
	}
	return health
}

func stateKey(target strategy.Target) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(target.Exchange)),
		strings.ToLower(strings.TrimSpace(target.Market)),
		strings.ToUpper(strings.TrimSpace(target.Symbol)),
		strings.TrimSpace(target.Interval),
	}, "|")
}

func valueOr(left string, right string) string {
	if left != "" {
		return left
	}
	return right
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
