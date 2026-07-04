package reader

import (
	"context"
	"errors"
	"testing"

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
	wantRequests := []SeriesKey{
		{Symbol: "ETHUSDT", Interval: "1m"},
		{Symbol: "ETHUSDT", Interval: "5m"},
		{Symbol: "BTCUSDT", Interval: "1m"},
		{Symbol: "BTCUSDT", Interval: "5m"},
	}
	for index, want := range wantRequests {
		if store.requests[index].Symbol != want.Symbol || store.requests[index].Interval != want.Interval {
			t.Fatalf("request[%d] = %s/%s, want %s/%s", index, store.requests[index].Symbol, store.requests[index].Interval, want.Symbol, want.Interval)
		}
	}
	if result.TotalKlines() == 0 {
		t.Fatal("total klines = 0, want loaded klines")
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
	request        Request
	requests       []Request
	klines         []marketmodel.Kline
	klinesBySeries map[SeriesKey][]marketmodel.Kline
	err            error
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
	s.request = Request{
		Exchange: exchange,
		Market:   market,
		Symbol:   symbol,
		Interval: interval,
		Start:    start,
		End:      end,
	}
	s.requests = append(s.requests, s.request)
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
