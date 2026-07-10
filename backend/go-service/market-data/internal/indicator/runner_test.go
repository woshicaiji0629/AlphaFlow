package indicator

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
)

type fakeStore struct {
	mu                       sync.Mutex
	available                bool
	symbolAvailable          *bool
	hasLast                  bool
	lastOpenTime             int64
	hasLastIndicator         bool
	lastIndicatorOpenTime    int64
	lastIndicatorOpenTimeErr error
	lastIndicatorCalls       int
	klines                   []model.Kline
	rangeCalls               int
	rangeRequests            [][2]int64
	snapshots                []model.IndicatorSnapshot
	latestSnapshots          []model.IndicatorSnapshot
	windowSnapshots          []model.IndicatorWindowSnapshot
	latestWindowSnapshots    []model.IndicatorWindowSnapshot
	realtimeSnapshots        []model.IndicatorRealtimeSnapshot
	writeOrder               []string
	rangeDelay               time.Duration
	activeRangeCalls         atomic.Int64
	maxActiveRangeCalls      atomic.Int64
}

func (s *fakeStore) IsSymbolAvailable(context.Context, string, string, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.symbolAvailable == nil {
		return s.available, nil
	}
	return *s.symbolAvailable, nil
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
	s.lastIndicatorCalls++
	return s.lastIndicatorOpenTime, s.hasLastIndicator, s.lastIndicatorOpenTimeErr
}

func (s *fakeStore) RecentIndicators(context.Context, string, string, string, string, int) ([]model.IndicatorSnapshot, error) {
	return nil, nil
}

func (s *fakeStore) SetIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snapshot)
	s.writeOrder = append(s.writeOrder, "indicator")
	return nil
}

func (s *fakeStore) SetIndicatorWindow(
	_ context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.windowSnapshots = append(s.windowSnapshots, snapshot)
	s.writeOrder = append(s.writeOrder, "window")
	return nil
}

func (s *fakeStore) SetClosedIndicator(
	_ context.Context,
	snapshot model.IndicatorSnapshot,
	windowSnapshot model.IndicatorWindowSnapshot,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snapshot)
	s.windowSnapshots = append(s.windowSnapshots, windowSnapshot)
	s.writeOrder = append(s.writeOrder, "closed")
	return nil
}

func (s *fakeStore) SetLatestIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestSnapshots = append(s.latestSnapshots, snapshot)
	return nil
}

func (s *fakeStore) SetLatestIndicatorWindow(
	_ context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestWindowSnapshots = append(s.latestWindowSnapshots, snapshot)
	return nil
}

func (s *fakeStore) SetIndicatorRealtime(
	_ context.Context,
	snapshot model.IndicatorRealtimeSnapshot,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.realtimeSnapshots = append(s.realtimeSnapshots, snapshot)
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
	if len(store.snapshots) != 20 {
		t.Fatalf("snapshots = %d, want 20", len(store.snapshots))
	}
	got := store.snapshots[len(store.snapshots)-1]
	if got.Exchange != "binance" || got.Market != "um" || got.Symbol != "ETHUSDT" || got.Interval != "1m" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.Values["ema7"] == "" {
		t.Fatalf("missing ema7: %#v", got.Values)
	}
	if got.Signals["ema_alignment"] == "" {
		t.Fatalf("missing ema alignment: %#v", got.Signals)
	}
	if len(store.windowSnapshots) != 1 {
		t.Fatalf("window snapshots = %d, want 1", len(store.windowSnapshots))
	}
	window := store.windowSnapshots[0]
	if window.Version != indicatorwindow.Version {
		t.Fatalf("window version = %q, want %q", window.Version, indicatorwindow.Version)
	}
	if window.Values["ema7_win_slope"] == "" {
		t.Fatalf("missing ema7 window slope: %#v", window.Values)
	}
	if window.Signals["ema_alignment_win_latest"] == "" {
		t.Fatalf("missing ema alignment window latest: %#v", window.Signals)
	}
	if len(store.writeOrder) != 21 {
		t.Fatalf("write order length = %d, want 21", len(store.writeOrder))
	}
	for index := 0; index < 20; index++ {
		if store.writeOrder[index] != "indicator" {
			t.Fatalf("write order[%d] = %q, want indicator", index, store.writeOrder[index])
		}
	}
	if got := store.writeOrder[20]; got != "window" {
		t.Fatalf("write order[20] = %q, want window", got)
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

func TestRunnerSkipsUnavailableSymbol(t *testing.T) {
	unavailable := false
	store := &fakeStore{available: true, symbolAvailable: &unavailable}
	runner := NewRunner(store, RunnerOptions{Rules: []Rule{{
		Exchange: "binance", Market: "um", Symbols: []string{"ETHUSDT"}, Intervals: []string{"1m"},
	}}})

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if store.rangeCalls != 0 {
		t.Fatalf("range calls = %d, want 0", store.rangeCalls)
	}
	if err := runner.HandleKline(context.Background(), model.Kline{
		Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "1m", IsClosed: true,
	}); err != nil {
		t.Fatalf("HandleKline: %v", err)
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

func TestRunnerNormalizesIncrementalKlinesBeforeAppending(t *testing.T) {
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
	currentLast := klines[len(klines)-1]
	next := minuteKline(int64(len(klines)), 110)
	replacementNext := next
	replacementNext.Close = "111"
	afterNext := minuteKline(int64(len(klines)+1), 111)
	store.klines = []model.Kline{
		afterNext,
		currentLast,
		next,
		replacementNext,
	}
	store.lastOpenTime = afterNext.OpenTime

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}

	if store.rangeCalls != 2 {
		t.Fatalf("range calls = %d, want 2", store.rangeCalls)
	}
	if got := store.snapshots[len(store.snapshots)-1].OpenTime; got != afterNext.OpenTime {
		t.Fatalf("latest snapshot open time = %d, want %d", got, afterNext.OpenTime)
	}
	key := windowKey("binance", "um", "ETHUSDT", "1m")
	runner.mu.Lock()
	window := runner.windows[key].Clone()
	runner.mu.Unlock()
	windowKlines := window.Klines()
	if len(windowKlines) != 5 {
		t.Fatalf("window length = %d, want 5", len(windowKlines))
	}
	if got := windowKlines[len(windowKlines)-2].Close; got != "111" {
		t.Fatalf("deduped kline close = %q, want 111", got)
	}
	if got := windowKlines[len(windowKlines)-1].OpenTime; got != afterNext.OpenTime {
		t.Fatalf("window last open time = %d, want %d", got, afterNext.OpenTime)
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

func TestRunnerSkipsScanAfterHandledClosedKline(t *testing.T) {
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
	next := minuteKline(int64(len(klines)), 110)
	store.mu.Lock()
	store.klines = append(store.klines, next)
	store.lastOpenTime = next.OpenTime
	store.rangeCalls = 0
	store.rangeRequests = nil
	store.snapshots = nil
	store.lastIndicatorCalls = 0
	store.mu.Unlock()

	if err := runner.HandleKline(context.Background(), next); err != nil {
		t.Fatalf("HandleKline: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce after HandleKline: %v", err)
	}
	if store.rangeCalls != 0 {
		t.Fatalf("range calls = %d, want 0", store.rangeCalls)
	}
	if store.lastIndicatorCalls != 0 {
		t.Fatalf("last indicator calls = %d, want 0", store.lastIndicatorCalls)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(store.snapshots))
	}
	if got := store.snapshots[0].OpenTime; got != next.OpenTime {
		t.Fatalf("snapshot open time = %d, want %d", got, next.OpenTime)
	}
}

func TestRunnerWritesRealtimeSnapshotForOpenKlineFromHandler(t *testing.T) {
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
	store.latestWindowSnapshots = nil
	store.realtimeSnapshots = nil
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
	if len(store.latestSnapshots) != 1 {
		t.Fatalf("latest snapshots = %d, want 1", len(store.latestSnapshots))
	}
	if len(store.latestWindowSnapshots) != 1 {
		t.Fatalf("latest window snapshots = %d, want 1", len(store.latestWindowSnapshots))
	}
	if len(store.realtimeSnapshots) != 1 {
		t.Fatalf("realtime snapshots = %d, want 1", len(store.realtimeSnapshots))
	}
	realtime := store.realtimeSnapshots[0]
	if realtime.Kline.IsClosed {
		t.Fatal("realtime kline is closed, want open")
	}
	if realtime.Kline.Close != open.Close {
		t.Fatalf("realtime kline close = %q, want %q", realtime.Kline.Close, open.Close)
	}
	if realtime.Values["vwap"] == "" {
		t.Fatalf("missing realtime vwap: %#v", realtime.Values)
	}
	key := windowKey("binance", "um", "ETHUSDT", "1m")
	runner.mu.Lock()
	cachedWindow := runner.windows[key].Clone()
	runner.mu.Unlock()
	cachedKlines := cachedWindow.Klines()
	if len(cachedKlines) != 5 {
		t.Fatalf("cached window length = %d, want 5", len(cachedKlines))
	}
	if got := cachedKlines[len(cachedKlines)-1].OpenTime; got != klines[len(klines)-1].OpenTime {
		t.Fatalf("cached window last open time = %d, want %d", got, klines[len(klines)-1].OpenTime)
	}
}

func TestRunnerCachedIndicatorSnapshotsReturnsAlignedSuffixWithCurrentSnapshot(t *testing.T) {
	klines := minuteKlines(25)
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, 25)
	key := windowKey("binance", "um", "ETHUSDT", "1m")
	current := testIndicatorSnapshot(klines[len(klines)-1], "current")
	runner := NewRunner(&fakeStore{}, RunnerOptions{})
	runner.indicatorSnapshots[key] = []model.IndicatorSnapshot{
		testIndicatorSnapshot(klines[18], "stale-prefix"),
		testIndicatorSnapshot(klines[21], "cached-21"),
		testIndicatorSnapshot(klines[22], "cached-22"),
		testIndicatorSnapshot(klines[23], "cached-23"),
	}

	snapshots := runner.cachedIndicatorSnapshots(key, window, current)

	if len(snapshots) != 4 {
		t.Fatalf("snapshots = %d, want 4", len(snapshots))
	}
	for offset, wantIndex := range []int{21, 22, 23, 24} {
		if got := snapshots[offset].OpenTime; got != klines[wantIndex].OpenTime {
			t.Fatalf("snapshot[%d] open time = %d, want %d", offset, got, klines[wantIndex].OpenTime)
		}
	}
	if got := snapshots[len(snapshots)-1].Values["source"]; got != "current" {
		t.Fatalf("current snapshot source = %q, want current", got)
	}
}

func TestRunnerCachedIndicatorSnapshotsForWindowAlignsBeforeLatest(t *testing.T) {
	klines := minuteKlines(25)
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, 25)
	key := windowKey("binance", "um", "ETHUSDT", "1m")
	runner := NewRunner(&fakeStore{}, RunnerOptions{})
	runner.indicatorSnapshots[key] = []model.IndicatorSnapshot{
		testIndicatorSnapshot(klines[20], "cached-20"),
		testIndicatorSnapshot(klines[21], "cached-21"),
		testIndicatorSnapshot(klines[22], "cached-22"),
		testIndicatorSnapshot(klines[23], "cached-23"),
	}

	snapshots := runner.cachedIndicatorSnapshotsForWindow(key, window)

	if len(snapshots) != 4 {
		t.Fatalf("snapshots = %d, want 4", len(snapshots))
	}
	for offset, wantIndex := range []int{20, 21, 22, 23} {
		if got := snapshots[offset].OpenTime; got != klines[wantIndex].OpenTime {
			t.Fatalf("snapshot[%d] open time = %d, want %d", offset, got, klines[wantIndex].OpenTime)
		}
	}
}

func TestRunnerCalculatedIndicatorSnapshotsForWindowOnlyFillsMissingSnapshots(t *testing.T) {
	klines := minuteKlines(25)
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, 25)
	cached := []model.IndicatorSnapshot{
		testIndicatorSnapshot(klines[23], "cached-23"),
		testIndicatorSnapshot(klines[24], "cached-24"),
	}
	runner := NewRunner(&fakeStore{}, RunnerOptions{})

	snapshots, err := runner.calculatedIndicatorSnapshotsForWindow(window, cached)
	if err != nil {
		t.Fatalf("calculatedIndicatorSnapshotsForWindow: %v", err)
	}

	if len(snapshots) != 20 {
		t.Fatalf("snapshots = %d, want 20", len(snapshots))
	}
	if got := snapshots[0].OpenTime; got != klines[5].OpenTime {
		t.Fatalf("first snapshot open time = %d, want %d", got, klines[5].OpenTime)
	}
	if got := snapshots[18].Values["source"]; got != "cached-23" {
		t.Fatalf("snapshot[18] source = %q, want cached-23", got)
	}
	if got := snapshots[19].Values["source"]; got != "cached-24" {
		t.Fatalf("snapshot[19] source = %q, want cached-24", got)
	}
	if snapshots[17].Values["source"] != "" {
		t.Fatalf("snapshot[17] unexpectedly reused cached source: %#v", snapshots[17].Values)
	}
}

func TestRunnerCalculatedIndicatorSnapshotsForWindowUsesCompleteCache(t *testing.T) {
	klines := minuteKlines(25)
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, 25)
	cached := make([]model.IndicatorSnapshot, 0, 20)
	for _, kline := range klines[5:] {
		cached = append(cached, testIndicatorSnapshot(kline, "cached"))
	}
	var calculateCalls atomic.Uint64
	runner := NewRunner(&fakeStore{}, RunnerOptions{
		OnCalculateWindow: func() {
			calculateCalls.Add(1)
		},
	})

	snapshots, err := runner.calculatedIndicatorSnapshotsForWindow(window, cached)
	if err != nil {
		t.Fatalf("calculatedIndicatorSnapshotsForWindow: %v", err)
	}

	if got := calculateCalls.Load(); got != 0 {
		t.Fatalf("calculate calls = %d, want 0", got)
	}
	if len(snapshots) != 20 {
		t.Fatalf("snapshots = %d, want 20", len(snapshots))
	}
	if got := snapshots[0].OpenTime; got != klines[5].OpenTime {
		t.Fatalf("first snapshot open time = %d, want %d", got, klines[5].OpenTime)
	}
}

func TestRunnerCalculatedIndicatorSnapshotsForWindowCalculatesOnlyMissingLatest(t *testing.T) {
	klines := minuteKlines(25)
	window := indicatorcalc.NewCalculationWindowFromKlines(klines, 25)
	cached := make([]model.IndicatorSnapshot, 0, 19)
	for _, kline := range klines[5:24] {
		cached = append(cached, testIndicatorSnapshot(kline, "cached"))
	}
	var calculateCalls atomic.Uint64
	runner := NewRunner(&fakeStore{}, RunnerOptions{
		OnCalculateWindow: func() {
			calculateCalls.Add(1)
		},
	})

	snapshots, err := runner.calculatedIndicatorSnapshotsForWindow(window, cached)
	if err != nil {
		t.Fatalf("calculatedIndicatorSnapshotsForWindow: %v", err)
	}

	if got := calculateCalls.Load(); got != 1 {
		t.Fatalf("calculate calls = %d, want 1", got)
	}
	if len(snapshots) != 20 {
		t.Fatalf("snapshots = %d, want 20", len(snapshots))
	}
	if got := snapshots[19].OpenTime; got != klines[24].OpenTime {
		t.Fatalf("last snapshot open time = %d, want %d", got, klines[24].OpenTime)
	}
}

func TestRunnerCalculatedIndicatorSnapshotsUsesFixedWarmupWindow(t *testing.T) {
	klines := minuteKlines(310)
	window := newCalculationWindowFromKlines(klines, 310)
	runner := NewRunner(&fakeStore{}, RunnerOptions{
		LookbackPeriods: 310,
		WarmupPeriods:   250,
		WindowLookback:  50,
	})

	snapshots, err := runner.calculatedIndicatorSnapshotsForWindow(window, nil)
	if err != nil {
		t.Fatalf("calculatedIndicatorSnapshotsForWindow: %v", err)
	}

	if len(snapshots) != 50 {
		t.Fatalf("snapshots = %d, want 50", len(snapshots))
	}
	if got := snapshots[0].OpenTime; got != klines[260].OpenTime {
		t.Fatalf("first snapshot open time = %d, want %d", got, klines[260].OpenTime)
	}
	if got := snapshots[0].Values["sample_count"]; got != "250" {
		t.Fatalf("first snapshot sample_count = %q, want 250", got)
	}
	if got := snapshots[len(snapshots)-1].Values["sample_count"]; got != "250" {
		t.Fatalf("last snapshot sample_count = %q, want 250", got)
	}
}

func TestRunnerValidateIndicatorSnapshotContinuityDetectsGap(t *testing.T) {
	klines := minuteKlines(3)
	snapshots := []model.IndicatorSnapshot{
		testIndicatorSnapshot(klines[0], "first"),
		testIndicatorSnapshot(klines[2], "gap"),
	}

	err := validateIndicatorSnapshotContinuity(snapshots, int64(time.Minute/time.Millisecond))
	if err == nil {
		t.Fatal("expected indicator snapshot gap")
	}
}

func TestRunnerReturnsErrorWhenIndicatorSnapshotsHaveGap(t *testing.T) {
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
	if err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected indicator snapshot gap")
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
	if len(store.snapshots) != 16*indicatorWindowLookback {
		t.Fatalf("snapshots = %d, want %d", len(store.snapshots), 16*indicatorWindowLookback)
	}
	if len(store.windowSnapshots) != 16 {
		t.Fatalf("window snapshots = %d, want 16", len(store.windowSnapshots))
	}
}

func testIndicatorSnapshot(kline model.Kline, source string) model.IndicatorSnapshot {
	return model.IndicatorSnapshot{
		Exchange:  kline.Exchange,
		Market:    kline.Market,
		Symbol:    kline.Symbol,
		Interval:  kline.Interval,
		OpenTime:  kline.OpenTime,
		CloseTime: kline.CloseTime,
		Values: map[string]string{
			"source": source,
		},
		Signals: map[string]string{},
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

func testKline(index int64, price float64, closed bool) model.Kline {
	return model.Kline{
		Exchange:    "binance",
		Market:      "um",
		Symbol:      "ETHUSDT",
		Interval:    "1m",
		OpenTime:    index * 1000,
		CloseTime:   index*1000 + 999,
		Open:        format(price),
		High:        format(price + 2),
		Low:         format(price - 2),
		Close:       format(price + 1),
		Volume:      format(10 + float64(index%5)),
		QuoteVolume: format((10 + float64(index%5)) * price),
		IsClosed:    closed,
	}
}

func format(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
