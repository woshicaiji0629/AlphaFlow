package indicator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	mu                       sync.Mutex
	available                bool
	hasLast                  bool
	lastOpenTime             int64
	hasLastIndicator         bool
	lastIndicatorOpenTime    int64
	lastIndicatorOpenTimeErr error
	klines                   []model.Kline
	rangeCalls               int
	rangeRequests            [][2]int64
	snapshots                []model.IndicatorSnapshot
	latestSnapshots          []model.IndicatorSnapshot
	rangeDelay               time.Duration
	activeRangeCalls         atomic.Int64
	maxActiveRangeCalls      atomic.Int64
}

func (s *fakeStore) LastOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastOpenTime, s.hasLast, nil
}

func (s *fakeStore) RangeKlines(_ context.Context, _ string, _ string, _ string, _ string, start int64, end int64) ([]model.Kline, error) {
	active := s.activeRangeCalls.Add(1)
	for {
		maxActive := s.maxActiveRangeCalls.Load()
		if active <= maxActive || s.maxActiveRangeCalls.CompareAndSwap(maxActive, active) {
			break
		}
	}
	defer s.activeRangeCalls.Add(-1)
	if s.rangeDelay > 0 {
		time.Sleep(s.rangeDelay)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rangeCalls++
	s.rangeRequests = append(s.rangeRequests, [2]int64{start, end})
	klines := make([]model.Kline, 0, len(s.klines))
	for _, kline := range s.klines {
		if kline.OpenTime < start || kline.OpenTime > end {
			continue
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

func (s *fakeStore) IsMarketAvailable(context.Context, string, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.available, nil
}

func (s *fakeStore) LastIndicatorOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastIndicatorOpenTime, s.hasLastIndicator, s.lastIndicatorOpenTimeErr
}

func (s *fakeStore) SetIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snapshot)
	return nil
}

func (s *fakeStore) SetLatestIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestSnapshots = append(s.latestSnapshots, snapshot)
	return nil
}

func TestRunnerWritesIndicatorSnapshot(t *testing.T) {
	klines := make([]model.Kline, 0, 120)
	for index := 0; index < 120; index++ {
		klines = append(klines, testKline(int64(index), 100+float64(index), true))
	}
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
	})
	runner.now = func() time.Time {
		return time.UnixMilli(1700000000000)
	}

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(store.snapshots))
	}
	got := store.snapshots[0]
	if got.Exchange != "binance" || got.Market != "um" || got.Symbol != "ETHUSDT" || got.Interval != "1m" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.Values["ema7"] == "" {
		t.Fatalf("missing ema7: %#v", got.Values)
	}
	if got.Signals["ema_alignment"] == "" {
		t.Fatalf("missing ema alignment: %#v", got.Signals)
	}
}

func TestRunnerSkipsAlreadyCalculatedClosedKline(t *testing.T) {
	klines := make([]model.Kline, 0, 120)
	for index := 0; index < 120; index++ {
		klines = append(klines, testKline(int64(index), 100+float64(index), true))
	}
	store := &fakeStore{
		available:             true,
		hasLast:               true,
		lastOpenTime:          klines[len(klines)-1].OpenTime,
		hasLastIndicator:      true,
		lastIndicatorOpenTime: klines[len(klines)-1].OpenTime,
		klines:                klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("snapshots = %d, want 0", len(store.snapshots))
	}
}

func TestRunnerSkipsUnavailableMarket(t *testing.T) {
	store := &fakeStore{
		available: false,
		hasLast:   true,
		klines:    []model.Kline{testKline(1, 100, true)},
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "gate",
			Market:    "usdt",
			Symbols:   []string{"ETH_USDT"},
			Intervals: []string{"1m"},
		}},
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("snapshots = %d, want 0", len(store.snapshots))
	}
}

func TestRunnerReusesCachedWindowWithoutNewKline(t *testing.T) {
	klines := minuteKlines(10)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 5,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}
	if store.rangeCalls != 1 {
		t.Fatalf("range calls = %d, want 1", store.rangeCalls)
	}
}

func TestRunnerAppendsNewKlineToCachedWindow(t *testing.T) {
	klines := minuteKlines(10)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 5,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}
	next := minuteKline(int64(len(klines)), 110)
	store.klines = append(store.klines, next)
	store.lastOpenTime = next.OpenTime
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}

	if store.rangeCalls != 2 {
		t.Fatalf("range calls = %d, want 2", store.rangeCalls)
	}
	wantStart := klines[len(klines)-1].OpenTime + 60*1000
	if got := store.rangeRequests[1][0]; got != wantStart {
		t.Fatalf("incremental range start = %d, want %d", got, wantStart)
	}
	if got := store.snapshots[len(store.snapshots)-1].OpenTime; got != next.OpenTime {
		t.Fatalf("latest snapshot open time = %d, want %d", got, next.OpenTime)
	}
}

func TestRunnerHandlesClosedKlineFromCachedWindowWithoutRangeRead(t *testing.T) {
	klines := minuteKlines(10)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 5,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	store.mu.Lock()
	store.rangeCalls = 0
	store.rangeRequests = nil
	store.snapshots = nil
	store.mu.Unlock()

	next := minuteKline(int64(len(klines)), 110)
	if err := runner.HandleKline(context.Background(), next); err != nil {
		t.Fatalf("HandleKline: %v", err)
	}
	if store.rangeCalls != 0 {
		t.Fatalf("range calls = %d, want 0", store.rangeCalls)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(store.snapshots))
	}
	if got := store.snapshots[0].OpenTime; got != next.OpenTime {
		t.Fatalf("snapshot open time = %d, want %d", got, next.OpenTime)
	}
}

func TestRunnerSkipsOpenKlineFromHandler(t *testing.T) {
	klines := minuteKlines(10)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 5,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	store.mu.Lock()
	store.rangeCalls = 0
	store.rangeRequests = nil
	store.snapshots = nil
	store.latestSnapshots = nil
	store.mu.Unlock()

	open := minuteKline(int64(len(klines)), 110)
	open.IsClosed = false
	if err := runner.HandleKline(context.Background(), open); err != nil {
		t.Fatalf("HandleKline: %v", err)
	}
	if store.rangeCalls != 0 {
		t.Fatalf("range calls = %d, want 0", store.rangeCalls)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("closed snapshots = %d, want 0", len(store.snapshots))
	}
	if len(store.latestSnapshots) != 0 {
		t.Fatalf("latest snapshots = %d, want 0", len(store.latestSnapshots))
	}
}

func TestRunnerReloadsWindowWhenCacheHasGap(t *testing.T) {
	klines := minuteKlines(10)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   []string{"ETHUSDT"},
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 5,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}
	gapped := minuteKline(int64(len(klines)+1), 112)
	store.klines = append(store.klines, gapped)
	store.lastOpenTime = gapped.OpenTime
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}

	if store.rangeCalls != 3 {
		t.Fatalf("range calls = %d, want 3", store.rangeCalls)
	}
}

func TestRunnerCalculatesWithConcurrentWorkers(t *testing.T) {
	klines := minuteKlines(120)
	store := &fakeStore{
		available:    true,
		hasLast:      true,
		lastOpenTime: klines[len(klines)-1].OpenTime,
		klines:       klines,
		rangeDelay:   20 * time.Millisecond,
	}
	runner := NewRunner(store, RunnerOptions{
		Rules: []Rule{{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   makeTestSymbols(16),
			Intervals: []string{"1m"},
		}},
		LookbackPeriods: 120,
	})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := store.maxActiveRangeCalls.Load(); got < 2 {
		t.Fatalf("max active range calls = %d, want concurrent calls", got)
	}
	if len(store.snapshots) != 16 {
		t.Fatalf("snapshots = %d, want 16", len(store.snapshots))
	}
}

func minuteKlines(count int) []model.Kline {
	klines := make([]model.Kline, 0, count)
	for index := 0; index < count; index++ {
		klines = append(klines, minuteKline(int64(index), 100+float64(index)))
	}
	return klines
}

func minuteKline(index int64, price float64) model.Kline {
	openTime := index * 60 * 1000
	return model.Kline{
		Exchange:    "binance",
		Market:      "um",
		Symbol:      "ETHUSDT",
		Interval:    "1m",
		OpenTime:    openTime,
		CloseTime:   openTime + 60*1000 - 1,
		Open:        format(price),
		High:        format(price + 2),
		Low:         format(price - 2),
		Close:       format(price + 1),
		Volume:      format(10 + float64(index%5)),
		QuoteVolume: format((10 + float64(index%5)) * price),
		IsClosed:    true,
	}
}

func makeTestSymbols(count int) []string {
	symbols := make([]string, 0, count)
	for index := 0; index < count; index++ {
		symbols = append(symbols, "TEST"+format(float64(index))+"USDT")
	}
	return symbols
}
