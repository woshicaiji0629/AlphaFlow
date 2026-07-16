package health

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	mu                     sync.Mutex
	lastOpenTimes          map[string]int64
	lastIndicatorOpenTimes map[string]int64
	klines                 map[string][]model.Kline
	marketAvailable        bool
	symbolAvailable        *bool
	written                []model.DataHealth
	checkDelay             time.Duration
	activeChecks           atomic.Int32
	maxActiveChecks        atomic.Int32
}

func (s *fakeStore) IsSymbolAvailable(context.Context, string, string, string) (bool, error) {
	if s.symbolAvailable == nil {
		return s.marketAvailable, nil
	}
	return *s.symbolAvailable, nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		lastOpenTimes:          map[string]int64{},
		lastIndicatorOpenTimes: map[string]int64{},
		klines:                 map[string][]model.Kline{},
		marketAvailable:        true,
	}
}

func (s *fakeStore) LastOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	active := s.activeChecks.Add(1)
	for current := s.maxActiveChecks.Load(); active > current && !s.maxActiveChecks.CompareAndSwap(current, active); current = s.maxActiveChecks.Load() {
	}
	if s.checkDelay > 0 {
		time.Sleep(s.checkDelay)
	}
	s.activeChecks.Add(-1)
	value, ok := s.lastOpenTimes["default"]
	return value, ok, nil
}

func (s *fakeStore) RangeKlines(
	_ context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]model.Kline, error) {
	values := s.klines[key(exchange, market, symbol, interval)]
	result := make([]model.Kline, 0, len(values))
	for _, kline := range values {
		if kline.OpenTime >= start && kline.OpenTime <= end {
			result = append(result, kline)
		}
	}
	return result, nil
}

func (s *fakeStore) IsMarketAvailable(context.Context, string, string) (bool, error) {
	return s.marketAvailable, nil
}

func (s *fakeStore) LastIndicatorOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	value, ok := s.lastIndicatorOpenTimes["default"]
	return value, ok, nil
}

func (s *fakeStore) SetDataHealth(_ context.Context, health model.DataHealth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, health)
	return nil
}

func TestRunnerWritesOKHealth(t *testing.T) {
	store := newFakeStore()
	now := time.UnixMilli(10 * 60 * 1000)
	lastOpenTime := now.Add(-time.Minute).UnixMilli()
	store.lastOpenTimes["default"] = lastOpenTime
	store.lastIndicatorOpenTimes["default"] = lastOpenTime
	store.klines[key("binance", "um", "ETHUSDT", "1m")] = closedKlines(lastOpenTime, time.Minute, 5)
	runner := testRunner(store, now)

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.KlineStatus != model.HealthStatusOK {
		t.Fatalf("kline status = %q, want ok", got.KlineStatus)
	}
	if got.IndicatorStatus != model.HealthStatusOK {
		t.Fatalf("indicator status = %q, want ok", got.IndicatorStatus)
	}
	if got.Reason != "" {
		t.Fatalf("reason = %q, want empty", got.Reason)
	}
}

func TestRunnerReportsMissingKlineAndIndicator(t *testing.T) {
	store := newFakeStore()
	runner := testRunner(store, time.UnixMilli(10*60*1000))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.KlineStatus != model.HealthStatusMissing {
		t.Fatalf("kline status = %q, want missing", got.KlineStatus)
	}
	if got.IndicatorStatus != model.HealthStatusMissing {
		t.Fatalf("indicator status = %q, want missing", got.IndicatorStatus)
	}
}

func TestRunnerReportsStaleKline(t *testing.T) {
	store := newFakeStore()
	now := time.UnixMilli(10 * 60 * 1000)
	lastOpenTime := now.Add(-3 * time.Minute).UnixMilli()
	store.lastOpenTimes["default"] = lastOpenTime
	store.lastIndicatorOpenTimes["default"] = lastOpenTime
	store.klines[key("binance", "um", "ETHUSDT", "1m")] = closedKlines(lastOpenTime, time.Minute, 5)
	runner := testRunner(store, now)

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.KlineStatus != model.HealthStatusStale {
		t.Fatalf("kline status = %q, want stale", got.KlineStatus)
	}
}

func TestRunnerReportsRecentGap(t *testing.T) {
	store := newFakeStore()
	now := time.UnixMilli(10 * 60 * 1000)
	lastOpenTime := now.Add(-time.Minute).UnixMilli()
	store.lastOpenTimes["default"] = lastOpenTime
	store.lastIndicatorOpenTimes["default"] = lastOpenTime
	store.klines[key("binance", "um", "ETHUSDT", "1m")] = closedKlines(lastOpenTime, time.Minute, 4)
	runner := testRunner(store, now)

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.KlineStatus != model.HealthStatusGap {
		t.Fatalf("kline status = %q, want gap", got.KlineStatus)
	}
}

func TestRunnerReportsStaleIndicator(t *testing.T) {
	store := newFakeStore()
	now := time.UnixMilli(10 * 60 * 1000)
	lastOpenTime := now.Add(-time.Minute).UnixMilli()
	store.lastOpenTimes["default"] = lastOpenTime
	store.lastIndicatorOpenTimes["default"] = lastOpenTime - int64(time.Minute/time.Millisecond)
	store.klines[key("binance", "um", "ETHUSDT", "1m")] = closedKlines(lastOpenTime, time.Minute, 5)
	runner := testRunner(store, now)

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.IndicatorStatus != model.HealthStatusStale {
		t.Fatalf("indicator status = %q, want stale", got.IndicatorStatus)
	}
}

func TestRunnerSkipsUnavailableMarket(t *testing.T) {
	store := newFakeStore()
	store.marketAvailable = false
	runner := testRunner(store, time.UnixMilli(10*60*1000))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := onlyHealth(t, store)
	if got.KlineStatus != model.HealthStatusSkipped {
		t.Fatalf("kline status = %q, want skipped", got.KlineStatus)
	}
	if got.IndicatorStatus != model.HealthStatusSkipped {
		t.Fatalf("indicator status = %q, want skipped", got.IndicatorStatus)
	}
}

func TestRunnerSkipsUnavailableSymbol(t *testing.T) {
	unavailable := false
	store := newFakeStore()
	store.symbolAvailable = &unavailable
	runner := testRunner(store, time.UnixMilli(10*60*1000))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	written := store.writtenHealth()
	if len(written) == 0 || written[0].KlineStatus != model.HealthStatusSkipped || written[0].IndicatorStatus != model.HealthStatusSkipped {
		t.Fatalf("health = %#v, want skipped", written)
	}
}

func TestRunnerChecksIntervalsConcurrently(t *testing.T) {
	store := newFakeStore()
	store.checkDelay = 10 * time.Millisecond
	runner := NewRunner(store, Options{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m", "3m", "5m", "15m"},
		}},
		Workers: 4,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := store.maxActiveChecks.Load(); got < 2 {
		t.Fatalf("max concurrent checks = %d, want at least 2", got)
	}
	if got := len(store.writtenHealth()); got != 4 {
		t.Fatalf("written health count = %d, want 4", got)
	}
}

func testRunner(store *fakeStore, now time.Time) *Runner {
	runner := NewRunner(store, Options{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		GapLookback: 5,
	})
	runner.now = func() time.Time { return now }
	return runner
}

func onlyHealth(t *testing.T, store *fakeStore) model.DataHealth {
	t.Helper()
	written := store.writtenHealth()
	if len(written) != 1 {
		t.Fatalf("written health count = %d, want 1", len(written))
	}
	return written[0]
}

func (s *fakeStore) writtenHealth() []model.DataHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.DataHealth(nil), s.written...)
}

func closedKlines(lastOpenTime int64, interval time.Duration, count int) []model.Kline {
	intervalMillis := int64(interval / time.Millisecond)
	start := lastOpenTime - int64(count-1)*intervalMillis
	klines := make([]model.Kline, 0, count)
	for index := 0; index < count; index++ {
		openTime := start + int64(index)*intervalMillis
		klines = append(klines, model.Kline{
			Exchange:  "binance",
			Market:    "um",
			Symbol:    "ETHUSDT",
			Interval:  "1m",
			OpenTime:  openTime,
			CloseTime: openTime + intervalMillis - 1,
			IsClosed:  true,
		})
	}
	return klines
}

func key(exchange string, market string, symbol string, interval string) string {
	return exchange + "\x00" + market + "\x00" + symbol + "\x00" + interval
}
