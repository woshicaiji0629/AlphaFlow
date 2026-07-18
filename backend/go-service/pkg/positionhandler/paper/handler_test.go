package paper

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyroute"
	"alphaflow/go-service/pkg/symbolspec"
)

func TestHandlerResumesFilledIntentWithoutExecutingBrokerAgain(t *testing.T) {
	positions := position.NewMemoryStore()
	store := &failOncePositionStore{Store: positions, failSave: true}
	intents := execution.NewMemoryIntentStore()
	broker := &countingBroker{Broker: execution.NewPaperBroker("101", func() int64 { return 2000 })}
	handler, err := New(Options{
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		IntentStore:     intents,
		Broker:          broker,
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	input := strategy.Context{
		Target:    strategy.Target{Scope: strategy.PositionScopePaper, Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"},
		Snapshots: map[string]strategy.Snapshot{"3m": {Current: marketmodel.Kline{Close: "100"}}},
		Positions: map[string]*strategy.Position{"supertrend": nil},
	}
	result := strategy.Result{
		StrategyName: "supertrend",
		Signal:       strategy.Signal{Strategy: "supertrend", Side: strategy.SignalSideBuy, Confidence: 0.9, OpenTime: 1000},
	}

	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkPaper}); err == nil {
		t.Fatal("first HandleResult() error = nil, want position save failure")
	}
	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkPaper}); err != nil {
		t.Fatalf("second HandleResult() error = %v", err)
	}
	if broker.executes != 1 {
		t.Fatalf("broker executes = %d, want 1", broker.executes)
	}
	current, err := positions.GetPosition(context.Background(), position.Key{Scope: strategy.PositionScopePaper, Exchange: "binance", Market: "um", Symbol: "ETHUSDT", StrategyName: "supertrend"})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if current == nil || current.EntryPrice != "101" {
		t.Fatalf("position = %#v, want recovered fill", current)
	}
}

func TestHandlerOpensPaperPositionAndAppendsEvents(t *testing.T) {
	store := position.NewMemoryStore()
	handler, err := New(Options{
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("101", func() int64 { return 2000 }),
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	input := strategy.Context{
		Target: strategy.Target{
			Scope:    strategy.PositionScopePaper,
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		},
		Snapshots: map[string]strategy.Snapshot{
			"3m": {Current: marketmodel.Kline{Close: "100"}},
		},
		Positions: map[string]*strategy.Position{"supertrend": nil},
	}
	result := strategy.Result{
		StrategyName: "supertrend",
		Signal: strategy.Signal{
			Strategy:   "supertrend",
			Side:       strategy.SignalSideBuy,
			Confidence: 0.9,
			Reason:     "trend up",
			OpenTime:   1000,
		},
		Analysis: strategy.Analysis{Summary: "trend up", Checks: []strategy.DiagnosticCheck{{
			Name: "trend", Side: strategy.SignalSideBuy, Status: strategy.DiagnosticStatusPass,
		}}},
	}

	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkPaper}); err != nil {
		t.Fatalf("HandleResult() error = %v", err)
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
	if currentPosition.EntryPrice != "101" {
		t.Fatalf("entry price = %q, want 101", currentPosition.EntryPrice)
	}
	if currentPosition.HighestPriceBarOpenTime != 1000 || currentPosition.LowestPriceBarOpenTime != 1000 {
		t.Fatalf("position extreme times = %d/%d, want 1000/1000", currentPosition.HighestPriceBarOpenTime, currentPosition.LowestPriceBarOpenTime)
	}
	events := store.Events()
	if got, want := len(events), 3; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	if events[0].EventType != strategy.EventTypeSignalGenerated {
		t.Fatalf("first event = %q, want signal_generated", events[0].EventType)
	}
	var analysis strategy.Analysis
	if err := json.Unmarshal([]byte(events[0].Metadata["analysis"]), &analysis); err != nil {
		t.Fatalf("decode event analysis: %v", err)
	}
	if len(analysis.Checks) != 1 || analysis.Checks[0].Name != "trend" {
		t.Fatalf("event analysis = %#v", analysis)
	}
	if err := json.Unmarshal([]byte(events[2].Metadata["analysis"]), &analysis); err != nil {
		t.Fatalf("decode filled event analysis: %v", err)
	}
	if len(analysis.Checks) != 1 || analysis.Checks[0].Name != "trend" {
		t.Fatalf("filled event analysis = %#v", analysis)
	}
}

type failOncePositionStore struct {
	position.Store
	failSave bool
}

func (s *failOncePositionStore) SavePosition(ctx context.Context, current strategy.Position) error {
	if s.failSave {
		s.failSave = false
		return errors.New("injected position save failure")
	}
	return s.Store.SavePosition(ctx, current)
}

type countingBroker struct {
	execution.Broker
	executes int
}

func (b *countingBroker) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	b.executes++
	return b.Broker.Execute(ctx, intent)
}

func TestHandlerUsesSymbolCapabilityForContractQuantity(t *testing.T) {
	store := position.NewMemoryStore()
	handler, err := New(Options{
		PositionManager: position.NewManager(position.ManagerConfig{
			MarginQuote:       100,
			Leverage:          1,
			MinOpenConfidence: 0.5,
		}),
		PositionStore: store,
		EventStore:    store,
		Broker:        execution.NewPaperBroker("10", func() int64 { return 2000 }),
		SizingConfig: SizingConfig{
			MarginQuote: 100,
			Leverage:    1,
			Capabilities: map[symbolspec.Key]symbolspec.Capability{
				symbolspec.NewKey("gate", "um", "BTCUSDT"): {
					Exchange:     "gate",
					Market:       "um",
					Symbol:       "BTCUSDT",
					QuantityUnit: symbolspec.QuantityUnitContract,
					QuantityStep: 1,
					MinQuantity:  1,
					MinNotional:  5,
					ContractSize: 0.1,
				},
			},
		},
		Now: func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	input := strategy.Context{
		Target: strategy.Target{
			Scope:    strategy.PositionScopeBacktest,
			RunID:    "run-1",
			Exchange: "gate",
			Market:   "um",
			Symbol:   "BTCUSDT",
			Interval: "3m",
		},
		Snapshots: map[string]strategy.Snapshot{
			"3m": {Current: marketmodel.Kline{Close: "10"}},
		},
		Positions: map[string]*strategy.Position{"supertrend": nil},
	}
	result := strategy.Result{
		StrategyName: "supertrend",
		Signal: strategy.Signal{
			Strategy:   "supertrend",
			Side:       strategy.SignalSideBuy,
			Confidence: 0.9,
			OpenTime:   1000,
		},
	}

	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkBacktest}); err != nil {
		t.Fatalf("HandleResult() error = %v", err)
	}
	currentPosition, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopeBacktest,
		RunID:        "run-1",
		Exchange:     "gate",
		Market:       "um",
		Symbol:       "BTCUSDT",
		StrategyName: "supertrend",
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if currentPosition == nil {
		t.Fatal("position = nil, want opened position")
	}
	if currentPosition.Size != 100 {
		t.Fatalf("position size = %f, want 100 contracts", currentPosition.Size)
	}
}

func TestHandlerBacktestStopUsesIntrabarTriggerPrice(t *testing.T) {
	store := position.NewMemoryStore()
	target := strategy.Target{Scope: strategy.PositionScopeBacktest, RunID: "run-1", Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"}
	currentPosition := strategy.Position{
		Scope:                   target.Scope,
		RunID:                   target.RunID,
		Exchange:                target.Exchange,
		Market:                  target.Market,
		Symbol:                  target.Symbol,
		StrategyName:            "supertrend",
		Side:                    strategy.PositionSideLong,
		Size:                    1,
		EntryPrice:              "100",
		HighestPrice:            "105",
		LowestPrice:             "95",
		HighestPriceBarOpenTime: 500,
		LowestPriceBarOpenTime:  600,
		EntryTime:               500,
		ExitRules: []strategy.ExitRule{{
			Type:         strategy.ExitReasonStopLoss,
			Reason:       "strategy stop loss",
			TriggerPrice: "90",
			SizePct:      1,
		}},
	}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	handler, err := New(Options{
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		EventStore:      store,
		Broker:          execution.NewPaperBroker("", func() int64 { return 2000 }),
		Now:             func() int64 { return 2000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	input := strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": {Current: marketmodel.Kline{Open: "100", High: "105", Low: "89", Close: "100"}},
		},
		Positions: map[string]*strategy.Position{"supertrend": &currentPosition},
	}
	result := strategy.Result{StrategyName: "supertrend", Signal: strategy.Signal{Strategy: "supertrend", Side: strategy.SignalSideHold, OpenTime: 1000}}

	if err := handler.HandleResult(context.Background(), input, result, strategyroute.Route{Sink: strategyroute.SinkBacktest}); err != nil {
		t.Fatalf("HandleResult() error = %v", err)
	}
	stored, err := store.GetPosition(context.Background(), position.KeyFromPosition(currentPosition))
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if stored != nil {
		t.Fatalf("position = %#v, want closed", stored)
	}
	events := store.Events()
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	filled := events[len(events)-1]
	if filled.EventType != strategy.EventTypeOrderFilled || filled.Price != "90" {
		t.Fatalf("filled event = %#v, want intrabar fill at 90", filled)
	}
	if filled.Metadata["trigger_price"] != "90" {
		t.Fatalf("trigger price = %q, want 90", filled.Metadata["trigger_price"])
	}
	for key, want := range map[string]string{
		"mfe_price":            "105",
		"mae_price":            "90",
		"mfe_bps":              "500",
		"mae_bps":              "1000",
		"exit_move_bps":        "-1000",
		"profit_giveback_bps":  "1500",
		"mfe_bar_open_time_ms": "500",
		"mae_bar_open_time_ms": "1000",
		"holding_time_ms":      "1500",
	} {
		if filled.Metadata[key] != want {
			t.Fatalf("%s = %q, want %q metadata=%#v", key, filled.Metadata[key], want, filled.Metadata)
		}
	}
}

func TestPositionExcursionMetadataUsesTrailingReferenceForShort(t *testing.T) {
	currentPosition := &strategy.Position{
		Side:                    strategy.PositionSideShort,
		Size:                    1,
		EntryPrice:              "100",
		HighestPrice:            "106",
		LowestPrice:             "90",
		HighestPriceBarOpenTime: 1200,
		LowestPriceBarOpenTime:  1400,
		EntryTime:               1000,
	}
	plan := &strategy.OrderPlan{TriggeredRule: &strategy.ExitRule{
		Type:     strategy.ExitReasonTrailingStop,
		Metadata: map[string]string{"reference_price": "88"},
	}}

	metadata := positionExcursionMetadata(currentPosition, execution.ExecutionReport{AveragePrice: "92", UpdatedAt: 3000}, plan, 2000)

	for key, want := range map[string]string{
		"mfe_price":            "88",
		"mae_price":            "106",
		"mfe_bps":              "1200",
		"mae_bps":              "600",
		"exit_move_bps":        "800",
		"profit_giveback_bps":  "400",
		"mfe_bar_open_time_ms": "2000",
		"mae_bar_open_time_ms": "1200",
		"holding_time_ms":      "2000",
	} {
		if metadata[key] != want {
			t.Fatalf("%s = %q, want %q metadata=%#v", key, metadata[key], want, metadata)
		}
	}
}
