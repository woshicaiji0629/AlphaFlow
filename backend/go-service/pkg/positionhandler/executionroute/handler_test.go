package executionroute

import (
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
	"context"
	"testing"
)

type publisher struct{ intents []execution.OrderIntent }

func (p *publisher) PublishIntent(_ context.Context, i execution.OrderIntent) error {
	p.intents = append(p.intents, i)
	return nil
}
func TestHandlerPublishesAccountNeutralPlan(t *testing.T) {
	p := &publisher{}
	h, _ := New(position.NewManager(position.ManagerConfig{MaxPositionSize: 1, MinOpenConfidence: .5}), p, func() int64 { return 2 })
	input := strategy.Context{Target: strategy.Target{Exchange: "binance", Market: "um", Symbol: "BTCUSDT", Interval: "3m"}, Snapshots: map[string]strategy.Snapshot{"3m": {Price: strategy.PriceView{MarkPrice: "50000"}}}}
	result := strategy.Result{StrategyName: "supertrend", Signal: strategy.Signal{Side: strategy.SignalSideBuy, Confidence: .9, OpenTime: 1}}
	if err := h.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkLive}); err != nil {
		t.Fatal(err)
	}
	if len(p.intents) != 1 || p.intents[0].Account != "" || p.intents[0].Scope != "live" {
		t.Fatalf("intents=%#v", p.intents)
	}
}
