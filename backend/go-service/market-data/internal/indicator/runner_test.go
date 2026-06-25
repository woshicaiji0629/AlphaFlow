package indicator

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
)

type fakeStore struct {
	available    bool
	hasLast      bool
	lastOpenTime int64
	klines       []model.Kline
	snapshots    []model.IndicatorSnapshot
}

func (s *fakeStore) LastOpenTime(context.Context, string, string, string, string) (int64, bool, error) {
	return s.lastOpenTime, s.hasLast, nil
}

func (s *fakeStore) RangeKlines(context.Context, string, string, string, string, int64, int64) ([]model.Kline, error) {
	return s.klines, nil
}

func (s *fakeStore) IsMarketAvailable(context.Context, string, string) (bool, error) {
	return s.available, nil
}

func (s *fakeStore) SetIndicator(_ context.Context, snapshot model.IndicatorSnapshot) error {
	s.snapshots = append(s.snapshots, snapshot)
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
