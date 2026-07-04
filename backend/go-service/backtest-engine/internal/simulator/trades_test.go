package simulator

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildBacktestTradesPairsEntryAndExit(t *testing.T) {
	trades, err := BuildBacktestTrades([]strategy.StrategyEvent{
		tradeEvent("entry-1", strategy.EventTypeOrderFilled, "100", 1, nil),
		tradeEvent("exit-1", strategy.EventTypeOrderFilled, "110", 1, map[string]string{
			"exit_reason":          string(strategy.ExitReasonStrategy),
			"return_pct":           "9.4",
			"return_on_margin_pct": "9.4",
			"gross_pnl":            "10",
		}),
	})
	if err != nil {
		t.Fatalf("BuildBacktestTrades() error = %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("trades len = %d, want 1", len(trades))
	}
	trade := trades[0]
	if trade.EntryEventID != "entry-1" || trade.ExitEventID != "exit-1" {
		t.Fatalf("trade event ids = %s/%s, want entry-1/exit-1", trade.EntryEventID, trade.ExitEventID)
	}
	if trade.EntryPrice != "100" || trade.ExitPrice != "110" {
		t.Fatalf("trade prices = %s/%s, want 100/110", trade.EntryPrice, trade.ExitPrice)
	}
	if trade.ExitReason != string(strategy.ExitReasonStrategy) {
		t.Fatalf("exit reason = %q, want strategy", trade.ExitReason)
	}
	if trade.ReturnPct != "9.4" || trade.Metadata["gross_pnl"] != "10" {
		t.Fatalf("trade metrics = %#v metadata=%#v", trade, trade.Metadata)
	}
}

func TestBuildBacktestTradesRejectsUnmatchedExit(t *testing.T) {
	_, err := BuildBacktestTrades([]strategy.StrategyEvent{
		tradeEvent("exit-1", strategy.EventTypeOrderFilled, "110", 1, map[string]string{
			"exit_reason": string(strategy.ExitReasonStrategy),
		}),
	})
	if err == nil {
		t.Fatal("BuildBacktestTrades() error = nil, want unmatched exit error")
	}
}

func TestBuildBacktestTradesDoesNotCrossSymbols(t *testing.T) {
	entry := tradeEvent("entry-1", strategy.EventTypeOrderFilled, "100", 1, nil)
	entry.Symbol = "BTCUSDT"
	exit := tradeEvent("exit-1", strategy.EventTypeOrderFilled, "110", 1, map[string]string{
		"exit_reason": string(strategy.ExitReasonStrategy),
	})
	exit.Symbol = "ETHUSDT"

	_, err := BuildBacktestTrades([]strategy.StrategyEvent{entry, exit})
	if err == nil {
		t.Fatal("BuildBacktestTrades() error = nil, want unmatched exit across symbols")
	}
}

func tradeEvent(
	eventID string,
	eventType strategy.EventType,
	price string,
	size float64,
	metadata map[string]string,
) strategy.StrategyEvent {
	return strategy.StrategyEvent{
		EventID:         eventID,
		Scope:           strategy.PositionScopeBacktest,
		RunID:           "run-1",
		Exchange:        "binance",
		Market:          "um",
		Symbol:          "ETHUSDT",
		StrategyName:    "supertrend",
		EventType:       eventType,
		EventTime:       2000,
		BarOpenTime:     1000,
		PositionSide:    strategy.ExchangePositionSideLong,
		Size:            size,
		Price:           price,
		Fee:             "0.1",
		PnL:             "9.4",
		Reason:          "trend",
		ExchangeOrderID: "order-" + eventID,
		Metadata:        metadata,
		CreatedAt:       2000,
	}
}
