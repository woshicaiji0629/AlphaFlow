package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/symbolspec"
)

type intentPublisher interface {
	PublishIntent(context.Context, execution.OrderIntent) error
}
type accountFanout struct {
	runtimes  []accountRuntime
	publisher intentPublisher
}

func newAccountFanout(runtimes []accountRuntime, publisher intentPublisher) *accountFanout {
	return &accountFanout{runtimes: runtimes, publisher: publisher}
}

func (f *accountFanout) Prepare(ctx context.Context, intent execution.OrderIntent) (execution.OrderIntent, bool, error) {
	for _, runtime := range f.runtimes {
		if runtime.account.ID != intent.Account || !strings.EqualFold(runtime.account.Exchange, intent.Exchange) {
			continue
		}
		if intent.Action == execution.OrderActionOpen && !subscribes(runtime, intent) {
			return execution.OrderIntent{}, false, nil
		}
		aligned, err := alignAccountAction(ctx, runtime, &intent)
		if err != nil {
			return execution.OrderIntent{}, false, err
		}
		if !aligned {
			return execution.OrderIntent{}, false, nil
		}
		quantity, err := accountQuantity(ctx, runtime, intent)
		if err != nil {
			return execution.OrderIntent{}, false, err
		}
		if quantity <= 0 {
			return execution.OrderIntent{}, false, fmt.Errorf("calculated quantity is not positive")
		}
		intent.Quantity = quantity
		return intent, true, nil
	}
	return execution.OrderIntent{}, false, fmt.Errorf("execution account %s:%s is not configured", intent.Exchange, intent.Account)
}

func (f *accountFanout) Publish(ctx context.Context, template execution.OrderIntent) error {
	for _, runtime := range f.runtimes {
		if !subscribes(runtime, template) {
			continue
		}
		intent, ok, err := buildAccountIntent(ctx, runtime, template)
		if err != nil {
			slog.Error("account plan fanout failed", "account", runtime.account.ID, "exchange", runtime.account.Exchange, "strategy", template.StrategyName, "error", err)
			continue
		}
		if !ok {
			continue
		}
		if err := f.publisher.PublishIntent(ctx, intent); err != nil {
			return err
		}
	}
	return nil
}
func subscribes(runtime accountRuntime, template execution.OrderIntent) bool {
	matched := false
	for _, name := range runtime.config.Strategies {
		if name == "*" || name == template.StrategyName {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	symbol := destinationSymbol(runtime, template.Symbol)
	if len(runtime.symbols) == 0 {
		return true
	}
	for _, allowed := range runtime.symbols {
		if strings.EqualFold(allowed, symbol) {
			return true
		}
	}
	return false
}
func destinationSymbol(runtime accountRuntime, source string) string {
	for key, value := range runtime.config.SymbolMap {
		if strings.EqualFold(key, source) {
			return value
		}
	}
	return source
}
func buildAccountIntent(ctx context.Context, runtime accountRuntime, template execution.OrderIntent) (execution.OrderIntent, bool, error) {
	intent := template
	intent.Account = runtime.account.ID
	intent.Exchange = runtime.account.Exchange
	intent.Market = runtime.account.Market
	intent.Symbol = destinationSymbol(runtime, template.Symbol)
	intent.Scope = string(strategy.PositionScopeTestnet)
	if runtime.account.Environment == "live" {
		intent.Scope = string(strategy.PositionScopeLive)
	}
	aligned, err := alignAccountAction(ctx, runtime, &intent)
	if err != nil {
		return execution.OrderIntent{}, false, err
	}
	if !aligned {
		return execution.OrderIntent{}, false, nil
	}
	if runtime.config.DisableShort && intent.Action == execution.OrderActionOpen && strings.EqualFold(intent.PositionSide, "short") {
		return execution.OrderIntent{}, false, nil
	}
	quantity, err := accountQuantity(ctx, runtime, intent)
	if err != nil {
		return execution.OrderIntent{}, false, err
	}
	if quantity <= 0 {
		return execution.OrderIntent{}, false, fmt.Errorf("calculated quantity is not positive")
	}
	intent.Quantity = quantity
	identity := strings.Join([]string{template.IntentID, intent.Scope, intent.Exchange, intent.Account, intent.Market, intent.Symbol, string(intent.Action), intent.PositionSide}, ":")
	intent.IntentID = identity
	intent.IdempotencyKey = identity
	return intent, true, nil
}
func alignAccountAction(ctx context.Context, runtime accountRuntime, intent *execution.OrderIntent) (bool, error) {
	if intent.Action != execution.OrderActionOpen {
		return true, nil
	}
	positions, err := runtime.adapter.Positions(ctx)
	if err != nil {
		return false, err
	}
	desired := strings.ToLower(intent.PositionSide)
	opposite := "short"
	if desired == "short" {
		opposite = "long"
	}
	for _, position := range positions {
		if !strings.EqualFold(position.Symbol, intent.Symbol) || position.Size <= 0 {
			continue
		}
		side := strings.ToLower(string(position.PositionSide))
		if side == desired {
			return false, nil
		}
		if side == opposite {
			intent.Action = execution.OrderActionClose
			intent.ReduceOnly = true
			intent.PositionSide = opposite
			if opposite == "long" {
				intent.Side = execution.OrderSideSell
			} else {
				intent.Side = execution.OrderSideBuy
			}
			return true, nil
		}
	}
	return true, nil
}
func accountQuantity(ctx context.Context, runtime accountRuntime, intent execution.OrderIntent) (float64, error) {
	if intent.Action == execution.OrderActionClose || intent.Action == execution.OrderActionReduce {
		positions, err := runtime.adapter.Positions(ctx)
		if err != nil {
			return 0, err
		}
		for _, position := range positions {
			if strings.EqualFold(position.Symbol, intent.Symbol) && strings.EqualFold(string(position.PositionSide), intent.PositionSide) {
				quantity := position.Size
				if intent.Action == execution.OrderActionReduce && intent.TriggeredRule != nil && intent.TriggeredRule.SizePct > 0 {
					quantity *= math.Min(intent.TriggeredRule.SizePct, 1)
				}
				return quantity, nil
			}
		}
		return 0, nil
	}
	price, err := strconv.ParseFloat(intent.ReferencePrice, 64)
	if err != nil || price <= 0 {
		return 0, fmt.Errorf("reference price is required")
	}
	margin := runtime.config.MarginQuote
	if runtime.config.AllocationPct > 0 || runtime.config.MaxMarginUsagePct > 0 {
		snapshot, err := runtime.adapter.Account(ctx)
		if err != nil {
			return 0, err
		}
		equity, err := strconv.ParseFloat(snapshot.Equity, 64)
		if err != nil {
			return 0, fmt.Errorf("parse account equity: %w", err)
		}
		if runtime.config.AllocationPct > 0 {
			margin = equity * runtime.config.AllocationPct
		}
		if runtime.config.MaxMarginUsagePct > 0 {
			margin = math.Min(margin, equity*runtime.config.MaxMarginUsagePct)
		}
	}
	notional := margin * runtime.config.Leverage
	if runtime.config.MaxPositionNotional > 0 {
		notional = math.Min(notional, runtime.config.MaxPositionNotional)
	}
	capability, err := runtime.adapter.Capability(ctx, intent.Symbol)
	if err != nil {
		return 0, err
	}
	spec := symbolspec.Capability{Exchange: capability.Exchange, Market: capability.Market, Symbol: capability.Symbol, PriceTick: parseNumber(capability.PriceTick), QuantityStep: parseNumber(capability.QtyStep), MinQuantity: parseNumber(capability.MinQty), MinNotional: parseNumber(capability.MinNotional), ContractSize: parseNumber(capability.ContractSize), QuantityUnit: capability.QuantityUnit}
	normalized, err := symbolspec.NormalizeQuoteOrder(spec, price, notional)
	if err != nil {
		return 0, err
	}
	return normalized.Quantity, nil
}
func parseNumber(value string) float64 { number, _ := strconv.ParseFloat(value, 64); return number }
