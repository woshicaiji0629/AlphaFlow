package position

import (
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategy"
)

const redisKeyPrefix = "st"

type Key struct {
	Scope        strategy.PositionScope
	RunID        string
	Account      string
	Exchange     string
	Market       string
	Symbol       string
	StrategyName string
	PositionSide strategy.ExchangePositionSide
}

func RedisKey(item Key) (string, error) {
	switch item.Scope {
	case strategy.PositionScopeBacktest:
		if err := requireFields(map[string]string{
			"run_id":        item.RunID,
			"exchange":      item.Exchange,
			"market":        item.Market,
			"symbol":        item.Symbol,
			"strategy_name": item.StrategyName,
		}); err != nil {
			return "", err
		}
		return joinKey(redisKeyPrefix, "pos", string(item.Scope), item.RunID, item.Exchange, item.Market, item.Symbol, item.StrategyName), nil
	case strategy.PositionScopePaper:
		if err := requireFields(map[string]string{
			"exchange":      item.Exchange,
			"market":        item.Market,
			"symbol":        item.Symbol,
			"strategy_name": item.StrategyName,
		}); err != nil {
			return "", err
		}
		return joinKey(redisKeyPrefix, "pos", string(item.Scope), item.Exchange, item.Market, item.Symbol, item.StrategyName), nil
	case strategy.PositionScopeTestnet, strategy.PositionScopeLive:
		if err := requireFields(map[string]string{
			"account":       item.Account,
			"exchange":      item.Exchange,
			"market":        item.Market,
			"symbol":        item.Symbol,
			"position_side": string(item.PositionSide),
		}); err != nil {
			return "", err
		}
		return joinKey(redisKeyPrefix, "pos", string(item.Scope), item.Account, item.Exchange, item.Market, item.Symbol, string(item.PositionSide)), nil
	default:
		return "", fmt.Errorf("unsupported position scope %q", item.Scope)
	}
}

func BacktestTempKeysKey(runID string) (string, error) {
	if err := requireFields(map[string]string{"run_id": runID}); err != nil {
		return "", err
	}
	return joinKey(redisKeyPrefix, "bt", runID, "keys"), nil
}

func KeyFromPosition(currentPosition strategy.Position) Key {
	return Key{
		Scope:        currentPosition.Scope,
		RunID:        currentPosition.RunID,
		Account:      currentPosition.Account,
		Exchange:     currentPosition.Exchange,
		Market:       currentPosition.Market,
		Symbol:       currentPosition.Symbol,
		StrategyName: currentPosition.StrategyName,
		PositionSide: currentPosition.PositionSide,
	}
}

func requireFields(fields map[string]string) error {
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func joinKey(parts ...string) string {
	return strings.Join(parts, ":")
}
