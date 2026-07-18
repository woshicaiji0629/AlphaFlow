package fill

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
)

type Applier struct{ store position.Store }

func NewApplier(store position.Store) (*Applier, error) {
	if store == nil {
		return nil, fmt.Errorf("position store is required")
	}
	return &Applier{store: store}, nil
}

func (a *Applier) Apply(ctx context.Context, intent execution.OrderIntent, report execution.ExecutionReport) error {
	if report.Status != execution.ExecutionStatusFilled {
		return nil
	}
	key, err := positionKey(intent)
	if err != nil {
		return err
	}
	current, err := a.store.GetPosition(ctx, key)
	if err != nil {
		return err
	}
	switch intent.Action {
	case execution.OrderActionOpen:
		return a.store.SavePosition(ctx, strategy.Position{Scope: strategy.PositionScope(intent.Scope), RunID: intent.RunID, Exchange: intent.Exchange, Market: intent.Market, Symbol: intent.Symbol, Account: intent.Account, StrategyName: intent.StrategyName, Mode: strategy.ExchangePositionModeNet, PositionSide: strategy.ExchangePositionSide(intent.PositionSide), Side: positionSide(intent), Size: report.FilledQuantity, InitialSize: report.FilledQuantity, EntryPrice: report.AveragePrice, HighestPrice: report.AveragePrice, LowestPrice: report.AveragePrice, HighestPriceBarOpenTime: intent.BarOpenTime, LowestPriceBarOpenTime: intent.BarOpenTime, ExitRules: append([]strategy.ExitRule(nil), intent.ExitRules...), EntryTime: report.UpdatedAt, EntryReason: intent.Reason, UpdatedAt: report.UpdatedAt})
	case execution.OrderActionClose:
		return a.store.DeletePosition(ctx, key)
	case execution.OrderActionReduce:
		if current == nil {
			return nil
		}
		remaining := current.Size - report.FilledQuantity
		if remaining <= 0 {
			return a.store.DeletePosition(ctx, key)
		}
		updated := *current
		updated.Size = remaining
		updated.UpdatedAt = report.UpdatedAt
		if intent.TriggeredRule != nil {
			updated.ExitRules = removeRule(updated.ExitRules, *intent.TriggeredRule)
		}
		return a.store.SavePosition(ctx, updated)
	default:
		return nil
	}
}

func positionKey(intent execution.OrderIntent) (position.Key, error) {
	side := strategy.ExchangePositionSideNet
	scope := strategy.PositionScope(intent.Scope)
	if scope == strategy.PositionScopeTestnet || scope == strategy.PositionScopeLive {
		side = strategy.ExchangePositionSide(intent.PositionSide)
	}
	key := position.Key{Scope: scope, RunID: intent.RunID, Account: intent.Account, Exchange: intent.Exchange, Market: intent.Market, Symbol: intent.Symbol, StrategyName: intent.StrategyName, PositionSide: side}
	if _, err := position.RedisKey(key); err != nil {
		return position.Key{}, err
	}
	return key, nil
}
func positionSide(intent execution.OrderIntent) strategy.PositionSide {
	if intent.PositionSide == string(strategy.ExchangePositionSideShort) {
		return strategy.PositionSideShort
	}
	return strategy.PositionSideLong
}
func removeRule(rules []strategy.ExitRule, triggered strategy.ExitRule) []strategy.ExitRule {
	for i, rule := range rules {
		if rule.Type == triggered.Type && rule.Reason == triggered.Reason && rule.TriggerPrice == triggered.TriggerPrice && rule.SizePct == triggered.SizePct {
			return append(append([]strategy.ExitRule{}, rules[:i]...), rules[i+1:]...)
		}
	}
	return rules
}
