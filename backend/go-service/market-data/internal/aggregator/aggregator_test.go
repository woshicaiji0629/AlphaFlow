package aggregator

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	lastOpenTime    int64
	hasLast         bool
	ranges          map[int64][]model.Kline
	writes          []model.Kline
	available       *bool
	symbolAvailable *bool
}

func (s *fakeStore) IsSymbolAvailable(context.Context, string, string, string) (bool, error) {
	if s.symbolAvailable == nil {
		return s.IsMarketAvailable(context.Background(), "", "")
	}
	return *s.symbolAvailable, nil
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

func (s *fakeStore) RangeKlines(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	start int64,
	_ int64,
) ([]model.Kline, error) {
	return s.ranges[start], nil
}

func (s *fakeStore) UpsertKline(_ context.Context, kline model.Kline) error {
	s.writes = append(s.writes, kline)
	return nil
}

func (s *fakeStore) IsMarketAvailable(context.Context, string, string) (bool, error) {
	if s.available == nil {
		return true, nil
	}
	return *s.available, nil
}

func TestAggregateClosedKlines(t *testing.T) {
	rule := Rule{
		Exchange:       "gate",
		Market:         "usdt",
		SourceInterval: "5m",
		TargetInterval: "10m",
	}
	openTime := int64(1700000000000)
	got, ok, err := Aggregate(rule, "ETH_USDT", openTime, []model.Kline{
		sourceKline(openTime, "1", "3", "0.5", "2", "10", "100", 5),
		sourceKline(openTime+300000, "2", "4", "1.5", "3", "20", "200", 7),
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if !ok {
		t.Fatal("expected aggregate to be produced")
	}
	if got.Interval != "10m" || got.Open != "1" || got.High != "4" || got.Low != "0.5" || got.Close != "3" {
		t.Fatalf("unexpected OHLC: %#v", got)
	}
	if got.Volume != "30" || got.QuoteVolume != "300" || got.TradeCount != 12 {
		t.Fatalf("unexpected totals: %#v", got)
	}
	if got.CloseTime != openTime+600000-1 {
		t.Fatalf("close time = %d", got.CloseTime)
	}
}

func TestAggregateRequiresCompleteClosedWindow(t *testing.T) {
	rule := Rule{
		Exchange:       "gate",
		Market:         "usdt",
		SourceInterval: "5m",
		TargetInterval: "10m",
	}
	openTime := int64(1700000000000)
	_, ok, err := Aggregate(rule, "ETH_USDT", openTime, []model.Kline{
		sourceKline(openTime, "1", "3", "0.5", "2", "10", "100", 5),
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if ok {
		t.Fatal("expected incomplete window to be skipped")
	}

	open := sourceKline(openTime, "1", "3", "0.5", "2", "10", "100", 5)
	open.IsClosed = false
	_, ok, err = Aggregate(rule, "ETH_USDT", openTime, []model.Kline{
		open,
		sourceKline(openTime+300000, "2", "4", "1.5", "3", "20", "200", 7),
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if ok {
		t.Fatal("expected open child kline to be skipped")
	}
}

func TestRunOnceWritesMissingDerivedWindow(t *testing.T) {
	openTime := int64(1700000000000)
	store := &fakeStore{
		hasLast:      true,
		lastOpenTime: openTime - 600000,
		ranges: map[int64][]model.Kline{
			openTime: {
				sourceKline(openTime, "1", "3", "0.5", "2", "10", "100", 5),
				sourceKline(openTime+300000, "2", "4", "1.5", "3", "20", "200", 7),
			},
		},
	}
	agg := New(store, Options{
		Rules: []Rule{{
			Exchange:       "gate",
			Market:         "usdt",
			Symbols:        []string{"ETH_USDT"},
			SourceInterval: "5m",
			TargetInterval: "10m",
		}},
	})
	agg.now = func() time.Time {
		return time.UnixMilli(openTime + 1200000)
	}

	if err := agg.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(store.writes))
	}
	if store.writes[0].Interval != "10m" {
		t.Fatalf("interval = %q, want 10m", store.writes[0].Interval)
	}
}

func TestRunOnceSkipsUnavailableMarket(t *testing.T) {
	openTime := int64(1700000000000)
	available := false
	store := &fakeStore{
		available: &available,
		hasLast:   true,
		ranges: map[int64][]model.Kline{
			openTime: {
				sourceKline(openTime, "1", "3", "0.5", "2", "10", "100", 5),
				sourceKline(openTime+300000, "2", "4", "1.5", "3", "20", "200", 7),
			},
		},
	}
	agg := New(store, Options{
		Rules: []Rule{{
			Exchange:       "gate",
			Market:         "usdt",
			Symbols:        []string{"ETH_USDT"},
			SourceInterval: "5m",
			TargetInterval: "10m",
		}},
	})
	agg.now = func() time.Time {
		return time.UnixMilli(openTime + 1200000)
	}

	if err := agg.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("writes = %d, want 0", len(store.writes))
	}
}

func TestRunOnceSkipsUnavailableSymbol(t *testing.T) {
	available := true
	unavailable := false
	store := &fakeStore{available: &available, symbolAvailable: &unavailable}
	aggregator := New(store, Options{Rules: []Rule{{Exchange: "gate", Market: "usdt", Symbols: []string{"ETH_USDT"}, SourceInterval: "5m", TargetInterval: "10m"}}})

	if err := aggregator.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("writes = %d, want 0", len(store.writes))
	}
}

func sourceKline(
	openTime int64,
	open string,
	high string,
	low string,
	close string,
	volume string,
	quoteVolume string,
	tradeCount int64,
) model.Kline {
	return model.Kline{
		Exchange:    "gate",
		Market:      "usdt",
		Symbol:      "ETH_USDT",
		Interval:    "5m",
		OpenTime:    openTime,
		CloseTime:   openTime + 300000 - 1,
		Open:        open,
		High:        high,
		Low:         low,
		Close:       close,
		Volume:      volume,
		QuoteVolume: quoteVolume,
		TradeCount:  tradeCount,
		IsClosed:    true,
	}
}
