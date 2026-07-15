package app

import (
	"alphaflow/go-service/execution-engine/internal/config"
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/executionadapter"
	"alphaflow/go-service/pkg/strategy"
	"context"
	"testing"
)

type fanoutAdapter struct {
	executionadapter.Adapter
	cap       execution.SymbolCapability
	positions []strategy.Position
}

func (a fanoutAdapter) Capability(context.Context, string) (execution.SymbolCapability, error) {
	return a.cap, nil
}
func (a fanoutAdapter) Positions(context.Context) ([]strategy.Position, error) {
	return a.positions, nil
}

type capturePublisher struct{ intents []execution.OrderIntent }

func (p *capturePublisher) PublishIntent(_ context.Context, i execution.OrderIntent) error {
	p.intents = append(p.intents, i)
	return nil
}
func TestAccountFanoutUsesIndependentSizingAndExchange(t *testing.T) {
	publisher := &capturePublisher{}
	template := execution.OrderIntent{IntentID: "plan-1", StrategyName: "supertrend", Symbol: "BTCUSDT", Action: execution.OrderActionOpen, PositionSide: "long", ReferencePrice: "50000"}
	cap := execution.SymbolCapability{Exchange: "binance", Market: "um", Symbol: "BTCUSDT", QtyStep: "0.001", MinQty: "0.001", ContractSize: "1", QuantityUnit: "base"}
	runtimes := []accountRuntime{{adapter: fanoutAdapter{cap: cap}, account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: "live", Market: "um"}, symbols: []string{"BTCUSDT"}, config: config.Account{Strategies: []string{"supertrend"}, MarginQuote: 100, Leverage: 10}}, {adapter: fanoutAdapter{cap: cap}, account: executionaccount.Account{ID: "b", Exchange: "bitget", Environment: "testnet", Market: "um"}, symbols: []string{"BTCUSDT"}, config: config.Account{Strategies: []string{"supertrend"}, MarginQuote: 50, Leverage: 10}}}
	if err := newAccountFanout(runtimes, publisher).Publish(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	if len(publisher.intents) != 2 || publisher.intents[0].Quantity != 0.02 || publisher.intents[1].Quantity != 0.01 || publisher.intents[0].Account == publisher.intents[1].Account {
		t.Fatalf("intents=%#v", publisher.intents)
	}
}
func TestAccountFanoutCloseUsesAccountPosition(t *testing.T) {
	publisher := &capturePublisher{}
	adapter := fanoutAdapter{positions: []strategy.Position{{Symbol: "BTCUSDT", PositionSide: strategy.ExchangePositionSideLong, Size: 3}}}
	runtime := accountRuntime{adapter: adapter, account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: "live", Market: "um"}, config: config.Account{Strategies: []string{"*"}, MarginQuote: 1, Leverage: 1}}
	template := execution.OrderIntent{IntentID: "p", StrategyName: "s", Symbol: "BTCUSDT", Action: execution.OrderActionClose, PositionSide: "long"}
	if err := newAccountFanout([]accountRuntime{runtime}, publisher).Publish(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	if len(publisher.intents) != 1 || publisher.intents[0].Quantity != 3 {
		t.Fatalf("intents=%#v", publisher.intents)
	}
}
func TestAccountFanoutFailureDoesNotBlockOtherAccount(t *testing.T) {
	publisher := &capturePublisher{}
	bad := accountRuntime{adapter: fanoutAdapter{}, account: executionaccount.Account{ID: "bad", Exchange: "binance", Environment: "live", Market: "um"}, config: config.Account{Strategies: []string{"*"}, MarginQuote: 1, Leverage: 1}}
	good := accountRuntime{adapter: fanoutAdapter{cap: execution.SymbolCapability{Exchange: "bitget", Market: "um", Symbol: "BTCUSDT", QtyStep: "0.001", MinQty: "0.001", ContractSize: "1", QuantityUnit: "base"}}, account: executionaccount.Account{ID: "good", Exchange: "bitget", Environment: "live", Market: "um"}, config: config.Account{Strategies: []string{"*"}, MarginQuote: 100, Leverage: 1}}
	template := execution.OrderIntent{IntentID: "p", StrategyName: "s", Symbol: "BTCUSDT", Action: execution.OrderActionOpen, PositionSide: "long", ReferencePrice: "100"}
	if err := newAccountFanout([]accountRuntime{bad, good}, publisher).Publish(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	if len(publisher.intents) != 1 || publisher.intents[0].Account != "good" {
		t.Fatalf("intents=%#v", publisher.intents)
	}
}
func TestAccountFanoutClosesOppositePositionFirst(t *testing.T) {
	publisher := &capturePublisher{}
	runtime := accountRuntime{adapter: fanoutAdapter{positions: []strategy.Position{{Symbol: "BTCUSDT", PositionSide: strategy.ExchangePositionSideShort, Size: 2}}}, account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: "live", Market: "um"}, config: config.Account{Strategies: []string{"*"}, MarginQuote: 100, Leverage: 1}}
	template := execution.OrderIntent{IntentID: "p", StrategyName: "s", Symbol: "BTCUSDT", Action: execution.OrderActionOpen, PositionSide: "long", Side: execution.OrderSideBuy, ReferencePrice: "100"}
	if err := newAccountFanout([]accountRuntime{runtime}, publisher).Publish(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	if len(publisher.intents) != 1 || publisher.intents[0].Action != execution.OrderActionClose || publisher.intents[0].Quantity != 2 || !publisher.intents[0].ReduceOnly {
		t.Fatalf("intents=%#v", publisher.intents)
	}
}

func TestAccountFanoutPrepareUsesRealPositionForPartialExit(t *testing.T) {
	runtime := accountRuntime{
		adapter: fanoutAdapter{positions: []strategy.Position{{Symbol: "BTCUSDT", PositionSide: strategy.ExchangePositionSideLong, Size: 4}}},
		account: executionaccount.Account{ID: "a", Exchange: "binance", Environment: "live", Market: "um"},
		config:  config.Account{Strategies: []string{"supertrend"}, Symbols: []string{"BTCUSDT"}, MarginQuote: 100, Leverage: 1},
	}
	intent := execution.OrderIntent{
		IntentID:      "risk-exit",
		StrategyName:  "supertrend",
		Exchange:      "binance",
		Account:       "a",
		Symbol:        "BTCUSDT",
		Action:        execution.OrderActionReduce,
		PositionSide:  "long",
		TriggeredRule: &strategy.ExitRule{SizePct: 0.25},
	}

	prepared, ok, err := newAccountFanout([]accountRuntime{runtime}, &capturePublisher{}).Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || prepared.Quantity != 1 || prepared.Account != "a" {
		t.Fatalf("prepared=%#v ok=%v", prepared, ok)
	}
}
