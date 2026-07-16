package reader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

func TestReadKlinesUsesStore(t *testing.T) {
	store := &fakeKlineStore{
		klines: []marketmodel.Kline{{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
			OpenTime: 1000,
		}},
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := item.ReadKlines(context.Background(), Request{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
		Start:    1000,
		End:      2000,
	})
	if err != nil {
		t.Fatalf("ReadKlines() error = %v", err)
	}
	if len(result.Klines) != 1 {
		t.Fatalf("klines len = %d, want 1", len(result.Klines))
	}
	if store.request.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", store.request.Symbol)
	}
}

func TestReadKlinesAppliesWarmupAndValidatesBothPhases(t *testing.T) {
	const minute = int64(60_000)
	store := &fakeKlineStore{
		klines: []marketmodel.Kline{
			{OpenTime: 0},
			{OpenTime: minute},
			{OpenTime: 2 * minute},
			{OpenTime: 3 * minute},
		},
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := item.ReadKlines(context.Background(), Request{
		Exchange:   "binance",
		Market:     "um",
		Symbol:     "ETHUSDT",
		Interval:   "1m",
		Start:      2 * minute,
		End:        4 * minute,
		WarmupBars: 2,
	})
	if err != nil {
		t.Fatalf("ReadKlines() error = %v", err)
	}
	if store.request.Start != 0 {
		t.Fatalf("store start = %d, want 0", store.request.Start)
	}
	if result.WarmupCount != 2 || result.TradingCount != 2 {
		t.Fatalf("counts warmup=%d trading=%d, want 2/2", result.WarmupCount, result.TradingCount)
	}
}

func TestReadKlinesRejectsMissingWarmup(t *testing.T) {
	const minute = int64(60_000)
	store := &fakeKlineStore{
		klines: []marketmodel.Kline{
			{OpenTime: minute},
			{OpenTime: 2 * minute},
			{OpenTime: 3 * minute},
		},
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = item.ReadKlines(context.Background(), Request{
		Exchange:   "binance",
		Market:     "um",
		Symbol:     "ETHUSDT",
		Interval:   "1m",
		Start:      2 * minute,
		End:        4 * minute,
		WarmupBars: 2,
	})
	if err == nil {
		t.Fatal("ReadKlines() error = nil, want missing warmup error")
	}
}

func TestReadDatasetLoadsSymbolsAndConfirmIntervals(t *testing.T) {
	const minute = int64(60_000)
	store := &fakeKlineStore{
		klinesBySeries: map[SeriesKey][]marketmodel.Kline{
			{Symbol: "ETHUSDT", Interval: "1m"}: datasetTestKlines(8*minute, 20*minute, minute),
			{Symbol: "ETHUSDT", Interval: "5m"}: datasetTestKlines(0, 20*minute, 5*minute),
			{Symbol: "BTCUSDT", Interval: "1m"}: datasetTestKlines(8*minute, 20*minute, minute),
			{Symbol: "BTCUSDT", Interval: "5m"}: datasetTestKlines(0, 20*minute, 5*minute),
		},
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := item.ReadDataset(context.Background(), DatasetRequest{
		Exchange:         "binance",
		Market:           "um",
		Symbols:          []string{"ETHUSDT", "BTCUSDT"},
		Interval:         "1m",
		ConfirmIntervals: []string{"5m", "1m"},
		Start:            10 * minute,
		End:              20 * minute,
		WarmupBars:       2,
	})
	if err != nil {
		t.Fatalf("ReadDataset() error = %v", err)
	}
	if len(result.Series) != 4 {
		t.Fatalf("series len = %d, want 4", len(result.Series))
	}
	wantSeries := []SeriesKey{
		{Symbol: "ETHUSDT", Interval: "1m"},
		{Symbol: "ETHUSDT", Interval: "5m"},
		{Symbol: "BTCUSDT", Interval: "1m"},
		{Symbol: "BTCUSDT", Interval: "5m"},
	}
	for index, want := range wantSeries {
		if result.Series[index].Key != want {
			t.Fatalf("series[%d] = %#v, want %#v", index, result.Series[index].Key, want)
		}
	}
	if result.TotalKlines() == 0 {
		t.Fatal("total klines = 0, want loaded klines")
	}
}

func TestReadDatasetLoadsSeriesConcurrently(t *testing.T) {
	store := &fakeKlineStore{
		klinesBySeries: map[SeriesKey][]marketmodel.Kline{},
		delay:          10 * time.Millisecond,
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := item.ReadDataset(context.Background(), DatasetRequest{
		Exchange:         "binance",
		Market:           "um",
		Symbols:          []string{"ETHUSDT"},
		Interval:         "1m",
		ConfirmIntervals: []string{"3m", "5m", "15m"},
	})
	if err != nil {
		t.Fatalf("ReadDataset() error = %v", err)
	}
	if got := store.maxActive.Load(); got < 2 {
		t.Fatalf("max concurrent reads = %d, want at least 2", got)
	}
	want := []SeriesKey{
		{Symbol: "ETHUSDT", Interval: "1m"},
		{Symbol: "ETHUSDT", Interval: "3m"},
		{Symbol: "ETHUSDT", Interval: "5m"},
		{Symbol: "ETHUSDT", Interval: "15m"},
	}
	for index, key := range want {
		if result.Series[index].Key != key {
			t.Fatalf("series[%d] = %#v, want %#v", index, result.Series[index].Key, key)
		}
	}
}

func TestReadDatasetRejectsMissingConfirmInterval(t *testing.T) {
	const minute = int64(60_000)
	store := &fakeKlineStore{
		klinesBySeries: map[SeriesKey][]marketmodel.Kline{
			{Symbol: "ETHUSDT", Interval: "1m"}: datasetTestKlines(0, 4*minute, minute),
			{Symbol: "ETHUSDT", Interval: "5m"}: datasetTestKlines(5*minute, 20*minute, 5*minute),
		},
	}
	item, err := New(store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = item.ReadDataset(context.Background(), DatasetRequest{
		Exchange:         "binance",
		Market:           "um",
		Symbols:          []string{"ETHUSDT"},
		Interval:         "1m",
		ConfirmIntervals: []string{"5m"},
		Start:            10 * minute,
		End:              20 * minute,
		WarmupBars:       2,
	})
	if err == nil {
		t.Fatal("ReadDataset() error = nil, want missing confirm interval error")
	}
}

func TestReadKlinesRejectsInvalidRequest(t *testing.T) {
	item, err := New(&fakeKlineStore{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = item.ReadKlines(context.Background(), Request{
		Exchange: "binance",
		Market:   "um",
		Interval: "3m",
		Start:    2000,
		End:      1000,
	})
	if err == nil {
		t.Fatal("ReadKlines() error = nil, want validation error")
	}
}

func TestReadKlinesRejectsNegativeWarmup(t *testing.T) {
	item, err := New(&fakeKlineStore{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = item.ReadKlines(context.Background(), Request{
		Exchange:   "binance",
		Market:     "um",
		Symbol:     "ETHUSDT",
		Interval:   "1m",
		WarmupBars: -1,
	})
	if err == nil {
		t.Fatal("ReadKlines() error = nil, want validation error")
	}
}

func TestCheckDatasetReportsGapsDuplicatesAndAvailableWarmup(t *testing.T) {
	const minute = int64(60_000)
	item, err := New(&fakeKlineStore{klines: []marketmodel.Kline{
		{OpenTime: 0}, {OpenTime: 0},
		{OpenTime: 2 * minute},
		{OpenTime: 4 * minute},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := item.CheckDataset(context.Background(), DatasetRequest{
		Exchange: "binance", Market: "um", Symbols: []string{"ETHUSDT"}, Interval: "1m",
		Start: 2 * minute, End: 5 * minute, WarmupBars: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	series := report.Series[0]
	if report.Complete {
		t.Fatal("complete = true, want gaps and duplicate")
	}
	if len(series.DuplicateOpenTimes) != 1 || series.DuplicateOpenTimes[0] != 0 {
		t.Fatalf("duplicates = %v, want [0]", series.DuplicateOpenTimes)
	}
	if len(series.MissingWarmupOpenTimes) != 1 || series.MissingWarmupOpenTimes[0] != minute {
		t.Fatalf("missing warmup = %v, want [%d]", series.MissingWarmupOpenTimes, minute)
	}
	if len(series.MissingTradingOpenTimes) != 1 || series.MissingTradingOpenTimes[0] != 3*minute {
		t.Fatalf("missing trading = %v, want [%d]", series.MissingTradingOpenTimes, 3*minute)
	}
	if series.AvailableWarmupBars != 0 {
		t.Fatalf("available warmup = %d, want 0", series.AvailableWarmupBars)
	}
	if series.LongestRunBars != 1 {
		t.Fatalf("longest run = %d, want 1", series.LongestRunBars)
	}
}

func TestCheckDatasetReportsCompleteSeries(t *testing.T) {
	const minute = int64(60_000)
	item, err := New(&fakeKlineStore{klines: datasetTestKlines(0, 5*minute, minute)})
	if err != nil {
		t.Fatal(err)
	}
	report, err := item.CheckDataset(context.Background(), DatasetRequest{
		Exchange: "binance", Market: "um", Symbols: []string{"ETHUSDT"}, Interval: "1m",
		Start: 2 * minute, End: 5 * minute, WarmupBars: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Series[0].AvailableWarmupBars != 2 || report.Series[0].LongestRunBars != 5 {
		t.Fatalf("report = %#v", report)
	}
}

func TestReadKlinesWrapsStoreError(t *testing.T) {
	item, err := New(&fakeKlineStore{err: errors.New("boom")})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = item.ReadKlines(context.Background(), Request{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	})
	if err == nil {
		t.Fatal("ReadKlines() error = nil, want store error")
	}
}

type fakeKlineStore struct {
	mu             sync.Mutex
	request        Request
	requests       []Request
	klines         []marketmodel.Kline
	klinesBySeries map[SeriesKey][]marketmodel.Kline
	err            error
	delay          time.Duration
	active         atomic.Int32
	maxActive      atomic.Int32
}

func (s *fakeKlineStore) RangeKlines(
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
	active := s.active.Add(1)
	for current := s.maxActive.Load(); active > current && !s.maxActive.CompareAndSwap(current, active); current = s.maxActive.Load() {
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.active.Add(-1)
	s.mu.Lock()
	s.request = Request{
		Exchange: exchange,
		Market:   market,
		Symbol:   symbol,
		Interval: interval,
		Start:    start,
		End:      end,
	}
	s.requests = append(s.requests, s.request)
	s.mu.Unlock()
	if s.klinesBySeries != nil {
		return s.klinesBySeries[SeriesKey{Symbol: symbol, Interval: interval}], s.err
	}
	return s.klines, s.err
}

func datasetTestKlines(start int64, end int64, intervalMillis int64) []marketmodel.Kline {
	klines := []marketmodel.Kline{}
	for openTime := start; openTime < end; openTime += intervalMillis {
		klines = append(klines, marketmodel.Kline{OpenTime: openTime})
	}
	return klines
}
