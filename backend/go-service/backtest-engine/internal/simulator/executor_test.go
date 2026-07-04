package simulator

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
)

func TestExecutorOpensBacktestPosition(t *testing.T) {
	store := position.NewMemoryStore()
	engine := strategy.NewEngine([]strategy.Strategy{fixedStrategy{
		name:       "fixed",
		signalSide: strategy.SignalSideBuy,
		confidence: 0.9,
	}})
	executor, err := NewExecutor(ExecutorOptions{
		Engine: engine,
		Store:  store,
		ManagerConfig: position.ManagerConfig{
			MarginQuote:       100,
			Leverage:          1,
			MaxPositionSize:   10,
			MinOpenConfidence: 0.5,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
			Scope:    strategy.PositionScopeBacktest,
			RunID:    "run-1",
		},
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{
					Close:    "100",
					OpenTime: 1000,
				},
			},
		},
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if summary.Contexts != 1 || summary.Decisions != 1 || summary.Results != 1 {
		t.Fatalf("summary counts = %#v, want one context/decision/result", summary)
	}
	if summary.OrderFills != 1 {
		t.Fatalf("order fills = %d, want 1", summary.OrderFills)
	}
	if summary.OpenPositions != 1 {
		t.Fatalf("open positions = %d, want 1", summary.OpenPositions)
	}
	if summary.RunSummary.RunID != "" {
		t.Fatalf("run summary = %#v, want zero value before app-level summary build", summary.RunSummary)
	}
	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopeBacktest,
		RunID:        "run-1",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "fixed",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want opened backtest position")
	}
	if currentPosition.EntryPrice != "100" {
		t.Fatalf("entry price = %q, want 100", currentPosition.EntryPrice)
	}
}

type fixedStrategy struct {
	name       string
	signalSide strategy.SignalSide
	confidence float64
}

func (s fixedStrategy) Name() string {
	return s.name
}

func (s fixedStrategy) RequiredIntervals(target strategy.Target) []string {
	return []string{target.Interval}
}

func (s fixedStrategy) Evaluate(ctx context.Context, snapshot strategy.Snapshot, currentPosition *strategy.Position) (strategy.Result, error) {
	if err := ctx.Err(); err != nil {
		return strategy.Result{}, err
	}
	return strategy.Result{
		StrategyName: s.name,
		Signal: strategy.Signal{
			Exchange:   snapshot.Target.Exchange,
			Market:     snapshot.Target.Market,
			Symbol:     snapshot.Target.Symbol,
			Interval:   snapshot.Target.Interval,
			Strategy:   s.name,
			Side:       s.signalSide,
			Confidence: s.confidence,
			OpenTime:   snapshot.Current.OpenTime,
		},
	}, nil
}
