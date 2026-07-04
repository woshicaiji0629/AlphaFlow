package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategybus"
	"alphaflow/go-service/pkg/strategyroute"
	"alphaflow/go-service/position-engine/internal/config"
)

func TestRunLoadsRoutes(t *testing.T) {
	originalBuilder := buildDecisionReader
	originalProcessorBuilder := buildDecisionProcessor
	t.Cleanup(func() {
		buildDecisionReader = originalBuilder
		buildDecisionProcessor = originalProcessorBuilder
	})
	reader := &fakeDecisionReader{
		messages:        []strategybus.DecisionMessage{{ID: "1-0"}},
		pendingMessages: []strategybus.DecisionMessage{{ID: "2-0", DeliveryCount: 2}},
	}
	closed := false
	buildDecisionReader = func(ctx context.Context, cfg config.Config) (decisionReader, func(), error) {
		return reader, func() { closed = true }, nil
	}
	processor := &fakeDecisionProcessor{}
	processorClosed := false
	buildDecisionProcessor = func(ctx context.Context, cfg config.Config, routes []strategyroute.Route) (decisionProcessor, func(), error) {
		return processor, func() { processorClosed = true }, nil
	}
	path := writeConfig(t, `
[redis]
addr = "localhost:6380"

[input]
stream = "st:decision:stream"
group = "position-engine"
consumer = "test"
block = "1s"
batch = 1
default_ttl = "60s"

[position]
scope = "paper"
account = "paper-default"

[sizing]
margin_quote = 100
leverage = 100
max_position_size = 1
min_open_confidence = 0.65

[fee]
fee_rate = 0.0006
rebate_pct = 0

[[routes]]
strategy = "supertrend"
sink = "paper"
enabled = true

[logging]
output = "stdout"
`)

	if err := Run(context.Background(), path); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reader.ensured {
		t.Fatal("consumer group ensured = false, want true")
	}
	if !reader.read {
		t.Fatal("read decisions = false, want true")
	}
	if len(processor.messages) != 2 {
		t.Fatalf("processed messages = %d, want 2", len(processor.messages))
	}
	if len(reader.acked) != 2 || reader.acked[0] != "1-0" || reader.acked[1] != "2-0" {
		t.Fatalf("acked = %v, want [1-0 2-0]", reader.acked)
	}
	if !closed {
		t.Fatal("close reader = false, want true")
	}
	if !processorClosed {
		t.Fatal("close processor = false, want true")
	}
}

func TestRunDeadLettersAfterMaxDeliveries(t *testing.T) {
	originalBuilder := buildDecisionReader
	originalProcessorBuilder := buildDecisionProcessor
	t.Cleanup(func() {
		buildDecisionReader = originalBuilder
		buildDecisionProcessor = originalProcessorBuilder
	})
	reader := &fakeDecisionReader{
		pendingMessages: []strategybus.DecisionMessage{{ID: "2-0", DeliveryCount: 5}},
	}
	buildDecisionReader = func(ctx context.Context, cfg config.Config) (decisionReader, func(), error) {
		return reader, func() {}, nil
	}
	buildDecisionProcessor = func(ctx context.Context, cfg config.Config, routes []strategyroute.Route) (decisionProcessor, func(), error) {
		return failingDecisionProcessor{}, func() {}, nil
	}
	path := writeConfig(t, `
[redis]
addr = "localhost:6380"

[input]
stream = "st:decision:stream"
group = "position-engine"
consumer = "test"
block = "1s"
batch = 1
default_ttl = "60s"
pending_idle = "30s"
dead_letter_stream = "st:decision:stream:dead"
max_deliveries = 5

[position]
scope = "paper"
account = "paper-default"

[sizing]
margin_quote = 100
leverage = 100
max_position_size = 1
min_open_confidence = 0.65

[fee]
fee_rate = 0.0006
rebate_pct = 0

[[routes]]
strategy = "supertrend"
sink = "paper"
enabled = true

[logging]
output = "stdout"
`)

	if err := Run(context.Background(), path); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(reader.deadLetters) != 1 || reader.deadLetters[0].ID != "2-0" {
		t.Fatalf("dead letters = %#v, want message 2-0", reader.deadLetters)
	}
	if len(reader.acked) != 1 || reader.acked[0] != "2-0" {
		t.Fatalf("acked = %v, want [2-0]", reader.acked)
	}
}

func TestPaperDecisionProcessorKeepsExpiredExitPendingWithoutPrice(t *testing.T) {
	store := position.NewMemoryStore()
	processor := newTestPaperDecisionProcessor(t, store, strategy.PriceView{})
	currentPosition := testLongPosition()
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}

	shouldAck, err := processor.ProcessDecision(context.Background(), expiredSellMessage())
	if err != nil {
		t.Fatalf("ProcessDecision() error = %v", err)
	}
	if shouldAck {
		t.Fatal("shouldAck = true, want false for expired exit signal")
	}
}

func TestPaperDecisionProcessorRechecksExpiredRiskExit(t *testing.T) {
	store := position.NewMemoryStore()
	processor := newTestPaperDecisionProcessor(t, store, strategy.PriceView{LastPrice: "94"})
	currentPosition := testLongPosition()
	currentPosition.ExitRules = []strategy.ExitRule{{
		Type:         strategy.ExitReasonStopLoss,
		Reason:       "stop loss",
		TriggerPrice: "95",
	}}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}

	shouldAck, err := processor.ProcessDecision(context.Background(), expiredSellMessage())
	if err != nil {
		t.Fatalf("ProcessDecision() error = %v", err)
	}
	if !shouldAck {
		t.Fatal("shouldAck = false, want true after risk recheck")
	}
	got, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Account:      "paper-default",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		PositionSide: strategy.ExchangePositionSideNet,
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if got != nil {
		t.Fatalf("position = %#v, want closed", got)
	}
}

func TestPaperDecisionProcessorAcksExpiredExitWhenRiskNotTriggered(t *testing.T) {
	store := position.NewMemoryStore()
	processor := newTestPaperDecisionProcessor(t, store, strategy.PriceView{LastPrice: "101"})
	currentPosition := testLongPosition()
	currentPosition.ExitRules = []strategy.ExitRule{{
		Type:         strategy.ExitReasonStopLoss,
		Reason:       "stop loss",
		TriggerPrice: "95",
	}}
	if err := store.SavePosition(context.Background(), currentPosition); err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}

	shouldAck, err := processor.ProcessDecision(context.Background(), expiredSellMessage())
	if err != nil {
		t.Fatalf("ProcessDecision() error = %v", err)
	}
	if !shouldAck {
		t.Fatal("shouldAck = false, want true when risk exit is not triggered")
	}
	got, err := store.GetPosition(context.Background(), position.Key{
		Scope:        strategy.PositionScopePaper,
		Account:      "paper-default",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		PositionSide: strategy.ExchangePositionSideNet,
	})
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}
	if got == nil {
		t.Fatal("position = nil, want still open")
	}
}

func TestPaperDecisionProcessorAcksExpiredEntry(t *testing.T) {
	processor := newTestPaperDecisionProcessor(t, position.NewMemoryStore(), strategy.PriceView{LastPrice: "101"})
	shouldAck, err := processor.ProcessDecision(context.Background(), strategybus.DecisionMessage{
		ID: "1-0",
		Envelope: strategybus.DecisionEnvelope{
			Target: strategy.Target{
				Exchange: "binance",
				Market:   "um",
				Symbol:   "ETHUSDT",
				Account:  "paper-default",
				Scope:    strategy.PositionScopePaper,
			},
			Results: []strategy.Result{{
				StrategyName: "supertrend",
				Signal: strategy.Signal{
					Side: strategy.SignalSideBuy,
				},
			}},
			CreatedAt: 1,
			ExpiresAt: 1_000,
		},
	})
	if err != nil {
		t.Fatalf("ProcessDecision() error = %v", err)
	}
	if !shouldAck {
		t.Fatal("shouldAck = false, want true for expired entry signal without position")
	}
}

func newTestPaperDecisionProcessor(t *testing.T, store *position.MemoryStore, price strategy.PriceView) *paperDecisionProcessor {
	t.Helper()
	now := func() int64 { return 2_000 }
	handler, err := paperhandler.New(paperhandler.Options{
		PositionManager: position.NewManager(position.ManagerConfig{}),
		PositionStore:   store,
		Broker:          execution.NewPaperBroker("", now),
		Now:             now,
	})
	if err != nil {
		t.Fatalf("paper handler: %v", err)
	}
	dispatcher, err := strategyroute.NewDispatcher(strategyroute.DispatcherOptions{
		Routes: []strategyroute.Route{{
			StrategyName: "supertrend",
			Sink:         strategyroute.SinkPaper,
			Enabled:      true,
		}},
		Handlers: map[strategyroute.Sink]strategyroute.ResultHandler{
			strategyroute.SinkPaper: handler,
		},
	})
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	return &paperDecisionProcessor{
		dispatcher:     dispatcher,
		positionStore:  store,
		prices:         fakePriceReader{price: price},
		defaultScope:   strategy.PositionScopePaper,
		defaultAccount: "paper-default",
		defaultTTL:     time.Second,
		now:            now,
	}
}

func testLongPosition() strategy.Position {
	return strategy.Position{
		Scope:        strategy.PositionScopePaper,
		Account:      "paper-default",
		Exchange:     "binance",
		Market:       "um",
		Symbol:       "ETHUSDT",
		StrategyName: "supertrend",
		PositionSide: strategy.ExchangePositionSideNet,
		Side:         strategy.PositionSideLong,
		Size:         1,
		InitialSize:  1,
		EntryPrice:   "100",
		HighestPrice: "100",
		LowestPrice:  "100",
	}
}

func expiredSellMessage() strategybus.DecisionMessage {
	return strategybus.DecisionMessage{
		ID: "1-0",
		Envelope: strategybus.DecisionEnvelope{
			Target: strategy.Target{
				Exchange: "binance",
				Market:   "um",
				Symbol:   "ETHUSDT",
				Interval: "3m",
				Account:  "paper-default",
				Scope:    strategy.PositionScopePaper,
			},
			Results: []strategy.Result{{
				StrategyName: "supertrend",
				Signal: strategy.Signal{
					Side:       strategy.SignalSideSell,
					Confidence: 0.9,
					OpenTime:   1_000,
				},
			}},
			CreatedAt: 1,
			ExpiresAt: 1_000,
		},
	}
}

func TestPaperDecisionProcessorContextIncludesPrice(t *testing.T) {
	store := position.NewMemoryStore()
	processor := &paperDecisionProcessor{
		positionStore: store,
		prices: fakePriceReader{price: strategy.PriceView{
			LastPrice: "101.25",
			MarkPrice: "101.2",
		}},
		defaultScope:   strategy.PositionScopePaper,
		defaultAccount: "paper-default",
		defaultTTL:     time.Second,
		now:            func() int64 { return 2_000 },
	}
	decision := strategy.Decision{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
			Account:  "paper-default",
			Scope:    strategy.PositionScopePaper,
		},
		Results: []strategy.Result{{
			StrategyName: "supertrend",
		}},
	}

	input, err := processor.contextForDecision(context.Background(), decision)
	if err != nil {
		t.Fatalf("contextForDecision() error = %v", err)
	}
	price := input.Snapshots["3m"].Price
	if price.LastPrice != "101.25" {
		t.Fatalf("last price = %q, want 101.25", price.LastPrice)
	}
	if price.MarkPrice != "101.2" {
		t.Fatalf("mark price = %q, want 101.2", price.MarkPrice)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

type fakeDecisionReader struct {
	ensured         bool
	read            bool
	acked           []string
	messages        []strategybus.DecisionMessage
	pendingMessages []strategybus.DecisionMessage
	deadLetters     []strategybus.DecisionMessage
}

func (r *fakeDecisionReader) EnsureConsumerGroup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.ensured = true
	return nil
}

func (r *fakeDecisionReader) ReadDecisions(ctx context.Context) ([]strategybus.DecisionMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.read = true
	return r.messages, nil
}

func (r *fakeDecisionReader) ClaimPending(ctx context.Context) ([]strategybus.DecisionMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.pendingMessages, nil
}

func (r *fakeDecisionReader) DeadLetter(ctx context.Context, message strategybus.DecisionMessage, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.deadLetters = append(r.deadLetters, message)
	return nil
}

func (r *fakeDecisionReader) Ack(ctx context.Context, ids ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.acked = append(r.acked, ids...)
	return nil
}

type fakeDecisionProcessor struct {
	messages []strategybus.DecisionMessage
}

func (p *fakeDecisionProcessor) ProcessDecision(ctx context.Context, message strategybus.DecisionMessage) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.messages = append(p.messages, message)
	return true, nil
}

type failingDecisionProcessor struct{}

func (p failingDecisionProcessor) ProcessDecision(ctx context.Context, message strategybus.DecisionMessage) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, os.ErrInvalid
}

type fakePriceReader struct {
	price strategy.PriceView
	err   error
}

func (r fakePriceReader) ReadPrice(ctx context.Context, target strategy.Target) (strategy.PriceView, error) {
	if err := ctx.Err(); err != nil {
		return strategy.PriceView{}, err
	}
	if r.err != nil {
		return strategy.PriceView{}, r.err
	}
	return r.price, nil
}
