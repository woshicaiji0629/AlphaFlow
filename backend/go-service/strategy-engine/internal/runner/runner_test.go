package runner

import (
	"context"
	"testing"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
)

type fixedStrategy struct {
	name         string
	result       strategy.Result
	seenPosition *strategy.Position
}

type captureBroker struct {
	intent execution.OrderIntent
}

type capturePublisher struct {
	decision strategy.Decision
	called   bool
}

func (p *capturePublisher) PublishDecision(ctx context.Context, decision strategy.Decision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.called = true
	p.decision = decision
	return nil
}

func (b *captureBroker) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := ctx.Err(); err != nil {
		return execution.ExecutionReport{}, err
	}
	b.intent = intent
	return execution.ExecutionReport{
		IntentID:       intent.IntentID,
		Status:         execution.ExecutionStatusFilled,
		FilledQuantity: intent.Quantity,
		AveragePrice:   intent.ReferencePrice,
		UpdatedAt:      2000,
	}, nil
}

func (s *fixedStrategy) Name() string {
	return s.name
}

func (s *fixedStrategy) RequiredIntervals(target strategy.Target) []string {
	return []string{target.Interval}
}

func (s *fixedStrategy) Evaluate(
	ctx context.Context,
	snapshot strategy.Snapshot,
	currentPosition *strategy.Position,
) (strategy.Result, error) {
	if err := ctx.Err(); err != nil {
		return strategy.Result{}, err
	}
	s.seenPosition = currentPosition
	return s.result, nil
}

func TestRunnerHandlePublishesDecisionWithoutLocalPaperDispatch(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideBuy,
				Confidence: 0.9,
				Reason:     "trend up",
				OpenTime:   1000,
			},
		},
	}
	store := position.NewMemoryStore()
	publisher := &capturePublisher{}
	runner, err := New(Options{
		Engine:        strategy.NewEngine([]strategy.Strategy{item}),
		Publisher:     publisher,
		PositionStore: store,
		Now:           func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "100"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !publisher.called {
		t.Fatal("publisher was not called")
	}
	if len(publisher.decision.Results) != 1 {
		t.Fatalf("len(published results) = %d, want 1", len(publisher.decision.Results))
	}
	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition != nil {
		t.Fatalf("position = %+v, want nil without local paper dispatch", currentPosition)
	}
}

func TestRunnerHandleExecutesPaperIntentAndAppendsEvents(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideBuy,
				Score:      0.8,
				Confidence: 0.9,
				Reason:     "trend up",
				OpenTime:   1000,
			},
		},
	}
	store := position.NewMemoryStore()
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("101", func() int64 { return 2000 }),
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "100"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(decision.Results) != 1 {
		t.Fatalf("len(decision.Results) = %d, want 1", len(decision.Results))
	}

	events := store.Events()
	if got, want := len(events), 3; got != want {
		t.Fatalf("len(events) = %d, want %d: %+v", got, want, events)
	}
	if events[0].EventType != strategy.EventTypeSignalGenerated {
		t.Fatalf("events[0].EventType = %q, want signal_generated", events[0].EventType)
	}
	if events[1].EventType != strategy.EventTypeOrderIntentCreated {
		t.Fatalf("events[1].EventType = %q, want order_intent_created", events[1].EventType)
	}
	if events[2].EventType != strategy.EventTypeOrderFilled {
		t.Fatalf("events[2].EventType = %q, want order_filled", events[2].EventType)
	}
	if events[2].Price != "101" {
		t.Fatalf("filled price = %q, want 101", events[2].Price)
	}
	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want opened paper position")
	}
	if currentPosition.Side != strategy.PositionSideLong {
		t.Fatalf("position side = %q, want long", currentPosition.Side)
	}
	if currentPosition.Size != 1 {
		t.Fatalf("position size = %v, want 1", currentPosition.Size)
	}
	if currentPosition.EntryPrice != "101" {
		t.Fatalf("entry price = %q, want 101", currentPosition.EntryPrice)
	}
}

func TestRunnerHandlePassesReferencePriceToBroker(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideBuy,
				Confidence: 0.9,
				Reason:     "trend up",
				OpenTime:   1000,
			},
		},
	}
	broker := &captureBroker{}
	store := position.NewMemoryStore()
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		Broker:          broker,
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Price: strategy.PriceView{LastPrice: "101.25"},
				Current: marketmodel.Kline{
					Close: "100",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if broker.intent.ReferencePrice != "101.25" {
		t.Fatalf("reference price = %q, want 101.25", broker.intent.ReferencePrice)
	}
}

func TestRunnerHandleUsesMarginLeverageNotionalSizingAndRebate(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideBuy,
				Score:      0.8,
				Confidence: 0.9,
				Reason:     "trend up",
				OpenTime:   1500,
			},
		},
	}
	store := position.NewMemoryStore()
	sizingConfig := SizingConfig{MarginQuote: 100, Leverage: 100}
	runner, err := New(Options{
		Engine: strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{
			MarginQuote: 100,
			Leverage:    100,
		}),
		PositionStore: store,
		EventStore:    store,
		Broker:        execution.NewPaperBroker("2000", func() int64 { return 2500 }),
		FeeConfig:     FeeConfig{FeeRate: 0.0006, RebatePct: 50},
		SizingConfig:  sizingConfig,
		Now:           func() int64 { return 2500 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "2000"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want opened position")
	}
	if currentPosition.Size != 5 {
		t.Fatalf("position size = %v, want 5", currentPosition.Size)
	}

	events := store.Events()
	filled := events[len(events)-1]
	if filled.Notional != "10000" {
		t.Fatalf("notional = %q, want 10000", filled.Notional)
	}
	if filled.Fee != "3" {
		t.Fatalf("fee = %q, want 3", filled.Fee)
	}
	if filled.Metadata["gross_fee"] != "6" {
		t.Fatalf("gross_fee = %q, want 6", filled.Metadata["gross_fee"])
	}
	if filled.Metadata["rebate"] != "3" {
		t.Fatalf("rebate = %q, want 3", filled.Metadata["rebate"])
	}
	if filled.Metadata["fee_rate"] != "0.0006" {
		t.Fatalf("fee_rate = %q, want 0.0006", filled.Metadata["fee_rate"])
	}
	if filled.Metadata["rebate_pct"] != "50" {
		t.Fatalf("rebate_pct = %q, want 50", filled.Metadata["rebate_pct"])
	}
	if filled.Metadata["margin_quote"] != "100" {
		t.Fatalf("margin_quote = %q, want 100", filled.Metadata["margin_quote"])
	}
	if filled.Metadata["leverage"] != "100" {
		t.Fatalf("leverage = %q, want 100", filled.Metadata["leverage"])
	}
}

func TestRunnerHandleHydratesPositionBeforeEvaluate(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "SOLUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "keltner",
		result: strategy.Result{
			StrategyName: "keltner",
			Signal: strategy.Signal{
				Strategy:   "keltner",
				Side:       strategy.SignalSideHold,
				Confidence: 1,
				OpenTime:   3000,
			},
		},
	}
	store := position.NewMemoryStore()
	if err := store.SavePosition(context.Background(), strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "SOLUSDT",
		StrategyName: "keltner",
		Side:         strategy.PositionSideLong,
		Size:         2,
	}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Now:             func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if item.seenPosition == nil {
		t.Fatal("strategy saw nil position, want hydrated position")
	}
	if item.seenPosition.Size != 2 {
		t.Fatalf("hydrated position size = %v, want 2", item.seenPosition.Size)
	}
	if got, want := len(store.Events()), 1; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
}

func TestRunnerHandleClosesPaperPositionAfterFill(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideSell,
				Score:      0.9,
				Confidence: 0.9,
				Reason:     "trend down",
				OpenTime:   5000,
			},
		},
	}
	store := position.NewMemoryStore()
	if err := store.SavePosition(context.Background(), strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         1,
	}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{DisableShortExposure: true}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("99", func() int64 { return 6000 }),
		Now:             func() int64 { return 6000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "100"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition != nil {
		t.Fatalf("position = %+v, want nil after close fill", currentPosition)
	}
}

func TestRunnerHandleTriggersStopLossAndPersistsExitMetrics(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideHold,
				Confidence: 1,
				OpenTime:   7000,
			},
		},
	}
	store := position.NewMemoryStore()
	if err := store.SavePosition(context.Background(), strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "100",
		HighestPrice: "100",
		LowestPrice:  "100",
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "hard stop",
			TriggerPrice: "95",
			SizePct:      1,
		}},
	}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("94", func() int64 { return 8000 }),
		FeeConfig:       FeeConfig{FeeRate: 0.001},
		Now:             func() int64 { return 8000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "94"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition != nil {
		t.Fatalf("position = %+v, want nil after stop loss", currentPosition)
	}
	events := store.Events()
	filled := events[len(events)-1]
	if filled.Reason != string(strategy.ExitReasonStopLoss) {
		t.Fatalf("filled reason = %q, want stop_loss", filled.Reason)
	}
	if filled.Notional != "94" {
		t.Fatalf("notional = %q, want 94", filled.Notional)
	}
	if filled.Fee != "0.194" {
		t.Fatalf("fee = %q, want 0.194", filled.Fee)
	}
	if filled.PnL != "-6.194" {
		t.Fatalf("pnl = %q, want -6.194", filled.PnL)
	}
	if filled.Metadata["exit_reason"] != string(strategy.ExitReasonStopLoss) {
		t.Fatalf("exit_reason metadata = %q, want stop_loss", filled.Metadata["exit_reason"])
	}
	if filled.Metadata["trigger_price"] != "95" {
		t.Fatalf("trigger_price metadata = %q, want 95", filled.Metadata["trigger_price"])
	}
}

func TestRunnerHandlePartialTakeProfitReducesPositionAndRemovesRule(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideHold,
				Confidence: 1,
				OpenTime:   9000,
			},
		},
	}
	store := position.NewMemoryStore()
	if err := store.SavePosition(context.Background(), strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         2,
		EntryPrice:   "100",
		HighestPrice: "100",
		LowestPrice:  "100",
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonTakeProfit,
			Reason:       "first target",
			TriggerPrice: "110",
			SizePct:      0.5,
		}},
	}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("111", func() int64 { return 10000 }),
		Now:             func() int64 { return 10000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "111"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want reduced position")
	}
	if currentPosition.Size != 1 {
		t.Fatalf("position size = %v, want 1", currentPosition.Size)
	}
	if len(currentPosition.ExitRules) != 0 {
		t.Fatalf("len(exit rules) = %d, want 0", len(currentPosition.ExitRules))
	}
	events := store.Events()
	filled := events[len(events)-1]
	if filled.Reason != string(strategy.ExitReasonTakeProfit) {
		t.Fatalf("filled reason = %q, want take_profit", filled.Reason)
	}
	if filled.Metadata["size_pct"] != "0.5" {
		t.Fatalf("size_pct metadata = %q, want 0.5", filled.Metadata["size_pct"])
	}
}

func TestRunnerHandleRefreshesTrailingStopReference(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	item := &fixedStrategy{
		name: "supertrend",
		result: strategy.Result{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideHold,
				Confidence: 1,
				OpenTime:   11000,
			},
		},
	}
	store := position.NewMemoryStore()
	if err := store.SavePosition(context.Background(), strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		Side:         strategy.PositionSideLong,
		Size:         1,
		EntryPrice:   "100",
		HighestPrice: "100",
		LowestPrice:  "100",
		ExitRules: []strategy.ExitRule{{
			Type:    strategy.ExitReasonTrailingStop,
			Reason:  "trail",
			SizePct: 1,
			Metadata: map[string]string{
				"trail_pct":       "2",
				"reference_price": "100",
			},
		}},
	}); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	runner, err := New(Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{item}),
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("110", func() int64 { return 12000 }),
		Now:             func() int64 { return 12000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runner.Handle(context.Background(), strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {
				Current: marketmodel.Kline{Close: "110"},
			},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want open position")
	}
	if currentPosition.HighestPrice != "110" {
		t.Fatalf("highest price = %q, want 110", currentPosition.HighestPrice)
	}
	if currentPosition.ExitRules[0].Metadata["reference_price"] != "110" {
		t.Fatalf("reference price = %q, want 110", currentPosition.ExitRules[0].Metadata["reference_price"])
	}
	if got, want := len(store.Events()), 1; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
}
