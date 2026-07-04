package position

import (
	"context"

	"alphaflow/go-service/pkg/strategy"
)

type Store interface {
	GetPosition(ctx context.Context, key Key) (*strategy.Position, error)
	SavePosition(ctx context.Context, currentPosition strategy.Position) error
	DeletePosition(ctx context.Context, key Key) error
}

type EventStore interface {
	AppendEvent(ctx context.Context, event strategy.StrategyEvent) error
	AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error
}

type BacktestRunStore interface {
	SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error
}

type BacktestTempRegistry interface {
	RegisterTempKey(ctx context.Context, runID string, key string) error
	CleanupTempKeys(ctx context.Context, runID string) error
}
