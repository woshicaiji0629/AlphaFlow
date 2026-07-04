package simulator

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
)

type ExecutorOptions struct {
	Engine        *strategy.Engine
	Store         *position.MemoryStore
	ManagerConfig position.ManagerConfig
	FeeConfig     paperhandler.FeeConfig
	SizingConfig  paperhandler.SizingConfig
	Now           func() int64
}

type ExecutionSummary struct {
	Contexts       int
	Decisions      int
	Results        int
	Events         int
	OpenPositions  int
	OrderFills     int
	StrategyEvents []strategy.StrategyEvent
	BacktestTrades []strategy.BacktestTrade
	RunSummary     strategy.BacktestRunSummary
}

type Executor struct {
	engine     *strategy.Engine
	store      *position.MemoryStore
	dispatcher *strategyroute.Dispatcher
}

func NewExecutor(options ExecutorOptions) (*Executor, error) {
	if options.Engine == nil {
		return nil, fmt.Errorf("strategy engine is required")
	}
	if options.Store == nil {
		options.Store = position.NewMemoryStore()
	}
	if options.Now == nil {
		options.Now = func() int64 { return 0 }
	}
	handler, err := paperhandler.New(paperhandler.Options{
		PositionManager: position.NewManager(options.ManagerConfig),
		PositionStore:   options.Store,
		EventStore:      options.Store,
		Broker:          execution.NewPaperBroker("", options.Now),
		FeeConfig:       options.FeeConfig,
		SizingConfig:    options.SizingConfig,
		Now:             options.Now,
	})
	if err != nil {
		return nil, err
	}
	dispatcher, err := strategyroute.NewDispatcher(strategyroute.DispatcherOptions{
		Routes: []strategyroute.Route{{
			StrategyName: "*",
			Sink:         strategyroute.SinkBacktest,
			Enabled:      true,
		}},
		Handlers: map[strategyroute.Sink]strategyroute.ResultHandler{
			strategyroute.SinkBacktest: handler,
		},
	})
	if err != nil {
		return nil, err
	}
	return &Executor{
		engine:     options.Engine,
		store:      options.Store,
		dispatcher: dispatcher,
	}, nil
}

func (e *Executor) Execute(ctx context.Context, contexts []strategy.Context) (ExecutionSummary, error) {
	if e == nil {
		return ExecutionSummary{}, fmt.Errorf("executor is required")
	}
	summary := ExecutionSummary{Contexts: len(contexts)}
	for _, item := range contexts {
		if err := ctx.Err(); err != nil {
			return ExecutionSummary{}, err
		}
		input, err := e.contextWithPositions(ctx, item)
		if err != nil {
			return ExecutionSummary{}, err
		}
		decision, err := e.engine.Evaluate(ctx, input)
		if err != nil {
			return ExecutionSummary{}, err
		}
		if err := e.dispatcher.Dispatch(ctx, input, decision); err != nil {
			return ExecutionSummary{}, err
		}
		summary.Decisions++
		summary.Results += len(decision.Results)
	}
	events := e.store.Events()
	summary.Events = len(events)
	summary.StrategyEvents = events
	for _, event := range events {
		if event.EventType == strategy.EventTypeOrderFilled {
			summary.OrderFills++
		}
	}
	runID := firstRunID(contexts)
	if runID != "" {
		positions, err := e.store.ListPositions(ctx, position.Filter{
			Scope: strategy.PositionScopeBacktest,
			RunID: runID,
		})
		if err != nil {
			return ExecutionSummary{}, err
		}
		summary.OpenPositions = len(positions)
	}
	return summary, nil
}

func (e *Executor) contextWithPositions(ctx context.Context, item strategy.Context) (strategy.Context, error) {
	positions := make(map[string]*strategy.Position, len(e.engine.Strategies()))
	for _, itemStrategy := range e.engine.Strategies() {
		currentPosition, err := e.store.GetPosition(ctx, position.Key{
			Scope:        item.Target.Scope,
			RunID:        item.Target.RunID,
			Account:      item.Target.Account,
			Exchange:     item.Target.Exchange,
			Market:       item.Target.Market,
			Symbol:       item.Target.Symbol,
			StrategyName: itemStrategy.Name(),
			PositionSide: strategy.ExchangePositionSideNet,
		})
		if err != nil {
			return strategy.Context{}, fmt.Errorf("read position for strategy %s: %w", itemStrategy.Name(), err)
		}
		positions[itemStrategy.Name()] = currentPosition
	}
	item.Positions = positions
	return item, nil
}

func firstRunID(contexts []strategy.Context) string {
	for _, item := range contexts {
		if item.Target.RunID != "" {
			return item.Target.RunID
		}
	}
	return ""
}
