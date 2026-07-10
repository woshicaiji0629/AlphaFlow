package executionroute

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
	"context"
	"fmt"
)

type Publisher interface {
	PublishIntent(context.Context, execution.OrderIntent) error
}
type Handler struct {
	manager   *position.Manager
	publisher Publisher
	now       func() int64
}

func New(manager *position.Manager, publisher Publisher, now func() int64) (*Handler, error) {
	if manager == nil {
		return nil, fmt.Errorf("position manager is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("intent publisher is required")
	}
	if now == nil {
		now = func() int64 { return 0 }
	}
	return &Handler{manager: manager, publisher: publisher, now: now}, nil
}
func (h *Handler) HandleResult(ctx context.Context, input strategy.Context, result strategy.Result, route strategyroute.Route) error {
	if route.Sink != strategyroute.SinkTestnet && route.Sink != strategyroute.SinkLive {
		return nil
	}
	plan := h.manager.Plan(result, nil)
	if plan == nil || plan.Action == strategy.PositionActionHold {
		return nil
	}
	target := input.Target
	target.Scope = strategy.PositionScope(route.Sink)
	target.Account = ""
	intent, ok, err := execution.BuildOrderIntent(execution.IntentRequest{Target: target, StrategyName: result.StrategyName, Plan: *plan, BarOpenTime: result.Signal.OpenTime, ReferencePrice: currentPrice(input), CreatedAt: h.now()})
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return h.publisher.PublishIntent(ctx, intent)
}
func currentPrice(input strategy.Context) string {
	snapshot, ok := input.Snapshots[input.Target.Interval]
	if !ok {
		return ""
	}
	if snapshot.Price.MarkPrice != "" {
		return snapshot.Price.MarkPrice
	}
	if snapshot.Price.LastPrice != "" {
		return snapshot.Price.LastPrice
	}
	return snapshot.Current.Close
}
