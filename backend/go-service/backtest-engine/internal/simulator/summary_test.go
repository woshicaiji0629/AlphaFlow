package simulator

import (
	"testing"

	"alphaflow/go-service/backtest-engine/internal/report"
	"alphaflow/go-service/pkg/strategy"
)

func TestBuildBacktestRunSummaryCountsOnlyExitFillsAsTrades(t *testing.T) {
	entryOne := tradeEvent("entry-1", strategy.EventTypeOrderFilled, "100", 1, nil)
	entryOne.PnL = "-0.06"
	entryTwo := tradeEvent("entry-2", strategy.EventTypeOrderFilled, "100", 1, nil)
	entryTwo.PnL = "0"
	exitOne := tradeEvent("exit-1", strategy.EventTypeOrderFilled, "110", 1, map[string]string{
		"exit_reason": string(strategy.ExitReasonStrategy),
	})
	exitOne.PnL = "9.4"
	exitTwo := tradeEvent("exit-2", strategy.EventTypeOrderFilled, "95", 1, map[string]string{
		"exit_reason": string(strategy.ExitReasonStopLoss),
	})
	exitTwo.PnL = "-4.6"

	summary := BuildBacktestRunSummary([]strategy.StrategyEvent{
		entryOne,
		exitOne,
		entryTwo,
		exitTwo,
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
	if summary.MaxDrawdown != "4.6" {
		t.Fatalf("max drawdown = %q, want 4.6", summary.MaxDrawdown)
	}
	if summary.Metadata["gross_profit"] != "9.4" || summary.Metadata["gross_loss"] != "4.6" {
		t.Fatalf("summary metadata = %#v, want gross report metrics", summary.Metadata)
	}
	if summary.Metadata["max_consecutive_losses"] != "1" {
		t.Fatalf("max consecutive losses = %q, want 1", summary.Metadata["max_consecutive_losses"])
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

func TestBuildBacktestRunSummaryUsesAccountCurveForFinalNetPnL(t *testing.T) {
	summary := BuildBacktestRunSummary([]strategy.StrategyEvent{{
		EventType: strategy.EventTypeOrderFilled,
		PnL:       "10",
	}}, SummaryOptions{
		AccountCurve: []report.AccountEquityPoint{
			{
				Time:             1000,
				InitialEquity:    1000,
				Balance:          1005,
				AvailableBalance: 1005,
				Equity:           1005,
				ReturnPct:        0.5,
				Fee:              1,
				Rebate:           0.2,
			},
			{
				Time:             2000,
				InitialEquity:    1000,
				Balance:          990,
				AvailableBalance: 990,
				Equity:           990,
				ReturnPct:        -1,
				Fee:              2,
				Rebate:           0.5,
				Liquidated:       true,
				StoppedReason:    "liquidated",
			},
		},
	})

	if summary.NetPnL != "-10" {
		t.Fatalf("net pnl = %q, want account net pnl -10", summary.NetPnL)
	}
	if summary.MaxDrawdown != "15" {
		t.Fatalf("max drawdown = %q, want account drawdown 15", summary.MaxDrawdown)
	}
	if summary.Metadata["final_equity"] != "990" ||
		summary.Metadata["account_return_pct"] != "-1" ||
		summary.Metadata["total_fee"] != "2" ||
		summary.Metadata["total_rebate"] != "0.5" ||
		summary.Metadata["liquidated"] != "true" ||
		summary.Metadata["stopped_reason"] != "liquidated" {
		t.Fatalf("metadata = %#v, want account summary fields", summary.Metadata)
	}
}
