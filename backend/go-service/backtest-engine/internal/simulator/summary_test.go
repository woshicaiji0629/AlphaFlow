package simulator

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildBacktestRunSummaryCountsOnlyExitFillsAsTrades(t *testing.T) {
	summary := BuildBacktestRunSummary([]strategy.StrategyEvent{
		{
			EventType: strategy.EventTypeOrderFilled,
			PnL:       "-0.06",
		},
		{
			EventType: strategy.EventTypeOrderFilled,
			PnL:       "9.4",
			Metadata: map[string]string{
				"exit_reason": string(strategy.ExitReasonStrategy),
			},
		},
		{
			EventType: strategy.EventTypeOrderFilled,
			PnL:       "-4.6",
			Metadata: map[string]string{
				"exit_reason": string(strategy.ExitReasonStopLoss),
			},
		},
	}, SummaryOptions{
		RunID:       "run-1",
		StrategySet: "supertrend",
		Exchange:    "binance",
		Market:      "um",
		Symbols:     []string{"ETHUSDT"},
		StartTime:   1000,
		EndTime:     2000,
	})

	if summary.TotalTrades != 2 {
		t.Fatalf("total trades = %d, want 2", summary.TotalTrades)
	}
	if summary.WinRate != 0.5 {
		t.Fatalf("win rate = %f, want 0.5", summary.WinRate)
	}
	if summary.NetPnL != "4.74" {
		t.Fatalf("net pnl = %q, want 4.74", summary.NetPnL)
	}
	if summary.ProfitFactor != 9.4/4.6 {
		t.Fatalf("profit factor = %f, want %f", summary.ProfitFactor, 9.4/4.6)
	}
	if summary.Status != strategy.BacktestRunStatusCompleted {
		t.Fatalf("status = %q, want completed", summary.Status)
	}
	if len(summary.Symbols) != 1 || summary.Symbols[0] != "ETHUSDT" {
		t.Fatalf("symbols = %v, want [ETHUSDT]", summary.Symbols)
	}
}

func TestBuildBacktestRunSummaryHandlesNoExitTrades(t *testing.T) {
	summary := BuildBacktestRunSummary([]strategy.StrategyEvent{{
		EventType: strategy.EventTypeOrderFilled,
		PnL:       "-0.06",
	}}, SummaryOptions{})

	if summary.TotalTrades != 0 {
		t.Fatalf("total trades = %d, want 0", summary.TotalTrades)
	}
	if summary.WinRate != 0 {
		t.Fatalf("win rate = %f, want 0", summary.WinRate)
	}
	if summary.ProfitFactor != 0 {
		t.Fatalf("profit factor = %f, want 0", summary.ProfitFactor)
	}
	if summary.NetPnL != "-0.06" {
		t.Fatalf("net pnl = %q, want -0.06", summary.NetPnL)
	}
}
