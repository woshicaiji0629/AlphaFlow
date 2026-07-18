package simulator

import (
	"encoding/json"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildBacktestTradesPairsEntryAndExit(t *testing.T) {
	analysis, err := json.Marshal(strategy.Analysis{Checks: []strategy.DiagnosticCheck{
		{Name: "entry_mode", Side: strategy.SignalSideBuy, Values: map[string]string{
			"mode":                       "trend_continuation",
			"trigger_sources":            "wick_reclaim,ma_cross",
			"trigger_source_count":       "2",
			"ma_tangled":                 "false",
			"volatility_state":           "expanding",
			"local_supertrend_direction": "up",
		}},
		{Name: "higher_timeframe_regime", Side: strategy.SignalSideBuy, Values: map[string]string{"state": "trend", "10m": "aligned", "15m": "aligned", "30m": "neutral"}},
		{Name: "pullback_resolution", Side: strategy.SignalSideBuy, Values: map[string]string{"5m": "aligned"}},
		{Name: "entry_mode", Side: strategy.SignalSideSell, Values: map[string]string{"mode": "", "trigger_sources": ""}},
	}})
	if err != nil {
		t.Fatalf("marshal analysis: %v", err)
	}
	entry := tradeEvent("entry-1", strategy.EventTypeOrderFilled, "100", 1, map[string]string{"analysis": string(analysis)})
	entry.Side = strategy.SignalSideBuy
	entry.Score = 0.85
	entry.Confidence = 0.85
	trades, err := BuildBacktestTrades([]strategy.StrategyEvent{
		entry,
		tradeEvent("exit-1", strategy.EventTypeOrderFilled, "110", 1, map[string]string{
			"exit_reason":          string(strategy.ExitReasonStrategy),
			"return_pct":           "9.4",
			"return_on_margin_pct": "9.4",
			"gross_pnl":            "10",
			"mfe_bps":              "150",
			"mae_bps":              "30",
			"profit_giveback_bps":  "50",
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
	for key, want := range map[string]string{
		"entry_mode":                 "trend_continuation",
		"trigger_source":             "wick_reclaim,ma_cross",
		"trigger_source_count":       "2",
		"entry_regime_state":         "trend",
		"entry_5m_state":             "aligned",
		"entry_10m_state":            "aligned",
		"entry_volatility_state":     "expanding",
		"entry_supertrend_direction": "up",
		"mfe_bps":                    "150",
		"mae_bps":                    "30",
		"profit_giveback_bps":        "50",
	} {
		if trade.Metadata[key] != want {
			t.Fatalf("%s = %q, want %q metadata=%#v", key, trade.Metadata[key], want, trade.Metadata)
		}
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
