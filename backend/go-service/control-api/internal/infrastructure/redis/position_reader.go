package redis

import (
	"context"
	"strconv"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
)

type PositionReader struct{ store position.Store }

func NewPositionReader(store position.Store) *PositionReader { return &PositionReader{store: store} }
func (r *PositionReader) ListPositions(ctx context.Context, scope, account string) ([]domain.DashboardPosition, error) {
	items, err := r.store.ListPositions(ctx, position.Filter{Scope: strategy.PositionScope(scope), Account: account})
	if err != nil {
		return nil, err
	}
	result := make([]domain.DashboardPosition, 0, len(items))
	for _, item := range items {
		entry, _ := strconv.ParseFloat(item.EntryPrice, 64)
		result = append(result, domain.DashboardPosition{ID: item.PositionID, Symbol: item.Symbol, Strategy: item.StrategyName, Side: string(item.Side), Account: item.Account, Scope: string(item.Scope), EntryPrice: entry})
	}
	return result, nil
}

var _ repository.PositionReader = (*PositionReader)(nil)
