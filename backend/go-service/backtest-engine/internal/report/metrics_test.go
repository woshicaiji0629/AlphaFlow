package report

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestBuildTradeMetricsCalculatesEquityAndDrawdown(t *testing.T) {
	metrics, err := BuildTradeMetrics([]strategy.BacktestTrade{
		reportTrade("t3", 3000, "7"),
		reportTrade("t1", 1000, "10"),
		reportTrade("t2", 2000, "-4"),
		reportTrade("t4", 4000, "-3"),
	})
	if err != nil {
		t.Fatalf("BuildTradeMetrics() error = %v", err)
	}
	if metrics.TotalTrades != 4 {
		t.Fatalf("total trades = %d, want 4", metrics.TotalTrades)
	}
	if metrics.WinningTrades != 2 || metrics.LosingTrades != 2 {
		t.Fatalf("win/loss trades = %d/%d, want 2/2", metrics.WinningTrades, metrics.LosingTrades)
	}
	if metrics.WinRate != 0.5 {
		t.Fatalf("win rate = %f, want 0.5", metrics.WinRate)
	}
	if metrics.NetPnL != 10 {
		t.Fatalf("net pnl = %f, want 10", metrics.NetPnL)
	}
	if metrics.GrossProfit != 17 || metrics.GrossLoss != 7 {
		t.Fatalf("gross profit/loss = %f/%f, want 17/7", metrics.GrossProfit, metrics.GrossLoss)
	}
	if metrics.ProfitFactor != 17.0/7.0 {
		t.Fatalf("profit factor = %f, want %f", metrics.ProfitFactor, 17.0/7.0)
	}
	if metrics.MaxDrawdown != 4 {
		t.Fatalf("max drawdown = %f, want 4", metrics.MaxDrawdown)
	}
	if metrics.MaxConsecutiveLosses != 1 {
		t.Fatalf("max consecutive losses = %d, want 1", metrics.MaxConsecutiveLosses)
	}
	if len(metrics.EquityCurve) != 4 || metrics.EquityCurve[0].TradeID != "t1" || metrics.EquityCurve[3].Equity != 10 {
		t.Fatalf("equity curve = %#v, want sorted cumulative points", metrics.EquityCurve)
	}
}

func TestBuildTradeMetricsHandlesNoLosingTrades(t *testing.T) {
	metrics, err := BuildTradeMetrics([]strategy.BacktestTrade{
		reportTrade("t1", 1000, "10"),
	})
	if err != nil {
		t.Fatalf("BuildTradeMetrics() error = %v", err)
	}
	if !math.IsInf(metrics.ProfitFactor, 1) {
		t.Fatalf("profit factor = %f, want +Inf", metrics.ProfitFactor)
	}
}

func TestBuildTradeMetricsRejectsInvalidPnL(t *testing.T) {
	_, err := BuildTradeMetrics([]strategy.BacktestTrade{
		reportTrade("t1", 1000, "bad"),
	})
	if err == nil {
		t.Fatal("BuildTradeMetrics() error = nil, want parse error")
	}
}

func TestFormatRunSummaryIncludesKeyFields(t *testing.T) {
	item, err := BuildBacktestReport(strategy.BacktestRunSummary{
		RunID:        "run-1",
		StrategySet:  "supertrend",
		Symbols:      []string{"ETHUSDT", "BTCUSDT"},
		TotalTrades:  3,
		WinRate:      0.6666666667,
		NetPnL:       "12.5",
		MaxDrawdown:  "4.2",
		ProfitFactor: 2.5,
		Metadata: map[string]string{
			"max_consecutive_losses": "1",
			"final_equity":           "1012.5",
			"account_return_pct":     "1.25",
			"total_fee":              "2.4",
			"total_rebate":           "0.6",
			"liquidated":             "false",
		},
	}, RunStats{
		Contexts:      10,
		Decisions:     9,
		Results:       8,
		Events:        7,
		OrderFills:    6,
		OpenPositions: 1,
	}, []strategy.BacktestTrade{
		reportTrade("t1", 1000, "10"),
		reportTrade("t2", 2000, "-4.2"),
	})
	if err != nil {
		t.Fatalf("BuildBacktestReport() error = %v", err)
	}
	text := FormatBacktestReport(item)

	for _, want := range []string{
		"Backtest report",
		"run_id: run-1",
		"strategy_set: supertrend",
		"symbols: ETHUSDT,BTCUSDT",
		"contexts: 10",
		"total_trades: 3",
		"max_drawdown: 4.2",
		"max_consecutive_losses: 1",
		"final_equity: 1012.5",
		"account_return_pct: 1.25",
		"total_fee: 2.4",
		"total_rebate: 0.6",
		"liquidated: false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary text = %q, want to contain %q", text, want)
		}
	}
}

func TestBuildBacktestReportFillsMissingSummaryFields(t *testing.T) {
	item, err := BuildBacktestReport(strategy.BacktestRunSummary{
		RunID: "run-1",
	}, RunStats{}, []strategy.BacktestTrade{
		reportTrade("t1", 1000, "10"),
		reportTrade("t2", 2000, "-4"),
	})
	if err != nil {
		t.Fatalf("BuildBacktestReport() error = %v", err)
	}
	if item.Summary.MaxDrawdown != "4" {
		t.Fatalf("max drawdown = %q, want 4", item.Summary.MaxDrawdown)
	}
	if item.Summary.Metadata["gross_profit"] != "10" || item.Summary.Metadata["gross_loss"] != "4" {
		t.Fatalf("metadata = %#v, want gross metrics", item.Summary.Metadata)
	}
	if item.Metrics.NetPnL != 6 {
		t.Fatalf("metrics net pnl = %f, want 6", item.Metrics.NetPnL)
	}
}

func TestMarshalBacktestReportUsesStableJSONKeys(t *testing.T) {
	item, err := BuildBacktestReportWithInitialEquity(strategy.BacktestRunSummary{
		RunID:       "run-1",
		StrategySet: "supertrend",
		Symbols:     []string{"ETHUSDT"},
	}, RunStats{
		Contexts:      1,
		OrderFills:    2,
		OpenPositions: 3,
	}, []strategy.BacktestTrade{
		reportTrade("t1", 1000, "10"),
	}, 1000, []BarEquityPoint{
		{Time: 1000, Symbol: "ETHUSDT", Price: 100, RealizedPnL: 0, UnrealizedPnL: 10, Equity: 10},
	})
	if err != nil {
		t.Fatalf("BuildBacktestReport() error = %v", err)
	}
	payload, err := MarshalBacktestReport(item)
	if err != nil {
		t.Fatalf("MarshalBacktestReport() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v payload=%s", err, payload)
	}
	if decoded["summary"] == nil || decoded["stats"] == nil || decoded["metrics"] == nil ||
		decoded["bar_equity_curve"] == nil || decoded["portfolio_equity_curve"] == nil ||
		decoded["account_equity_curve"] == nil {
		t.Fatalf("json keys = %#v, want summary/stats/metrics/equity curves", decoded)
	}
	summary := decoded["summary"].(map[string]any)
	if summary["run_id"] != "run-1" || summary["strategy_set"] != "supertrend" {
		t.Fatalf("summary = %#v, want snake_case summary fields", summary)
	}
	if _, ok := summary["RunID"]; ok {
		t.Fatalf("summary = %#v, should not include Go field name RunID", summary)
	}
	stats := decoded["stats"].(map[string]any)
	if stats["order_fills"] != float64(2) || stats["open_positions"] != float64(3) {
		t.Fatalf("stats = %#v, want snake_case run stats", stats)
	}
	metrics := decoded["metrics"].(map[string]any)
	if metrics["equity_curve"] == nil || metrics["max_drawdown"] == nil {
		t.Fatalf("metrics = %#v, want equity_curve and max_drawdown", metrics)
	}
	barEquityCurve := decoded["bar_equity_curve"].([]any)
	if len(barEquityCurve) != 1 {
		t.Fatalf("bar equity curve len = %d, want 1", len(barEquityCurve))
	}
	barPoint := barEquityCurve[0].(map[string]any)
	if barPoint["unrealized_pnl"] != float64(10) || barPoint["equity"] != float64(10) {
		t.Fatalf("bar equity point = %#v, want unrealized/equity 10", barPoint)
	}
	portfolioEquityCurve := decoded["portfolio_equity_curve"].([]any)
	if len(portfolioEquityCurve) != 1 {
		t.Fatalf("portfolio equity curve len = %d, want 1", len(portfolioEquityCurve))
	}
	portfolioPoint := portfolioEquityCurve[0].(map[string]any)
	if portfolioPoint["equity"] != float64(10) {
		t.Fatalf("portfolio equity point = %#v, want equity 10", portfolioPoint)
	}
}

func TestWriteBacktestReportMatchesMarshal(t *testing.T) {
	pointCount := backtestReportCurveChunkSize + 1
	want, err := MarshalBacktestReport(streamingBacktestReport(pointCount))
	if err != nil {
		t.Fatalf("MarshalBacktestReport() error = %v", err)
	}
	var output strings.Builder
	if err := WriteBacktestReport(&output, streamingBacktestReport(pointCount)); err != nil {
		t.Fatalf("WriteBacktestReport() error = %v", err)
	}
	if got := output.String(); got != string(want) {
		t.Fatalf("streamed report differs from marshal: got len=%d want len=%d", len(got), len(want))
	}
}

func TestWriteBacktestReportPreservesNilAndEmptyCurves(t *testing.T) {
	item := BacktestReport{
		BarEquityCurve:       nil,
		PortfolioEquityCurve: []PortfolioEquityPoint{},
		AccountEquityCurve:   nil,
	}
	want, err := MarshalBacktestReport(item)
	if err != nil {
		t.Fatalf("MarshalBacktestReport() error = %v", err)
	}
	var output strings.Builder
	if err := WriteBacktestReport(&output, item); err != nil {
		t.Fatalf("WriteBacktestReport() error = %v", err)
	}
	if got := output.String(); got != string(want) {
		t.Fatalf("streamed report = %q, want %q", got, want)
	}
}

func TestWriteBacktestReportReturnsWriterError(t *testing.T) {
	writeErr := errors.New("write failed")
	err := WriteBacktestReport(failingWriter{err: writeErr}, streamingBacktestReport(1))
	if !errors.Is(err, writeErr) {
		t.Fatalf("WriteBacktestReport() error = %v, want %v", err, writeErr)
	}
}

func BenchmarkBacktestReportJSON(b *testing.B) {
	item := streamingBacktestReport(backtestReportCurveChunkSize + 1)
	b.Run("marshal", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			payload, err := MarshalBacktestReport(item)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkBacktestReportPayload = payload
		}
	})
	b.Run("stream", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			if err := WriteBacktestReport(io.Discard, item); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestBuildAccountEquityCurveAddsInitialEquity(t *testing.T) {
	curve := BuildAccountEquityCurve(1000, []PortfolioEquityPoint{
		{Time: 1000, RealizedPnL: 5, UnrealizedPnL: 10, Equity: 15},
		{Time: 2000, RealizedPnL: 5, UnrealizedPnL: -5, Equity: 0},
	})
	if len(curve) != 2 {
		t.Fatalf("account equity curve len = %d, want 2", len(curve))
	}
	if curve[0].InitialEquity != 1000 || curve[0].Equity != 1015 || curve[0].ReturnPct != 1.5 {
		t.Fatalf("curve[0] = %#v, want equity 1015 return 1.5%%", curve[0])
	}
	if curve[1].Equity != 1000 || curve[1].ReturnPct != 0 {
		t.Fatalf("curve[1] = %#v, want equity 1000 return 0", curve[1])
	}
}

func TestBuildBacktestReportWithInitialEquityAddsAccountCurve(t *testing.T) {
	item, err := BuildBacktestReportWithInitialEquity(strategy.BacktestRunSummary{
		RunID: "run-1",
	}, RunStats{}, []strategy.BacktestTrade{
		reportTrade("t1", 1000, "10"),
	}, 1000, []BarEquityPoint{
		{Time: 1000, Symbol: "ETHUSDT", RealizedPnL: 0, UnrealizedPnL: 10, Equity: 10},
	})
	if err != nil {
		t.Fatalf("BuildBacktestReportWithInitialEquity() error = %v", err)
	}
	if len(item.AccountEquityCurve) != 1 {
		t.Fatalf("account equity curve len = %d, want 1", len(item.AccountEquityCurve))
	}
	if item.AccountEquityCurve[0].Equity != 1010 {
		t.Fatalf("account equity = %f, want 1010", item.AccountEquityCurve[0].Equity)
	}
}

func TestBuildPortfolioEquityCurveAggregatesLatestSymbolPnL(t *testing.T) {
	curve := BuildPortfolioEquityCurve([]BarEquityPoint{
		{Time: 2000, Symbol: "ETHUSDT", RealizedPnL: 3, UnrealizedPnL: 4},
		{Time: 1000, Symbol: "ETHUSDT", RealizedPnL: 0, UnrealizedPnL: 1},
		{Time: 1000, Symbol: "BTCUSDT", RealizedPnL: 0, UnrealizedPnL: 2},
		{Time: 3000, Symbol: "BTCUSDT", RealizedPnL: 5, UnrealizedPnL: -1},
	})
	if len(curve) != 3 {
		t.Fatalf("portfolio curve len = %d, want 3", len(curve))
	}
	if curve[0].Time != 1000 || curve[0].UnrealizedPnL != 3 || curve[0].Equity != 3 {
		t.Fatalf("curve[0] = %#v, want time 1000 unrealized/equity 3", curve[0])
	}
	if curve[1].Time != 2000 || curve[1].RealizedPnL != 3 || curve[1].UnrealizedPnL != 6 || curve[1].Equity != 9 {
		t.Fatalf("curve[1] = %#v, want realized 3 unrealized 6 equity 9", curve[1])
	}
	if curve[2].Time != 3000 || curve[2].RealizedPnL != 5 || curve[2].UnrealizedPnL != 3 || curve[2].Equity != 8 {
		t.Fatalf("curve[2] = %#v, want realized 5 unrealized 3 equity 8", curve[2])
	}
}

func TestToBacktestSummaryDTOCopiesMutableFields(t *testing.T) {
	summary := strategy.BacktestRunSummary{
		Symbols:  []string{"ETHUSDT"},
		Metadata: map[string]string{"key": "value"},
	}
	dto := ToBacktestSummaryDTO(summary)
	summary.Symbols[0] = "BTCUSDT"
	summary.Metadata["key"] = "changed"

	if dto.Symbols[0] != "ETHUSDT" {
		t.Fatalf("dto symbols = %v, want copied symbols", dto.Symbols)
	}
	if dto.Metadata["key"] != "value" {
		t.Fatalf("dto metadata = %#v, want copied metadata", dto.Metadata)
	}
}

func reportTrade(id string, exitTime int64, pnl string) strategy.BacktestTrade {
	return strategy.BacktestTrade{
		TradeID:  id,
		ExitTime: exitTime,
		PnL:      pnl,
	}
}

var benchmarkBacktestReportPayload []byte

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func streamingBacktestReport(pointCount int) BacktestReport {
	item := BacktestReport{
		Summary: strategy.BacktestRunSummary{
			RunID:        "run-streaming",
			StrategySet:  "supertrend",
			Symbols:      []string{"ETHUSDT"},
			WinRate:      math.NaN(),
			ProfitFactor: math.Inf(1),
			Metadata:     map[string]string{"source": "test"},
		},
		Stats: RunStats{Contexts: pointCount, Events: pointCount},
		Metrics: TradeMetrics{
			NetPnL:       math.NaN(),
			ProfitFactor: math.Inf(1),
			EquityCurve:  []EquityPoint{{TradeID: "trade-1", Time: 1, Equity: math.Inf(-1)}},
		},
		BarEquityCurve:       make([]BarEquityPoint, pointCount),
		PortfolioEquityCurve: make([]PortfolioEquityPoint, pointCount),
		AccountEquityCurve:   make([]AccountEquityPoint, pointCount),
	}
	for index := 0; index < pointCount; index++ {
		timestamp := int64(index + 1)
		item.BarEquityCurve[index] = BarEquityPoint{
			Time: timestamp, Symbol: "ETHUSDT", Price: float64(index), Equity: float64(index),
		}
		item.PortfolioEquityCurve[index] = PortfolioEquityPoint{
			Time: timestamp, Equity: float64(index),
		}
		item.AccountEquityCurve[index] = AccountEquityPoint{
			Time: timestamp, InitialEquity: 1000, Balance: 1000, Equity: 1000 + float64(index),
		}
	}
	if pointCount > 0 {
		item.BarEquityCurve[pointCount-1].Price = math.NaN()
		item.PortfolioEquityCurve[pointCount-1].Equity = math.Inf(1)
		item.AccountEquityCurve[pointCount-1].ReturnPct = math.Inf(-1)
	}
	return item
}
