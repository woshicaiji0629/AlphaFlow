package app

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
)

type positionListStore interface {
	position.Store
}

type positionScanResult struct {
	Position strategy.Position
	Price    strategy.PriceView
	Plan     *strategy.OrderPlan
}

func scanOpenPositions(
	ctx context.Context,
	store positionListStore,
	prices priceReader,
	dispatcher *strategyroute.Dispatcher,
	filter position.Filter,
) (int, error) {
	results, err := evaluateOpenPositions(ctx, store, prices, position.NewManager(position.ManagerConfig{}), filter)
	if err != nil {
		return 0, err
	}
	for _, result := range results {
		decision := scannerDecision(result)
		input := strategy.Context{
			Target:    decision.Target,
			Snapshots: snapshotsWithPrice(decision.Target, result.Price),
			Positions: map[string]*strategy.Position{
				result.Position.StrategyName: &result.Position,
			},
		}
		if err := dispatcher.Dispatch(ctx, input, decision); err != nil {
			return 0, err
		}
	}
	return len(results), nil
}

func evaluateOpenPositions(
	ctx context.Context,
	store positionListStore,
	prices priceReader,
	manager *position.Manager,
	filter position.Filter,
) ([]positionScanResult, error) {
	if store == nil {
		return nil, fmt.Errorf("position store is required")
	}
	if prices == nil {
		return nil, fmt.Errorf("price reader is required")
	}
	if manager == nil {
		return nil, fmt.Errorf("position manager is required")
	}
	positions, err := store.ListPositions(ctx, filter)
	if err != nil {
		return nil, err
	}
	results := []positionScanResult{}
	for _, currentPosition := range positions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if currentPosition.IsFlat() {
			continue
		}
		target := targetFromPosition(currentPosition)
		price, err := prices.ReadPrice(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("read price for %s %s %s: %w", currentPosition.Exchange, currentPosition.Market, currentPosition.Symbol, err)
		}
		currentPrice := priceValue(price)
		if currentPrice == "" {
			continue
		}
		refreshed := position.RefreshPositionExtremes(currentPosition, currentPrice)
		if err := store.SavePosition(ctx, refreshed); err != nil {
			return nil, fmt.Errorf("refresh position %s: %w", currentPosition.StrategyName, err)
		}
		plan := manager.PlanWithPrice(holdResult(refreshed), &refreshed, currentPrice)
		if plan == nil || plan.Action == strategy.PositionActionHold {
			continue
		}
		results = append(results, positionScanResult{
			Position: refreshed,
			Price:    price,
			Plan:     plan,
		})
	}
	return results, nil
}

func scannerDecision(result positionScanResult) strategy.Decision {
	target := targetFromPosition(result.Position)
	return strategy.Decision{
		Target: target,
		Results: []strategy.Result{{
			StrategyName: result.Position.StrategyName,
			Signal: strategy.Signal{
				Exchange:   result.Position.Exchange,
				Market:     result.Position.Market,
				Symbol:     result.Position.Symbol,
				Strategy:   result.Position.StrategyName,
				Side:       strategy.SignalSideHold,
				Confidence: 0,
				Reason:     "position_scanner",
			},
		}},
	}
}

func targetFromPosition(currentPosition strategy.Position) strategy.Target {
	return strategy.Target{
		Exchange: currentPosition.Exchange,
		Market:   currentPosition.Market,
		Symbol:   currentPosition.Symbol,
		Account:  currentPosition.Account,
		Scope:    currentPosition.Scope,
		RunID:    currentPosition.RunID,
	}
}

func priceValue(price strategy.PriceView) string {
	if price.LastPrice != "" {
		return price.LastPrice
	}
	return price.MarkPrice
}

func holdResult(currentPosition strategy.Position) strategy.Result {
	return strategy.Result{
		StrategyName: currentPosition.StrategyName,
		Signal: strategy.Signal{
			Exchange: currentPosition.Exchange,
			Market:   currentPosition.Market,
			Symbol:   currentPosition.Symbol,
			Strategy: currentPosition.StrategyName,
			Side:     strategy.SignalSideHold,
		},
	}
}
