package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func TestRunLoadsHistoricalKlines(t *testing.T) {
	originalBuilder := buildMarketStore
	originalResultBuilder := buildResultStore
	t.Cleanup(func() {
		buildMarketStore = originalBuilder
		buildResultStore = originalResultBuilder
	})
	startTime := mustParseTime(t, "2026-01-01T00:15:00Z")
	endTime := mustParseTime(t, "2026-01-01T00:30:00Z")
	warmupBars := int64(5)
	store := &fakeMarketStore{
		klinesBySeries: map[reader.SeriesKey][]marketmodel.Kline{
			{Symbol: "ETHUSDT", Interval: "3m"}: appTestKlines("ETHUSDT", "3m", startTime.UnixMilli()-warmupBars*int64(3*time.Minute/time.Millisecond), endTime.UnixMilli(), int64(3*time.Minute/time.Millisecond)),
			{Symbol: "ETHUSDT", Interval: "5m"}: appTestKlines("ETHUSDT", "5m", startTime.UnixMilli()-warmupBars*int64(5*time.Minute/time.Millisecond), endTime.UnixMilli(), int64(5*time.Minute/time.Millisecond)),
			{Symbol: "BTCUSDT", Interval: "3m"}: appTestKlines("BTCUSDT", "3m", startTime.UnixMilli()-warmupBars*int64(3*time.Minute/time.Millisecond), endTime.UnixMilli(), int64(3*time.Minute/time.Millisecond)),
			{Symbol: "BTCUSDT", Interval: "5m"}: appTestKlines("BTCUSDT", "5m", startTime.UnixMilli()-warmupBars*int64(5*time.Minute/time.Millisecond), endTime.UnixMilli(), int64(5*time.Minute/time.Millisecond)),
		},
	}
	buildMarketStore = func(ctx context.Context, cfg config.Config) (marketStore, error) {
		return store, nil
	}
	persistedStore := &fakeResultStore{}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return persistedStore, nil
	}
	path := writeConfig(t, `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT", "BTCUSDT"]
interval = "3m"
confirm_intervals = ["5m"]
warmup_bars = 5
start_time = "2026-01-01T00:15:00Z"
end_time = "2026-01-01T00:30:00Z"

[clickhouse]
enabled = true

[logging]
output = "stdout"
`)

	if err := Run(context.Background(), path); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(store.requests) != 4 {
		t.Fatalf("requests len = %d, want 4", len(store.requests))
	}
	wantRequests := []reader.SeriesKey{
		{Symbol: "ETHUSDT", Interval: "3m"},
		{Symbol: "ETHUSDT", Interval: "5m"},
		{Symbol: "BTCUSDT", Interval: "3m"},
		{Symbol: "BTCUSDT", Interval: "5m"},
	}
	for index, want := range wantRequests {
		got := store.requests[index]
		if got.Symbol != want.Symbol || got.Interval != want.Interval {
			t.Fatalf("request[%d] = %s/%s, want %s/%s", index, got.Symbol, got.Interval, want.Symbol, want.Interval)
		}
		if got.Start == 0 || got.End == 0 {
			t.Fatalf("request[%d] time range not set: %#v", index, got)
		}
	}
	if !store.closed {
		t.Fatal("store closed = false, want true")
	}
	if !persistedStore.closed {
		t.Fatal("result store closed = false, want true")
	}
	if len(persistedStore.events) == 0 {
		t.Fatal("persisted events len = 0, want strategy events")
	}
	if persistedStore.summary.RunID != "run-1" {
		t.Fatalf("summary run id = %q, want run-1", persistedStore.summary.RunID)
	}
}

func TestRunReturnsResultStoreWriteError(t *testing.T) {
	originalBuilder := buildMarketStore
	originalResultBuilder := buildResultStore
	t.Cleanup(func() {
		buildMarketStore = originalBuilder
		buildResultStore = originalResultBuilder
	})
	startTime := mustParseTime(t, "2026-01-01T00:15:00Z")
	endTime := mustParseTime(t, "2026-01-01T00:30:00Z")
	warmupBars := int64(5)
	store := &fakeMarketStore{
		klinesBySeries: map[reader.SeriesKey][]marketmodel.Kline{
			{Symbol: "ETHUSDT", Interval: "3m"}: appTestKlines("ETHUSDT", "3m", startTime.UnixMilli()-warmupBars*int64(3*time.Minute/time.Millisecond), endTime.UnixMilli(), int64(3*time.Minute/time.Millisecond)),
		},
	}
	buildMarketStore = func(ctx context.Context, cfg config.Config) (marketStore, error) {
		return store, nil
	}
	writeErr := errors.New("write failed")
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return &fakeResultStore{appendErr: writeErr}, nil
	}
	path := writeConfig(t, `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
confirm_intervals = []
warmup_bars = 5
start_time = "2026-01-01T00:15:00Z"
end_time = "2026-01-01T00:30:00Z"

[clickhouse]
enabled = true

[logging]
output = "stdout"
`)

	err := Run(context.Background(), path)
	if !errors.Is(err, writeErr) {
		t.Fatalf("Run() error = %v, want %v", err, writeErr)
	}
}

func TestPersistBacktestResultsAppendsEventsInBatches(t *testing.T) {
	originalResultBuilder := buildResultStore
	t.Cleanup(func() { buildResultStore = originalResultBuilder })
	store := &fakeResultStore{}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return store, nil
	}
	batchSize := 2
	events := make([]strategy.StrategyEvent, batchSize+1)
	for index := range events {
		events[index] = strategy.StrategyEvent{EventID: string(rune('a' + index%26))}
	}

	err := persistBacktestResults(context.Background(), config.Config{
		Result: config.ResultConfig{
			EventBatchSize: batchSize,
			TradeBatchSize: 1000,
		},
	}, strategyExecutionSummary(events))
	if err != nil {
		t.Fatalf("persistBacktestResults() error = %v", err)
	}
	if len(store.appendBatchSizes) != 2 {
		t.Fatalf("append batches = %v, want 2 batches", store.appendBatchSizes)
	}
	if store.appendBatchSizes[0] != batchSize || store.appendBatchSizes[1] != 1 {
		t.Fatalf("append batch sizes = %v, want [%d 1]", store.appendBatchSizes, batchSize)
	}
	if store.summary.RunID != "run-1" {
		t.Fatalf("summary run id = %q, want run-1", store.summary.RunID)
	}
}

func TestPersistBacktestResultsWritesTradesBeforeSummary(t *testing.T) {
	originalResultBuilder := buildResultStore
	t.Cleanup(func() { buildResultStore = originalResultBuilder })
	store := &fakeResultStore{}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return store, nil
	}
	summary := strategyExecutionSummary([]strategy.StrategyEvent{{EventID: "event-1"}})
	summary.BacktestTrades = []strategy.BacktestTrade{{TradeID: "trade-1"}}

	err := persistBacktestResults(context.Background(), config.Config{
		Result: config.ResultConfig{
			EventBatchSize: 1000,
			TradeBatchSize: 1000,
		},
	}, summary)
	if err != nil {
		t.Fatalf("persistBacktestResults() error = %v", err)
	}
	if len(store.trades) != 1 || store.trades[0].TradeID != "trade-1" {
		t.Fatalf("trades = %#v, want trade-1", store.trades)
	}
	if store.writeOrder != "events,trades,summary" {
		t.Fatalf("write order = %q, want events,trades,summary", store.writeOrder)
	}
}

func TestPersistBacktestResultsSavesTradesInBatches(t *testing.T) {
	originalResultBuilder := buildResultStore
	t.Cleanup(func() { buildResultStore = originalResultBuilder })
	store := &fakeResultStore{}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return store, nil
	}
	batchSize := 2
	summary := strategyExecutionSummary([]strategy.StrategyEvent{{EventID: "event-1"}})
	summary.BacktestTrades = make([]strategy.BacktestTrade, batchSize+1)
	for index := range summary.BacktestTrades {
		summary.BacktestTrades[index] = strategy.BacktestTrade{TradeID: string(rune('a' + index%26))}
	}

	err := persistBacktestResults(context.Background(), config.Config{
		Result: config.ResultConfig{
			EventBatchSize: 1000,
			TradeBatchSize: batchSize,
		},
	}, summary)
	if err != nil {
		t.Fatalf("persistBacktestResults() error = %v", err)
	}
	if len(store.tradeBatchSizes) != 2 {
		t.Fatalf("trade batches = %v, want 2 batches", store.tradeBatchSizes)
	}
	if store.tradeBatchSizes[0] != batchSize || store.tradeBatchSizes[1] != 1 {
		t.Fatalf("trade batch sizes = %v, want [%d 1]", store.tradeBatchSizes, batchSize)
	}
	if store.summary.RunID != "run-1" {
		t.Fatalf("summary run id = %q, want run-1", store.summary.RunID)
	}
}

func TestPersistBacktestResultsStopsBeforeSummaryWhenTradeBatchFails(t *testing.T) {
	originalResultBuilder := buildResultStore
	t.Cleanup(func() { buildResultStore = originalResultBuilder })
	writeErr := errors.New("trade batch failed")
	store := &fakeResultStore{tradesErrAtCall: 2, tradesErr: writeErr}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return store, nil
	}
	batchSize := 2
	summary := strategyExecutionSummary([]strategy.StrategyEvent{{EventID: "event-1"}})
	summary.BacktestTrades = make([]strategy.BacktestTrade, batchSize+1)

	err := persistBacktestResults(context.Background(), config.Config{
		Result: config.ResultConfig{
			EventBatchSize: 1000,
			TradeBatchSize: batchSize,
		},
	}, summary)
	if !errors.Is(err, writeErr) {
		t.Fatalf("persistBacktestResults() error = %v, want %v", err, writeErr)
	}
	if store.summary.RunID != "" {
		t.Fatalf("summary = %#v, want zero value when trade batch fails", store.summary)
	}
}

func TestPersistBacktestResultsStopsBeforeSummaryWhenBatchFails(t *testing.T) {
	originalResultBuilder := buildResultStore
	t.Cleanup(func() { buildResultStore = originalResultBuilder })
	writeErr := errors.New("batch failed")
	store := &fakeResultStore{appendErrAtCall: 2, appendErr: writeErr}
	buildResultStore = func(ctx context.Context, cfg config.Config) (resultStore, error) {
		return store, nil
	}
	batchSize := 2
	events := make([]strategy.StrategyEvent, batchSize+1)

	err := persistBacktestResults(context.Background(), config.Config{
		Result: config.ResultConfig{
			EventBatchSize: batchSize,
			TradeBatchSize: 1000,
		},
	}, strategyExecutionSummary(events))
	if !errors.Is(err, writeErr) {
		t.Fatalf("persistBacktestResults() error = %v, want %v", err, writeErr)
	}
	if store.summary.RunID != "" {
		t.Fatalf("summary = %#v, want zero value when event batch fails", store.summary)
	}
}

func TestRunRequiresClickHouse(t *testing.T) {
	path := writeConfig(t, `
[runtime]
run_id = "run-1"
strategy_set = "supertrend"

[data]
exchange = "binance"
market = "um"
symbols = ["ETHUSDT"]
interval = "3m"
start_time = "2026-01-01T00:00:00Z"
end_time = "2026-01-02T00:00:00Z"
`)

	if err := Run(context.Background(), path); err == nil {
		t.Fatal("Run() error = nil, want clickhouse required error")
	}
}

func TestRunStrategyBacktestRejectsUnsupportedStrategySet(t *testing.T) {
	_, err := runStrategyBacktest(context.Background(), config.Config{
		Runtime: config.RuntimeConfig{
			RunID:       "run-1",
			StrategySet: "unknown",
		},
		Data: config.DataConfig{
			Exchange: "binance",
			Market:   "um",
			Symbols:  []string{"ETHUSDT"},
			Interval: "1m",
		},
	}, reader.Dataset{})
	if err == nil {
		t.Fatal("runStrategyBacktest() error = nil, want unsupported strategy set error")
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

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}

func appTestKlines(symbol string, interval string, start int64, end int64, intervalMillis int64) []marketmodel.Kline {
	klines := []marketmodel.Kline{}
	for openTime := start; openTime < end; openTime += intervalMillis {
		klines = append(klines, marketmodel.Kline{
			Exchange:    "binance",
			Market:      "um",
			Symbol:      symbol,
			Interval:    interval,
			OpenTime:    openTime,
			CloseTime:   openTime + intervalMillis - 1,
			Open:        "100",
			High:        "110",
			Low:         "90",
			Close:       "105",
			Volume:      "10",
			QuoteVolume: "1050",
			IsClosed:    true,
		})
	}
	return klines
}

type fakeMarketStore struct {
	request        reader.Request
	requests       []reader.Request
	klinesBySeries map[reader.SeriesKey][]marketmodel.Kline
	closed         bool
}

func (s *fakeMarketStore) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]marketmodel.Kline, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.request = reader.Request{
		Exchange: exchange,
		Market:   market,
		Symbol:   symbol,
		Interval: interval,
		Start:    start,
		End:      end,
	}
	s.requests = append(s.requests, s.request)
	return s.klinesBySeries[reader.SeriesKey{Symbol: symbol, Interval: interval}], nil
}

func (s *fakeMarketStore) Close() error {
	s.closed = true
	return nil
}

type fakeResultStore struct {
	events           []strategy.StrategyEvent
	trades           []strategy.BacktestTrade
	summary          strategy.BacktestRunSummary
	appendBatchSizes []int
	tradeBatchSizes  []int
	appendCalls      int
	tradesCalls      int
	appendErr        error
	appendErrAtCall  int
	tradesErr        error
	tradesErrAtCall  int
	summaryErr       error
	closed           bool
	writeOrder       string
}

func (s *fakeResultStore) AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.appendCalls++
	if s.appendErr != nil && (s.appendErrAtCall == 0 || s.appendErrAtCall == s.appendCalls) {
		return s.appendErr
	}
	s.appendBatchSizes = append(s.appendBatchSizes, len(events))
	s.events = append(s.events, events...)
	s.appendWriteOrder("events")
	return nil
}

func (s *fakeResultStore) SaveBacktestTrades(ctx context.Context, trades []strategy.BacktestTrade) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.tradesCalls++
	if s.tradesErr != nil && (s.tradesErrAtCall == 0 || s.tradesErrAtCall == s.tradesCalls) {
		return s.tradesErr
	}
	s.tradeBatchSizes = append(s.tradeBatchSizes, len(trades))
	s.trades = append(s.trades, trades...)
	s.appendWriteOrder("trades")
	return nil
}

func (s *fakeResultStore) SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.summaryErr != nil {
		return s.summaryErr
	}
	s.summary = summary
	s.appendWriteOrder("summary")
	return nil
}

func (s *fakeResultStore) Close() error {
	s.closed = true
	return nil
}

func (s *fakeResultStore) appendWriteOrder(item string) {
	if s.writeOrder != "" {
		s.writeOrder += ","
	}
	s.writeOrder += item
}

func strategyExecutionSummary(events []strategy.StrategyEvent) simulator.ExecutionSummary {
	return simulator.ExecutionSummary{
		StrategyEvents: events,
		RunSummary: strategy.BacktestRunSummary{
			RunID: "run-1",
		},
	}
}
