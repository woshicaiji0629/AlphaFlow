package position

import (
	"context"
	"sync"

	"alphaflow/go-service/pkg/strategy"
)

type MemoryStore struct {
	mu                sync.RWMutex
	positions         map[string]strategy.Position
	events            []strategy.StrategyEvent
	backtestSummaries map[string]strategy.BacktestRunSummary
	backtestTrades    []strategy.BacktestTrade
	tempKeys          map[string]map[string]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		positions:         map[string]strategy.Position{},
		backtestSummaries: map[string]strategy.BacktestRunSummary{},
		tempKeys:          map[string]map[string]struct{}{},
	}
}

func (s *MemoryStore) GetPosition(ctx context.Context, key Key) (*strategy.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	positionKey, err := RedisKey(key)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	currentPosition, ok := s.positions[positionKey]
	if !ok {
		return nil, nil
	}
	copied := copyPosition(currentPosition)
	return &copied, nil
}

func (s *MemoryStore) SavePosition(ctx context.Context, currentPosition strategy.Position) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	positionKey, err := RedisKey(KeyFromPosition(currentPosition))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positions[positionKey] = copyPosition(currentPosition)
	return nil
}

func (s *MemoryStore) DeletePosition(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	positionKey, err := RedisKey(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.positions, positionKey)
	return nil
}

func (s *MemoryStore) ListPositions(ctx context.Context, filter Filter) ([]strategy.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateListFilter(filter); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	positions := make([]strategy.Position, 0, len(s.positions))
	for _, currentPosition := range s.positions {
		if !positionMatchesFilter(currentPosition, filter) {
			continue
		}
		positions = append(positions, copyPosition(currentPosition))
	}
	return positions, nil
}

func (s *MemoryStore) AppendEvent(ctx context.Context, event strategy.StrategyEvent) error {
	return s.AppendEvents(ctx, []strategy.StrategyEvent{event})
}

func (s *MemoryStore) AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		s.events = append(s.events, copyStrategyEvent(event))
	}
	return nil
}

func (s *MemoryStore) SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backtestSummaries[summary.RunID] = copyBacktestRunSummary(summary)
	return nil
}

func (s *MemoryStore) SaveBacktestTrades(ctx context.Context, trades []strategy.BacktestTrade) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, trade := range trades {
		s.backtestTrades = append(s.backtestTrades, copyBacktestTrade(trade))
	}
	return nil
}

func (s *MemoryStore) RegisterTempKey(ctx context.Context, runID string, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := BacktestTempKeysKey(runID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tempKeys[runID] == nil {
		s.tempKeys[runID] = map[string]struct{}{}
	}
	s.tempKeys[runID][key] = struct{}{}
	return nil
}

func (s *MemoryStore) CleanupTempKeys(ctx context.Context, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := BacktestTempKeysKey(runID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.tempKeys[runID] {
		delete(s.positions, key)
	}
	delete(s.tempKeys, runID)
	return nil
}

func (s *MemoryStore) Events() []strategy.StrategyEvent {
	events, _ := s.EventsSince(0)
	return events
}

// DetachEvents transfers ownership of all stored events to the caller without
// copying them. It is intended for terminal consumers after event producers
// and incremental readers have stopped.
func (s *MemoryStore) DetachEvents() []strategy.StrategyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events
	s.events = nil
	return events
}

// EventsSince returns a copy of events appended at or after cursor and the
// cursor to use for the next incremental read.
func (s *MemoryStore) EventsSince(cursor int) ([]strategy.StrategyEvent, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(s.events) {
		cursor = len(s.events)
	}
	events := make([]strategy.StrategyEvent, 0, len(s.events)-cursor)
	for _, event := range s.events[cursor:] {
		events = append(events, copyStrategyEvent(event))
	}
	return events, len(s.events)
}

func (s *MemoryStore) BacktestRunSummary(runID string) (strategy.BacktestRunSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	summary, ok := s.backtestSummaries[runID]
	if !ok {
		return strategy.BacktestRunSummary{}, false
	}
	return copyBacktestRunSummary(summary), true
}

func (s *MemoryStore) BacktestTrades() []strategy.BacktestTrade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trades := make([]strategy.BacktestTrade, 0, len(s.backtestTrades))
	for _, trade := range s.backtestTrades {
		trades = append(trades, copyBacktestTrade(trade))
	}
	return trades
}

func copyPosition(currentPosition strategy.Position) strategy.Position {
	currentPosition.ExitRules = copyExitRules(currentPosition.ExitRules)
	return currentPosition
}

func copyExitRules(rules []strategy.ExitRule) []strategy.ExitRule {
	if rules == nil {
		return nil
	}
	copied := make([]strategy.ExitRule, len(rules))
	for index, rule := range rules {
		copied[index] = rule
		copied[index].Metadata = copyStringMap(rule.Metadata)
	}
	return copied
}

func copyStrategyEvent(event strategy.StrategyEvent) strategy.StrategyEvent {
	event.Metadata = copyStringMap(event.Metadata)
	return event
}

func copyBacktestRunSummary(summary strategy.BacktestRunSummary) strategy.BacktestRunSummary {
	summary.Symbols = append([]string(nil), summary.Symbols...)
	summary.Metadata = copyStringMap(summary.Metadata)
	return summary
}

func copyBacktestTrade(trade strategy.BacktestTrade) strategy.BacktestTrade {
	trade.Metadata = copyStringMap(trade.Metadata)
	return trade
}

func copyStringMap(items map[string]string) map[string]string {
	if items == nil {
		return nil
	}
	copied := make(map[string]string, len(items))
	for key, value := range items {
		copied[key] = value
	}
	return copied
}
