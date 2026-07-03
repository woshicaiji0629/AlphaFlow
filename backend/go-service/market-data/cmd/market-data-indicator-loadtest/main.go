package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alphaflow/go-service/market-data/internal/indicator"
	"alphaflow/go-service/market-data/internal/model"
)

type loadStore struct {
	mu             sync.Mutex
	klines         []model.Kline
	lastOpenTime   int64
	redisLatency   time.Duration
	rangeReads     atomic.Uint64
	redisWrites    atomic.Uint64
	lastIndicators map[string]int64
}

func (s *loadStore) LastOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	return s.lastOpenTime, true, nil
}

func (s *loadStore) RangeKlines(context.Context, string, string, string, string, int64, int64) ([]model.Kline, error) {
	s.rangeReads.Add(1)
	return s.klines, nil
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

func (s *loadStore) SetIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	s.mu.Lock()
	s.lastIndicators[indicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)] = snapshot.OpenTime
	s.mu.Unlock()
	return nil
}

func (s *loadStore) SetLatestIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	if s.redisLatency > 0 {
		time.Sleep(s.redisLatency)
	}
	s.redisWrites.Add(1)
	return nil
}

func (s *loadStore) SetIndicatorWindow(_ context.Context, snapshot model.IndicatorWindowSnapshot) error {
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

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	symbolCount := flag.Int("symbols", 500, "number of symbols per exchange")
	lookback := flag.Int("lookback", 200, "closed klines per symbol/interval")
	runs := flag.Int("runs", 1, "number of RunOnce executions")
	redisLatency := flag.Duration("redis-latency", 0, "simulated Redis indicator write latency")
	flag.Parse()

	if *symbolCount <= 0 {
		log.Fatal("symbols must be positive")
	}
	if *lookback <= 0 {
		log.Fatal("lookback must be positive")
	}
	if *runs <= 0 {
		log.Fatal("runs must be positive")
	}

	klines := makeKlines(*lookback)
	store := &loadStore{
		klines:         klines,
		lastOpenTime:   klines[len(klines)-1].OpenTime,
		redisLatency:   *redisLatency,
		lastIndicators: map[string]int64{},
	}
	rules := makeRules(*symbolCount)
	tasks := countTasks(rules)
	runner := indicator.NewRunner(store, indicator.RunnerOptions{
		Rules:           rules,
		ScanInterval:    10 * time.Second,
		LookbackPeriods: int64(*lookback),
	})

	startedAt := time.Now()
	for run := 0; run < *runs; run++ {
		if err := runner.RunOnce(context.Background()); err != nil {
			log.Fatalf("RunOnce %d: %v", run+1, err)
		}
	}
	elapsed := time.Since(startedAt)

	fmt.Printf("exchanges=%d symbols_per_exchange=%d tasks=%d lookback=%d runs=%d redis_latency=%s\n",
		len(rules),
		*symbolCount,
		tasks,
		*lookback,
		*runs,
		*redisLatency,
	)
	fmt.Printf("elapsed=%s throughput=%.2f tasks/s\n", elapsed, float64(tasks*(*runs))/elapsed.Seconds())
	fmt.Printf("range_reads=%d redis_writes=%d\n",
		store.rangeReads.Load(),
		store.redisWrites.Load(),
	)
}

func indicatorKey(exchange string, market string, symbol string, interval string) string {
	return strings.Join([]string{exchange, market, symbol, interval}, "\x00")
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
		closeValue := 100 + float64(i%50)*0.1
		klines = append(klines, model.Kline{
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
			Volume:              formatFloat(1000 + float64(i%20)),
			QuoteVolume:         formatFloat(100000 + float64(i%20)*100),
			TradeCount:          int64(100 + i%50),
			TakerBuyVolume:      formatFloat(500 + float64(i%10)),
			TakerBuyQuoteVolume: formatFloat(50000 + float64(i%10)*100),
			IsClosed:            true,
		})
	}
	return klines
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.4f", value)
}
