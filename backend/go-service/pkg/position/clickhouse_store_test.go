package position

import (
	"strings"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildStrategyEventsInsert(t *testing.T) {
	events := []strategy.StrategyEvent{
		{
			EventID:      "event-1",
			Scope:        strategy.PositionScopeBacktest,
			RunID:        "run-1",
			Exchange:     "binance",
			Market:       "um",
			Symbol:       "ETHUSDT",
			StrategyName: "supertrend",
			EventType:    strategy.EventTypeSignalGenerated,
			EventTime:    1000,
			BarOpenTime:  900,
			Side:         strategy.SignalSideBuy,
			PositionSide: strategy.ExchangePositionSideLong,
			PositionMode: strategy.ExchangePositionModeNet,
			Size:         1,
			Price:        "100",
			Score:        0.8,
			Confidence:   0.7,
			Metadata: map[string]string{
				"source": "test",
			},
			CreatedAt: 1001,
		},
		{
			EventID:      "event-2",
			Scope:        strategy.PositionScopePaper,
			Exchange:     "binance",
			Market:       "um",
			Symbol:       "SOLUSDT",
			StrategyName: "keltner",
			EventType:    strategy.EventTypePositionOpened,
			EventTime:    2000,
			CreatedAt:    2001,
		},
	}

	query, args, err := buildStrategyEventsInsert(events)
	if err != nil {
		t.Fatalf("buildStrategyEventsInsert() error = %v", err)
	}
	if !strings.Contains(query, "INSERT INTO strategy_events") {
		t.Fatalf("query missing insert target: %s", query)
	}
	if got, want := len(args), len(events)*27; got != want {
		t.Fatalf("len(args) = %d, want %d", got, want)
	}
	if got := args[0]; got != "event-1" {
		t.Fatalf("args[0] = %v, want event-1", got)
	}
	if got := args[25]; got != `{"source":"test"}` {
		t.Fatalf("metadata arg = %v, want source json", got)
	}
}

func TestBuildStrategyEventsInsertEmpty(t *testing.T) {
	query, args, err := buildStrategyEventsInsert(nil)
	if err != nil {
		t.Fatalf("buildStrategyEventsInsert(nil) error = %v", err)
	}
	if query != "" {
		t.Fatalf("query = %q, want empty", query)
	}
	if args != nil {
		t.Fatalf("args = %+v, want nil", args)
	}
}

func TestBuildBacktestRunSummaryInsert(t *testing.T) {
	summary := strategy.BacktestRunSummary{
		RunID:        "run-1",
		Status:       strategy.BacktestRunStatusCompleted,
		StrategySet:  "set-1",
		Exchange:     "binance",
		Market:       "um",
		Symbols:      []string{"ETHUSDT", "SOLUSDT"},
		StartTime:    1000,
		EndTime:      2000,
		TotalTrades:  12,
		WinRate:      0.5,
		NetPnL:       "10",
		MaxDrawdown:  "2",
		ProfitFactor: 1.2,
		Sharpe:       0.8,
		Metadata: map[string]string{
			"run_kind": "bt",
		},
		CreatedAt: 1001,
		UpdatedAt: 2001,
	}

	query, args, err := buildBacktestRunSummaryInsert(summary)
	if err != nil {
		t.Fatalf("buildBacktestRunSummaryInsert() error = %v", err)
	}
	if !strings.Contains(query, "INSERT INTO backtest_run_summary") {
		t.Fatalf("query missing insert target: %s", query)
	}
	if got, want := len(args), 18; got != want {
		t.Fatalf("len(args) = %d, want %d", got, want)
	}
	if got := args[5]; got != `["ETHUSDT","SOLUSDT"]` {
		t.Fatalf("symbols arg = %v, want symbols json", got)
	}
	if got := args[15]; got != `{"run_kind":"bt"}` {
		t.Fatalf("metadata arg = %v, want metadata json", got)
	}
}

func TestClickHouseDSN(t *testing.T) {
	dsn := clickHouseDSN(ClickHouseOptions{
		Addr:        "127.0.0.1:9000",
		Database:    "alphaflow",
		Username:    "user",
		Password:    "pass",
		DialTimeout: 1,
		ReadTimeout: 2,
	}, "alphaflow")

	if !strings.HasPrefix(dsn, "clickhouse://user:pass@127.0.0.1:9000/alphaflow?") {
		t.Fatalf("dsn = %q", dsn)
	}
	if !strings.Contains(dsn, "dial_timeout=1ns") {
		t.Fatalf("dsn missing dial_timeout: %q", dsn)
	}
	if !strings.Contains(dsn, "read_timeout=2ns") {
		t.Fatalf("dsn missing read_timeout: %q", dsn)
	}
}
