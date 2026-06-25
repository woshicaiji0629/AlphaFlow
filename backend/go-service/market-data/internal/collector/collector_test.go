package collector

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	lastOpenTime int64
	hasLast      bool
}

type fakeREST struct{}

func (fakeREST) Exchange() string {
	return "binance"
}

func (fakeREST) Market() string {
	return "um"
}

func (fakeREST) FetchKlines(
	context.Context,
	string,
	string,
	int,
	int64,
) ([]model.Kline, error) {
	return nil, nil
}

func (fakeREST) FetchOpenInterest(context.Context, string) (model.OpenInterest, error) {
	return model.OpenInterest{}, nil
}

func (s fakeStore) LastOpenTime(
	context.Context,
	string,
	string,
	string,
	string,
) (int64, bool, error) {
	return s.lastOpenTime, s.hasLast, nil
}

func (s fakeStore) UpsertKline(context.Context, model.Kline) error {
	return nil
}

func (s fakeStore) SetOpenInterest(context.Context, model.OpenInterest) error {
	return nil
}

func (s fakeStore) SetLastPrice(context.Context, model.LastPrice) error {
	return nil
}

func (s fakeStore) SetMarkPrice(context.Context, model.MarkPrice) error {
	return nil
}

func (s fakeStore) SetBookTicker(context.Context, model.BookTicker) error {
	return nil
}

func (s fakeStore) AddLiquidation(context.Context, model.Liquidation, int64) error {
	return nil
}

func TestNextStartTimeWithoutExistingData(t *testing.T) {
	c := New(testOptions(), fakeREST{}, nil, fakeStore{})

	got, err := c.nextStartTime(context.Background(), "ETHUSDT", "3m")
	if err != nil {
		t.Fatalf("nextStartTime: %v", err)
	}
	if got != 0 {
		t.Fatalf("nextStartTime = %d, want 0", got)
	}
}

func TestNextStartTimeAfterExistingKline(t *testing.T) {
	c := New(testOptions(), fakeREST{}, nil, fakeStore{
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
