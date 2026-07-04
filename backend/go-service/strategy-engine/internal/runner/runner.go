package runner

import (
	"context"
	"fmt"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
)

type FeeConfig = paperhandler.FeeConfig
type SizingConfig = paperhandler.SizingConfig

type Options struct {
	Engine     *strategy.Engine
	Dispatcher *strategyroute.Dispatcher

	PositionManager *position.Manager
	PositionStore   position.Store
	EventStore      position.EventStore
	Broker          execution.Broker
	FeeConfig       FeeConfig
	SizingConfig    SizingConfig
	Now             func() int64
}

type Runner struct {
	engine        *strategy.Engine
	dispatcher    *strategyroute.Dispatcher
	positionStore position.Store
}

func New(options Options) (*Runner, error) {
	if options.Engine == nil {
		return nil, fmt.Errorf("strategy engine is required")
	}
	dispatcher := options.Dispatcher
	if dispatcher == nil {
		built, err := buildPaperDispatcher(options)
		if err != nil {
			return nil, err
		}
		dispatcher = built
	}
	return &Runner{
		engine:        options.Engine,
		dispatcher:    dispatcher,
		positionStore: options.PositionStore,
	}, nil
}

func buildPaperDispatcher(options Options) (*strategyroute.Dispatcher, error) {
	handler, err := paperhandler.New(paperhandler.Options{
		PositionManager: options.PositionManager,
		PositionStore:   options.PositionStore,
		EventStore:      options.EventStore,
		Broker:          options.Broker,
		FeeConfig:       options.FeeConfig,
		SizingConfig:    options.SizingConfig,
		Now:             options.Now,
	})
	if err != nil {
		return nil, err
	}
	return strategyroute.NewDispatcher(strategyroute.DispatcherOptions{
		Routes: []strategyroute.Route{{
			StrategyName: "*",
			Sink:         strategyroute.SinkPaper,
			Enabled:      true,
		}},
		Handlers: map[strategyroute.Sink]strategyroute.ResultHandler{
			strategyroute.SinkPaper: handler,
		},
	})
}

func (r *Runner) Handle(ctx context.Context, input strategy.Context) (strategy.Decision, error) {
	if err := r.hydratePositions(ctx, &input); err != nil {
		return strategy.Decision{}, err
	}
	decision, err := r.engine.Evaluate(ctx, input)
	if err != nil {
		return strategy.Decision{}, err
	}
	if r.dispatcher != nil {
		if err := r.dispatcher.Dispatch(ctx, input, decision); err != nil {
			return strategy.Decision{}, err
		}
	}
	return decision, nil
}

func (r *Runner) hydratePositions(ctx context.Context, input *strategy.Context) error {
	if r.positionStore == nil {
		return nil
	}
	if input.Positions == nil {
		input.Positions = map[string]*strategy.Position{}
	}
	for _, item := range r.engine.Strategies() {
		name := item.Name()
		if input.Positions[name] != nil {
			continue
		}
		currentPosition, err := r.positionStore.GetPosition(ctx, positionKey(input.Target, name))
		if err != nil {
			return fmt.Errorf("get position for strategy %s: %w", name, err)
		}
		input.Positions[name] = currentPosition
	}
	return nil
}

func positionKey(target strategy.Target, strategyName string) position.Key {
	return position.Key{
		Scope:        target.Scope,
		RunID:        target.RunID,
		Account:      target.Account,
		Exchange:     target.Exchange,
		Market:       target.Market,
		Symbol:       target.Symbol,
		StrategyName: strategyName,
		PositionSide: strategy.ExchangePositionSideNet,
	}
}
