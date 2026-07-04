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

	klines, err := item.ReadKlines(context.Background(), Request{
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
	if len(klines) != 1 {
		t.Fatalf("klines len = %d, want 1", len(klines))
	}
	if store.request.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", store.request.Symbol)
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
