package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/indicator"
	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/logger"
)

type loadStore struct {
	mu             sync.Mutex
	klines         []model.Kline
	lastOpenTime   int64
	redisLatency   time.Duration
	rangeReads     atomic.Uint64
	redisWrites    atomic.Uint64
	redisBatches   atomic.Uint64
	lastIndicators map[string]int64
	snapshots      map[string][]model.IndicatorSnapshot
	snapshotLimit  int

	recentIndicatorReads  atomic.Uint64
	recentIndicatorHits   atomic.Uint64
	recentIndicatorMisses atomic.Uint64
	calculateCalls        atomic.Uint64
}

func (s *loadStore) LastOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastOpenTime, true, nil
}

func (s *loadStore) RangeKlines(_ context.Context, exchange string, market string, symbol string, interval string, _ int64, _ int64) ([]model.Kline, error) {
	s.rangeReads.Add(1)
	s.mu.Lock()
	source := append([]model.Kline(nil), s.klines...)
	s.mu.Unlock()
	klines := make([]model.Kline, len(source))
	for index, kline := range source {
		kline.Exchange = exchange
		kline.Market = market
		kline.Symbol = symbol
		kline.Interval = interval
		klines[index] = kline
	}
	return klines, nil
}

func (s *loadStore) IsMarketAvailable(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *loadStore) LastIndicatorOpenTime(_ context.Context, exchange string, market string, symbol string, interval string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	openTime, ok := s.lastIndicators[indicatorKey(exchange, market, symbol, interval)]
	return openTime, ok, nil
}

func (s *loadStore) RecentIndicators(_ context.Context, exchange string, market string, symbol string, interval string, limit int) ([]model.IndicatorSnapshot, error) {
	s.recentIndicatorReads.Add(1)
	s.mu.Lock()
	snapshots := append([]model.IndicatorSnapshot(nil), s.snapshots[indicatorKey(exchange, market, symbol, interval)]...)
	s.mu.Unlock()
	if len(snapshots) == 0 {
		s.recentIndicatorMisses.Add(1)
		return nil, nil
	}
	s.recentIndicatorHits.Add(1)
	if limit > 0 && len(snapshots) > limit {
		snapshots = snapshots[len(snapshots)-limit:]
	}
	return snapshots, nil
}

func (s *loadStore) SetIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	s.mu.Lock()
	s.rememberIndicatorLocked(snapshot)
	s.mu.Unlock()
	return nil
}

func (s *loadStore) SetIndicators(_ context.Context, snapshots []model.IndicatorSnapshot) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(uint64(len(snapshots)))
	s.redisBatches.Add(1)
	s.mu.Lock()
	for _, snapshot := range snapshots {
		s.rememberIndicatorLocked(snapshot)
	}
	s.mu.Unlock()
	return nil
}

func (s *loadStore) SetIndicatorWindow(
	_ context.Context,
	_ model.IndicatorWindowSnapshot,
) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	return nil
}

func (s *loadStore) SetClosedIndicator(
	_ context.Context,
	snapshot model.IndicatorSnapshot,
	_ model.IndicatorWindowSnapshot,
) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	s.mu.Lock()
	s.rememberIndicatorLocked(snapshot)
	s.mu.Unlock()
	return nil
}

func (s *loadStore) rememberIndicatorLocked(snapshot model.IndicatorSnapshot) {
	key := indicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	s.lastIndicators[key] = snapshot.OpenTime
	s.snapshots[key] = appendIndicatorSnapshot(s.snapshots[key], snapshot, s.snapshotLimit)
}

func (s *loadStore) SetLatestIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	return nil
}

func (s *loadStore) SetLatestIndicatorWindow(
	_ context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	return nil
}

func (s *loadStore) SetIndicatorRealtime(
	_ context.Context,
	snapshot model.IndicatorRealtimeSnapshot,
) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	return nil
}

func (s *loadStore) AdvanceClosedKline() {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := s.klines[len(s.klines)-1]
	next := makeNextKline(last, len(s.klines))
	s.klines = append(s.klines, next)
	s.lastOpenTime = next.OpenTime
}

func main() {
	setupLogger()

	symbolCount := flag.Int("symbols", 500, "number of symbols per exchange")
	lookback := flag.Int("lookback", 200, "closed klines per symbol/interval")
	warmup := flag.Int("warmup", 0, "indicator warmup periods, defaults to lookback")
	windowLookback := flag.Int("window-lookback", 0, "indicator snapshots used for window analysis")
	snapshotCacheLimit := flag.Int("snapshot-cache-limit", 0, "recent indicator snapshots kept in the loadtest store")
	runs := flag.Int("runs", 1, "number of RunOnce executions")
	advanceEachRun := flag.Bool("advance-each-run", false, "append one closed kline after each run except the last")
	redisLatency := flag.Duration("redis-latency", 0, "simulated Redis indicator write latency")
	flag.Parse()

	if *symbolCount <= 0 {
		exitWithError("symbols must be positive")
	}
	if *lookback <= 0 {
		exitWithError("lookback must be positive")
	}
	if *warmup < 0 {
		exitWithError("warmup must be non-negative")
	}
	if *windowLookback < 0 {
		exitWithError("window-lookback must be non-negative")
	}
	if *snapshotCacheLimit < 0 {
		exitWithError("snapshot-cache-limit must be non-negative")
	}
	if *runs <= 0 {
		exitWithError("runs must be positive")
	}
	if *snapshotCacheLimit == 0 {
		*snapshotCacheLimit = *windowLookback
	}

	klines := makeKlines(*lookback)
	store := &loadStore{
		klines:         klines,
		lastOpenTime:   klines[len(klines)-1].OpenTime,
		redisLatency:   *redisLatency,
		lastIndicators: map[string]int64{},
		snapshots:      map[string][]model.IndicatorSnapshot{},
		snapshotLimit:  *snapshotCacheLimit,
	}
	rules := makeRules(*symbolCount)
	tasks := countTasks(rules)
	runner := indicator.NewRunner(store, indicator.RunnerOptions{
		Rules:              rules,
		ScanInterval:       10 * time.Second,
		LookbackPeriods:    int64(*lookback),
		WarmupPeriods:      int64(*warmup),
		WindowLookback:     *windowLookback,
		SnapshotCacheLimit: *snapshotCacheLimit,
		OnCalculateWindow: func() {
			store.calculateCalls.Add(1)
		},
	})

	startedAt := time.Now()
	runDurations := make([]time.Duration, 0, *runs)
	for run := 0; run < *runs; run++ {
		runStartedAt := time.Now()
		if err := runner.RunOnce(context.Background()); err != nil {
			exitWithError("RunOnce failed", "run", run+1, "error", err)
		}
		runDurations = append(runDurations, time.Since(runStartedAt))
		if *advanceEachRun && run < *runs-1 {
			store.AdvanceClosedKline()
		}
	}
	elapsed := time.Since(startedAt)

	fmt.Printf("exchanges=%d symbols_per_exchange=%d tasks=%d lookback=%d warmup=%d window_lookback=%d snapshot_cache_limit=%d runs=%d advance_each_run=%t redis_latency=%s\n",
		len(rules),
		*symbolCount,
		tasks,
		*lookback,
		*warmup,
		*windowLookback,
		*snapshotCacheLimit,
		*runs,
		*advanceEachRun,
		*redisLatency,
	)
	fmt.Printf("elapsed=%s throughput=%.2f tasks/s\n", elapsed, float64(tasks*(*runs))/elapsed.Seconds())
	for run, duration := range runDurations {
		phase := "repeat"
		if run == 0 {
			phase = "cold"
		} else if *advanceEachRun {
			phase = "steady_advance"
		}
		fmt.Printf("run=%d phase=%s elapsed=%s throughput=%.2f tasks/s\n",
			run+1,
			phase,
			duration,
			float64(tasks)/duration.Seconds(),
		)
	}
	fmt.Printf("range_reads=%d redis_writes=%d redis_batches=%d recent_indicator_reads=%d recent_indicator_hits=%d recent_indicator_misses=%d calculate_window_calls=%d\n",
		store.rangeReads.Load(),
		store.redisWrites.Load(),
		store.redisBatches.Load(),
		store.recentIndicatorReads.Load(),
		store.recentIndicatorHits.Load(),
		store.recentIndicatorMisses.Load(),
		store.calculateCalls.Load(),
	)
}

func setupLogger() {
	if err := logger.Setup(logger.Config{
		Service: "market-data-indicator-loadtest",
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

func indicatorKey(exchange string, market string, symbol string, interval string) string {
	return strings.Join([]string{exchange, market, symbol, interval}, "\x00")
}

func appendIndicatorSnapshot(
	snapshots []model.IndicatorSnapshot,
	snapshot model.IndicatorSnapshot,
	limit int,
) []model.IndicatorSnapshot {
	candidates := make([]model.IndicatorSnapshot, 0, len(snapshots)+1)
	for _, current := range snapshots {
		if current.OpenTime >= snapshot.OpenTime {
			continue
		}
		candidates = append(candidates, current)
	}
	candidates = append(candidates, snapshot)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[len(candidates)-limit:]
	}
	return candidates
}

func makeRules(symbolCount int) []indicator.Rule {
	return []indicator.Rule{
		{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   makeSymbols("BN", symbolCount),
			Intervals: []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"},
		},
		{
			Exchange:  "gate",
			Market:    "usdt",
			Symbols:   makeSymbols("GT", symbolCount),
			Intervals: []string{"1m", "5m", "15m", "30m", "1h", "4h"},
		},
		{
			Exchange:  "bitget",
			Market:    "usdt-futures",
			Symbols:   makeSymbols("BG", symbolCount),
			Intervals: []string{"1m", "5m", "15m", "30m", "1h", "4h"},
		},
		{
			Exchange:  "bybit",
			Market:    "linear",
			Symbols:   makeSymbols("BB", symbolCount),
			Intervals: []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h"},
		},
	}
}

func countTasks(rules []indicator.Rule) int {
	total := 0
	for _, rule := range rules {
		total += len(rule.Symbols) * len(rule.Intervals)
	}
	return total
}

func makeSymbols(prefix string, count int) []string {
	symbols := make([]string, 0, count)
	for i := 0; i < count; i++ {
		symbols = append(symbols, fmt.Sprintf("%s%03dUSDT", prefix, i+1))
	}
	return symbols
}

func makeKlines(count int) []model.Kline {
	const intervalMillis int64 = 60 * 1000
	start := time.Now().Add(-time.Duration(count) * time.Minute).UnixMilli()
	klines := make([]model.Kline, 0, count)
	for i := 0; i < count; i++ {
		openTime := start + int64(i)*intervalMillis
		klines = append(klines, makeLoadKline(openTime, i))
	}
	return klines
}

func makeNextKline(last model.Kline, index int) model.Kline {
	const intervalMillis int64 = 60 * 1000
	return makeLoadKline(last.OpenTime+intervalMillis, index)
}

func makeLoadKline(openTime int64, index int) model.Kline {
	const intervalMillis int64 = 60 * 1000
	closeValue := 100 + float64(index%50)*0.1
	return model.Kline{
		Exchange:            "loadtest",
		Market:              "sim",
		Symbol:              "LOADTEST",
		Interval:            "1m",
		OpenTime:            openTime,
		CloseTime:           openTime + intervalMillis - 1,
		Open:                formatFloat(closeValue - 0.1),
		High:                formatFloat(closeValue + 0.2),
		Low:                 formatFloat(closeValue - 0.2),
		Close:               formatFloat(closeValue),
		Volume:              formatFloat(1000 + float64(index%20)),
		QuoteVolume:         formatFloat(100000 + float64(index%20)*100),
		TradeCount:          int64(100 + index%50),
		TakerBuyVolume:      formatFloat(500 + float64(index%10)),
		TakerBuyQuoteVolume: formatFloat(50000 + float64(index%10)*100),
		IsClosed:            true,
	}
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.4f", value)
}
