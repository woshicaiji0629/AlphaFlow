package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	lastOpenTime int64
	hasLast      bool
	statuses     []model.MarketStatus
}

type fakeREST struct {
	fetchKlinesErr error
	fetchKlines    int
}

func (fakeREST) Exchange() string {
	return "binance"
}

func (fakeREST) Market() string {
	return "um"
}

func (r *fakeREST) FetchKlines(
	context.Context,
	string,
	string,
	int,
	int64,
) ([]model.Kline, error) {
	r.fetchKlines++
	if r.fetchKlinesErr != nil {
		return nil, r.fetchKlinesErr
	}
	return []model.Kline{{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "1m",
		OpenTime:  1700000000000,
		CloseTime: 1700000059999,
		IsClosed:  true,
	}}, nil
}

func (*fakeREST) FetchOpenInterest(context.Context, string) (model.OpenInterest, error) {
	return model.OpenInterest{}, nil
}

func (s *fakeStore) LastOpenTime(
	context.Context,
	string,
	string,
	string,
	string,
) (int64, bool, error) {
	return s.lastOpenTime, s.hasLast, nil
}

func (s *fakeStore) UpsertKline(context.Context, model.Kline) error {
	return nil
}

func (s *fakeStore) SetOpenInterest(context.Context, model.OpenInterest) error {
	return nil
}

func (s *fakeStore) SetLastPrice(context.Context, model.LastPrice) error {
	return nil
}

func (s *fakeStore) SetMarkPrice(context.Context, model.MarkPrice) error {
	return nil
}

func (s *fakeStore) SetBookTicker(context.Context, model.BookTicker) error {
	return nil
}

func (s *fakeStore) AddLiquidation(context.Context, model.Liquidation, int64) error {
	return nil
}

func (s *fakeStore) SetMarketStatus(_ context.Context, status model.MarketStatus) error {
	s.statuses = append(s.statuses, status)
	return nil
}

func TestNextStartTimeWithoutExistingData(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{})

	got, err := c.nextStartTime(context.Background(), "ETHUSDT", "3m")
	if err != nil {
		t.Fatalf("nextStartTime: %v", err)
	}
	if got != 0 {
		t.Fatalf("nextStartTime = %d, want 0", got)
	}
}

func TestNextStartTimeAfterExistingKline(t *testing.T) {
	c := New(testOptions(), &fakeREST{}, nil, &fakeStore{
		lastOpenTime: 1700000000000,
		hasLast:      true,
	})

	got, err := c.nextStartTime(context.Background(), "ETHUSDT", "5m")
	if err != nil {
		t.Fatalf("nextStartTime: %v", err)
	}

	const want int64 = 1700000300000
	if got != want {
		t.Fatalf("nextStartTime = %d, want %d", got, want)
	}
}

func TestBackfillMarksMarketAvailableAfterSuccessfulUpdate(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{}, nil, store)

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if len(store.statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(store.statuses))
	}
	if !store.statuses[0].Available {
		t.Fatalf("market status available = false, want true")
	}
}

func TestRunMarksMarketUnavailableAfterBackfillFailure(t *testing.T) {
	store := &fakeStore{}
	c := New(testOptions(), &fakeREST{fetchKlinesErr: errors.New("exchange unavailable")}, nil, store)

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(store.statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(store.statuses))
	}
	if store.statuses[0].Available {
		t.Fatalf("market status available = true, want false")
	}
	if store.statuses[0].Reason == "" {
		t.Fatal("expected unavailable reason")
	}
}

func TestBackfillSkipsWhenNextStartTimeHasNoClosedWindow(t *testing.T) {
	rest := &fakeREST{}
	store := &fakeStore{
		lastOpenTime: 1700000000000,
		hasLast:      true,
	}
	c := New(testOptions(), rest, nil, store)
	c.now = func() time.Time {
		return time.UnixMilli(1700000061000)
	}

	if err := c.Backfill(context.Background()); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if rest.fetchKlines != 0 {
		t.Fatalf("FetchKlines calls = %d, want 0", rest.fetchKlines)
	}
	if len(store.statuses) != 0 {
		t.Fatalf("statuses = %d, want 0", len(store.statuses))
	}
}

func testOptions() Options {
	return Options{
		Symbols:              []string{"ETHUSDT"},
		Intervals:            []string{"1m"},
		RESTLimit:            200,
		ReconnectDelay:       time.Second,
		LiquidationLimit:     200,
		PollOpenInterest:     false,
		OpenInterestInterval: time.Minute,
		MarkPriceInterval:    "1s",
	}
}
