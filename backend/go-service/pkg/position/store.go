package position

import (
	"context"

	"alphaflow/go-service/pkg/strategy"
)

type Store interface {
	GetPosition(ctx context.Context, key Key) (*strategy.Position, error)
	SavePosition(ctx context.Context, currentPosition strategy.Position) error
	DeletePosition(ctx context.Context, key Key) error
	ListPositions(ctx context.Context, filter Filter) ([]strategy.Position, error)
}

type Filter struct {
	Scope    strategy.PositionScope
	RunID    string
	Account  string
	Exchange string
	Market   string
	Symbol   string
}

type EventStore interface {
	AppendEvent(ctx context.Context, event strategy.StrategyEvent) error
	AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error
}

type BacktestRunStore interface {
	SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error
}

type BacktestTradeStore interface {
	SaveBacktestTrades(ctx context.Context, trades []strategy.BacktestTrade) error
}

type BacktestTempRegistry interface {
	RegisterTempKey(ctx context.Context, runID string, key string) error
	CleanupTempKeys(ctx context.Context, runID string) error
}
