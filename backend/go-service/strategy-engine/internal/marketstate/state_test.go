package marketstate

import (
	"strings"
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func TestStoreSeedBuildsContext(t *testing.T) {
	store := New(Options{Now: func() int64 { return 4000 }})
	target := testTarget()
	store.Seed(strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": testSnapshot(target, 3000),
		},
	})

	got, degraded, reason, err := store.BuildContext(target, nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if degraded {
		t.Fatalf("degraded = true reason=%s", reason)
	}
	if got.Snapshots["3m"].Current.Close != "101" {
		t.Fatalf("current close = %q, want 101", got.Snapshots["3m"].Current.Close)
	}
}

func TestStoreApplyRejectsExpiredMessage(t *testing.T) {
	store := New(Options{Now: func() int64 { return 4000 }})
	_, err := store.Apply(marketbus.SnapshotEnvelope{
		Type:      marketbus.SnapshotTypeRealtime,
		Target:    marketbus.SnapshotTarget{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m"},
		Kline:     &marketmodel.Kline{Symbol: "ETHUSDT"},
		Indicator: &marketmodel.IndicatorSnapshot{OpenTime: 1000, UpdatedAt: 2000},
		CreatedAt: 1000,
		ExpiresAt: 3000,
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Apply() error = %v, want expired", err)
	}
}

func TestStoreApplySkipsOlderRealtime(t *testing.T) {
	store := New(Options{Now: func() int64 { return 4000 }, MaxMessageAge: time.Minute})
	target := testTarget()
	store.Seed(strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": testSnapshot(target, 3000),
		},
	})
	newer := marketbus.NewRealtimeEnvelope(marketmodel.IndicatorRealtimeSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
		OpenTime: 2000,
		Kline:    marketmodel.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m", Close: "102"},
		Values:   map[string]string{"last_price": "102"},
		UpdatedAt: 3000,
	}, 3000, time.Minute)
	older := marketbus.NewRealtimeEnvelope(marketmodel.IndicatorRealtimeSnapshot{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
		OpenTime: 1000,
		Kline:    marketmodel.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m", Close: "101"},
		Values:   map[string]string{"last_price": "101"},
		UpdatedAt: 3500,
	}, 3500, time.Minute)

	applied, err := store.Apply(newer)
	if err != nil || !applied {
		t.Fatalf("Apply(newer) applied=%v error=%v", applied, err)
	}
	applied, err = store.Apply(older)
	if err != nil {
		t.Fatalf("Apply(older) error = %v", err)
	}
	if applied {
		t.Fatal("Apply(older) applied = true, want false")
	}
	got, _, _, err := store.BuildContext(target, nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if got.Snapshots["3m"].Current.Close != "102" {
		t.Fatalf("current close = %q, want 102", got.Snapshots["3m"].Current.Close)
	}
}

func TestStoreBuildContextReturnsDegradedForStaleRealtime(t *testing.T) {
	store := New(Options{
		Now:              func() int64 { return 30_000 },
		RealtimeStaleAge: 10 * time.Second,
		ClosedStaleFactor: 100,
	})
	target := testTarget()
	store.Seed(strategy.Context{
		Target: target,
		Snapshots: map[string]strategy.Snapshot{
			"3m": testSnapshot(target, 1000),
		},
	})

	_, degraded, reason, err := store.BuildContext(target, nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if !degraded || !strings.Contains(reason, "realtime stale") {
		t.Fatalf("degraded=%v reason=%q, want realtime stale", degraded, reason)
	}
}

func testTarget() strategy.Target {
	return strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
}

func testSnapshot(target strategy.Target, updatedAt int64) strategy.Snapshot {
	return strategy.Snapshot{
		Target: target,
		Current: marketmodel.Kline{
			Exchange: target.Exchange,
			Market:   target.Market,
			Symbol:   target.Symbol,
			Interval: target.Interval,
			Close:    "101",
		},
		Indicator: strategy.IndicatorView{
			OpenTime:  1000,
			CloseTime: 2000,
			Values:    map[string]string{"last_price": "101"},
			UpdatedAt: updatedAt,
		},
		Window: strategy.IndicatorWindowView{
			OpenTime:    1000,
			CloseTime:   2000,
			SampleCount: 20,
			UpdatedAt:   updatedAt,
		},
		Health:    strategy.HealthView{OK: true, UpdatedAt: updatedAt},
		UpdatedAt: updatedAt,
	}
}
