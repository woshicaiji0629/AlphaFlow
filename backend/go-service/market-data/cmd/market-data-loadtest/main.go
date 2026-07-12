package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/exchange"
	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/logger"
)

type fakeREST struct {
	symbols []string
}

func (fakeREST) Exchange() string {
	return "loadtest"
}

func (fakeREST) Market() string {
	return "sim"
}

func (r fakeREST) FetchKlines(
	context.Context,
	string,
	string,
	int,
	int64,
) ([]model.Kline, error) {
	return nil, nil
}

func (fakeREST) FetchOpenInterest(context.Context, string) (model.OpenInterest, error) {
	return model.OpenInterest{}, nil
}

type blockingWS struct {
	started chan struct{}
	once    sync.Once
}

func (w *blockingWS) Run(ctx context.Context, _ []exchange.Stream, _ exchange.Handler) error {
	w.once.Do(func() { close(w.started) })
	<-ctx.Done()
	return ctx.Err()
}

type loadStore struct {
	latency time.Duration
	writes  atomic.Uint64
}

func (s *loadStore) LastOpenTime(
	context.Context,
	string,
	string,
	string,
	string,
) (int64, bool, error) {
	return 0, false, nil
}

func (s *loadStore) RangeKlines(context.Context, string, string, string, string, int64, int64) ([]model.Kline, error) {
	return nil, nil
}

func (s *loadStore) UpsertKline(context.Context, model.Kline) error {
	s.write()
	return nil
}

func (s *loadStore) UpsertKlines(_ context.Context, klines []model.Kline) error {
	for range klines {
		s.write()
	}
	return nil
}

func (s *loadStore) SetLastPrice(context.Context, model.LastPrice) error {
	s.write()
	return nil
}

func (s *loadStore) SetMarkPrice(context.Context, model.MarkPrice) error {
	s.write()
	return nil
}

func (s *loadStore) SetBookTicker(context.Context, model.BookTicker) error {
	s.write()
	return nil
}

func (s *loadStore) SetOpenInterest(context.Context, model.OpenInterest) error {
	s.write()
	return nil
}

func (s *loadStore) AddLiquidation(context.Context, model.Liquidation, int64) error {
	s.write()
	return nil
}

func (s *loadStore) SetMarketStatus(context.Context, model.MarketStatus) error {
	return nil
}

func (s *loadStore) SetWebSocketStatus(context.Context, model.WebSocketStatus) error {
	return nil
}

func (s *loadStore) write() {
	if s.latency > 0 {
		time.Sleep(s.latency)
	}
	s.writes.Add(1)
}

func main() {
	setupLogger()

	symbolCount := flag.Int("symbols", 50, "number of symbols to simulate")
	duration := flag.Duration("duration", 30*time.Second, "load test duration")
	rate := flag.Int("rate", 5000, "events per second")
	storeLatency := flag.Duration("store-latency", time.Millisecond, "simulated store write latency")
	flag.Parse()

	if *symbolCount <= 0 {
		exitWithError("symbols must be positive")
	}
	if *duration <= 0 {
		exitWithError("duration must be positive")
	}
	if *rate <= 0 {
		exitWithError("rate must be positive")
	}

	symbols := makeSymbols(*symbolCount)
	store := &loadStore{latency: *storeLatency}
	ws := &blockingWS{started: make(chan struct{})}
	c := collector.New(collector.Options{
		Symbols:              symbols,
		Intervals:            []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"},
		RESTLimit:            200,
		ReconnectDelay:       collector.DefaultReconnectDelay(),
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}, fakeREST{symbols: symbols}, ws, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.RunRealtime(ctx)
	}()
	select {
	case <-ws.started:
	case err := <-errCh:
		if err == nil {
			exitWithError("collector stopped before load started")
		}
		exitWithError("collector stopped before load started", "error", err)
	case <-time.After(5 * time.Second):
		exitWithError("collector did not become ready")
	}

	startedAt := time.Now()
	sent := runLoad(ctx, c, symbols, *duration, *rate)
	cancel()
	err := <-errCh
	if err != nil && err != context.Canceled {
		exitWithError("collector stopped", "error", err)
	}

	elapsed := time.Since(startedAt)
	stats := c.Stats()
	fmt.Printf("symbols=%d duration=%s rate=%d store_latency=%s\n", *symbolCount, *duration, *rate, *storeLatency)
	fmt.Printf("sent_events=%d elapsed=%s send_throughput=%.2f/s\n", sent, elapsed, float64(sent)/elapsed.Seconds())
	fmt.Printf("processed_events=%d store_writes=%d process_errors=%d dropped_latest_events=%d coalesced_latest_events=%d flushed_latest_events=%d\n",
		stats.ProcessedEvents,
		store.writes.Load(),
		stats.ProcessEventErrors,
		stats.DroppedLatestEvents,
		stats.CoalescedLatestEvents,
		stats.FlushedLatestEvents,
	)
	fmt.Printf("queue_len=%d queue_cap=%d queue_peak=%d\n", stats.QueueLen, stats.QueueCap, stats.QueuePeak)
	fmt.Printf("last_received_at=%d last_processed_at=%d source_delay_max_ms=%d queue_delay_max_ms=%d process_max_ms=%d out_of_order_events=%d duplicate_kline_events=%d stale_kline_events=%d open_after_closed_events=%d websocket_kline_events=%d startup_rest_klines=%d derived_klines=%d kline_corrections=%d kline_gaps_detected=%d kline_gap_bars=%d kline_gap_requests=%d kline_gap_request_errors=%d\n",
		stats.LastEventReceivedAt,
		stats.LastEventProcessedAt,
		stats.SourceDelayMaxMillis,
		stats.QueueDelayMaxMillis,
		stats.ProcessMaxMillis,
		stats.OutOfOrderEvents,
		stats.DuplicateKlineEvents,
		stats.StaleKlineEvents,
		stats.OpenAfterClosedEvents,
		stats.WebSocketKlineEvents,
		stats.StartupRESTKlines,
		stats.DerivedKlines,
		stats.KlineCorrections,
		stats.KlineGapsDetected,
		stats.KlineGapBars,
		stats.KlineGapRequests,
		stats.KlineGapRequestErrors,
	)
}

func setupLogger() {
	if err := logger.Setup(logger.Config{
		Service: "market-data-loadtest",
		Level:   "error",
		Format:  "text",
		Output:  "stderr",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		os.Exit(1)
	}
}

func exitWithError(message string, attrs ...any) {
	slog.Error(message, attrs...)
	os.Exit(1)
}

func runLoad(
	ctx context.Context,
	c *collector.Collector,
	symbols []string,
	duration time.Duration,
	rate int,
) uint64 {
	interval := time.Second / time.Duration(rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()

	var sent uint64
	for {
		select {
		case <-ctx.Done():
			return sent
		case <-deadline.C:
			return sent
		case <-ticker.C:
			symbol := symbols[int(sent)%len(symbols)]
			now := time.Now().UnixMilli()
			handleEvent(ctx, c, sent, symbol, now)
			sent++
		}
	}
}

func handleEvent(ctx context.Context, c *collector.Collector, index uint64, symbol string, now int64) {
	switch {
	case index%500 == 0:
		_ = c.HandleLiquidation(ctx, model.Liquidation{
			Exchange:  "loadtest",
			Market:    "sim",
			Symbol:    symbol,
			Side:      "SELL",
			Price:     "100",
			TradeTime: now,
			EventTime: now,
		})
	case index%100 == 0:
		_ = c.HandleKline(ctx, model.Kline{
			Exchange:  "loadtest",
			Market:    "sim",
			Symbol:    symbol,
			Interval:  "1m",
			OpenTime:  now - 60000,
			CloseTime: now - 1,
			Open:      "100",
			High:      "101",
			Low:       "99",
			Close:     "100.5",
			Volume:    "10",
			IsClosed:  true,
			EventTime: now,
		})
	case index%2 == 0:
		_ = c.HandleBookTicker(ctx, model.BookTicker{
			Exchange:  "loadtest",
			Market:    "sim",
			Symbol:    symbol,
			BidPrice:  "100",
			AskPrice:  "100.1",
			EventTime: now,
		})
	default:
		_ = c.HandleLastPrice(ctx, model.LastPrice{
			Exchange:  "loadtest",
			Market:    "sim",
			Symbol:    symbol,
			Price:     "100",
			EventTime: now,
			TradeTime: now,
		})
	}
}

func makeSymbols(count int) []string {
	symbols := make([]string, 0, count)
	for i := 0; i < count; i++ {
		symbols = append(symbols, fmt.Sprintf("SYM%03dUSDT", i+1))
	}
	return symbols
}
