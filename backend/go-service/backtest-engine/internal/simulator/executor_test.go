package simulator

import (
	"context"
	"errors"
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/position"
	paper "alphaflow/go-service/pkg/positionhandler/paper"
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

func TestExecutorIncrementalRetainsEventStateWithoutMaterializingHistory(t *testing.T) {
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

	first, err := executor.ExecuteIncremental(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
	})
	if err != nil {
		t.Fatalf("first ExecuteIncremental() error = %v", err)
	}
	second, err := executor.ExecuteIncremental(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 2000, "101"),
	})
	if err != nil {
		t.Fatalf("second ExecuteIncremental() error = %v", err)
	}
	if first.OrderFills != 1 || second.OrderFills != 1 {
		t.Fatalf("order fills = %d then %d, want cumulative 1", first.OrderFills, second.OrderFills)
	}
	if first.Events == 0 || second.Events < first.Events {
		t.Fatalf("events = %d then %d, want cumulative non-decreasing counts", first.Events, second.Events)
	}
	if first.StrategyEvents != nil || second.StrategyEvents != nil {
		t.Fatal("incremental execution materialized full event history")
	}
}

func TestExecutorAppliesConfiguredSlippage(t *testing.T) {
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
		SlippageBps: 100,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	if _, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
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
	if currentPosition.EntryPrice != "101" {
		t.Fatalf("entry price = %q, want 101", currentPosition.EntryPrice)
	}
}

func TestExecutorTracksBarEquityCurveWithUnrealizedPnL(t *testing.T) {
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
		SizingConfig: paper.SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
		executorTestContext("ETHUSDT", 2000, "110"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(summary.BarEquityCurve) != 2 {
		t.Fatalf("bar equity curve len = %d, want 2", len(summary.BarEquityCurve))
	}
	first := summary.BarEquityCurve[0]
	if first.RealizedPnL != 0 || first.UnrealizedPnL != 0 || first.Equity != 0 {
		t.Fatalf("first equity point = %#v, want flat equity after entry at same price", first)
	}
	second := summary.BarEquityCurve[1]
	if second.Symbol != "ETHUSDT" || second.Price != 110 {
		t.Fatalf("second equity identity = %#v, want ETHUSDT at 110", second)
	}
	if second.RealizedPnL != 0 || second.UnrealizedPnL != 10 || second.Equity != 10 {
		t.Fatalf("second equity point = %#v, want unrealized/equity 10", second)
	}
}

func TestExecutorBlocksOpenWhenAccountCannotCoverMarginAndFee(t *testing.T) {
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
		SizingConfig: paper.SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
		},
		AccountConfig: AccountConfig{
			InitialEquity: 50,
			MarginQuote:   100,
			Leverage:      1,
			FeeRate:       0.001,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if summary.Results != 0 || summary.OrderFills != 0 || summary.OpenPositions != 0 {
		t.Fatalf("summary = %#v, want no order execution", summary)
	}
	if len(summary.AccountCurve) != 1 {
		t.Fatalf("account curve len = %d, want 1", len(summary.AccountCurve))
	}
	point := summary.AccountCurve[0]
	if point.StoppedReason != "insufficient_available_balance" {
		t.Fatalf("stopped reason = %q, want insufficient_available_balance", point.StoppedReason)
	}
}

func TestExecutorTracksAccountFeesRebatesAndMargin(t *testing.T) {
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
		FeeConfig: paper.FeeConfig{
			FeeRate:   0.01,
			RebatePct: 50,
		},
		SizingConfig: paper.SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
		},
		AccountConfig: AccountConfig{
			InitialEquity: 1000,
			MarginQuote:   100,
			Leverage:      1,
			FeeRate:       0.01,
			RebatePct:     50,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
		executorTestContext("ETHUSDT", 2000, "110"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(summary.AccountCurve) != 2 {
		t.Fatalf("account curve len = %d, want 2", len(summary.AccountCurve))
	}
	first := summary.AccountCurve[0]
	if first.Balance != 999.5 || first.UsedMargin != 100 || first.Fee != 0.5 || first.Rebate != 0.5 {
		t.Fatalf("first account point = %#v, want entry fee/rebate and margin", first)
	}
	second := summary.AccountCurve[1]
	if second.Equity != 1009.5 || second.UnrealizedPnL != 10 || second.AvailableBalance != 909.5 {
		t.Fatalf("second account point = %#v, want equity 1009.5 and available 909.5", second)
	}
}

func TestExecutorWritesOneAccountPointPerOpenTimeBatch(t *testing.T) {
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
		SizingConfig: paper.SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
		},
		AccountConfig: AccountConfig{
			InitialEquity: 1000,
			MarginQuote:   100,
			Leverage:      1,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("BTCUSDT", 1000, "100"),
		executorTestContext("ETHUSDT", 1000, "100"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(summary.AccountCurve) != 1 {
		t.Fatalf("account curve len = %d, want 1 account point for one open_time batch", len(summary.AccountCurve))
	}
	if summary.AccountCurve[0].UsedMargin != 200 {
		t.Fatalf("used margin = %f, want 200 for two opened positions", summary.AccountCurve[0].UsedMargin)
	}
}

func TestExecutorRefreshesBatchPricesBeforeOpenChecks(t *testing.T) {
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
			MarginQuote:       50,
			Leverage:          1,
			MaxPositionSize:   10,
			MinOpenConfidence: 0.5,
		},
		SizingConfig: paper.SizingConfig{
			MarginQuote: 50,
			Leverage:    1,
		},
		AccountConfig: AccountConfig{
			InitialEquity: 100,
			MarginQuote:   50,
			Leverage:      1,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if _, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("BTCUSDT", 1000, "100"),
	}); err != nil {
		t.Fatalf("initial Execute() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 2000, "100"),
		executorTestContext("BTCUSDT", 2000, "0"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if summary.OrderFills != 1 {
		t.Fatalf("order fills = %d, want only the existing BTC fill", summary.OrderFills)
	}
	ethPosition, err := store.GetPosition(context.Background(), position.Key{
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
	if ethPosition != nil {
		t.Fatalf("ETH position = %#v, want nil because BTC price refresh consumed available balance", ethPosition)
	}
	last := summary.AccountCurve[len(summary.AccountCurve)-1]
	if last.StoppedReason != "insufficient_available_balance" {
		t.Fatalf("stopped reason = %q, want insufficient_available_balance", last.StoppedReason)
	}
}

func TestExecutorLiquidatesAccountWhenEquityFallsToZero(t *testing.T) {
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
		SizingConfig: paper.SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
		},
		AccountConfig: AccountConfig{
			InitialEquity: 100,
			MarginQuote:   100,
			Leverage:      1,
		},
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
		executorTestContext("ETHUSDT", 2000, "0"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	last := summary.AccountCurve[len(summary.AccountCurve)-1]
	if !last.Liquidated || last.Equity != 0 || last.StoppedReason != "liquidated" {
		t.Fatalf("last account point = %#v, want liquidation at zero equity", last)
	}
	if summary.OpenPositions != 0 {
		t.Fatalf("open positions = %d, want 0 after liquidation", summary.OpenPositions)
	}
}

func executorTestContext(symbol string, openTime int64, close string) strategy.Context {
	return strategy.Context{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   symbol,
			Interval: "3m",
			Scope:    strategy.PositionScopeBacktest,
			RunID:    "run-1",
		},
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{
					Close:    close,
					OpenTime: openTime,
				},
			},
		},
	}
}

type fixedStrategy struct {
	name       string
	signalSide strategy.SignalSide
	confidence float64
	err        error
}

func (s fixedStrategy) Name() string {
	return s.name
}

func (s fixedStrategy) Requirements(target strategy.Target) strategy.Requirements {
	return strategy.Requirements{EntryInterval: target.Interval, Trigger: strategy.TriggerOnEntryClose}
}

func (s fixedStrategy) Evaluate(ctx context.Context, snapshot strategy.Snapshot, currentPosition *strategy.Position) (strategy.Result, error) {
	if err := ctx.Err(); err != nil {
		return strategy.Result{}, err
	}
	if s.err != nil {
		return strategy.Result{}, s.err
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

func TestExecutorReturnsPartialSummaryForStrategyFailure(t *testing.T) {
	executor, err := NewExecutor(ExecutorOptions{
		Engine: strategy.NewEngine([]strategy.Strategy{fixedStrategy{name: "broken", err: errors.New("boom")}}),
		Store:  position.NewMemoryStore(),
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	summary, err := executor.Execute(context.Background(), []strategy.Context{
		executorTestContext("ETHUSDT", 1000, "100"),
		executorTestContext("ETHUSDT", 2000, "101"),
	})
	var evaluationErr StrategyEvaluationError
	if !errors.As(err, &evaluationErr) {
		t.Fatalf("Execute() error = %v, want StrategyEvaluationError", err)
	}
	if summary.Decisions != 1 || len(summary.Failures) != 1 || summary.Failures[0].StrategyName != "broken" {
		t.Fatalf("summary = %#v, want first-decision failure", summary)
	}
}
