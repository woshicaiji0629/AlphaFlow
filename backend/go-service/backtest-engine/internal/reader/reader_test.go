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
	request Request
	klines  []marketmodel.Kline
	err     error
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
	return s.klines, s.err
}
