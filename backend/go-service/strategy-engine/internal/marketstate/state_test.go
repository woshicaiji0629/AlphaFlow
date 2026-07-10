package marketstate

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
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
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "3m",
		OpenTime:  2000,
		Kline:     marketmodel.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m", Close: "102"},
		Values:    map[string]string{"last_price": "102"},
		UpdatedAt: 3000,
	}, 3000, time.Minute)
	older := marketbus.NewRealtimeEnvelope(marketmodel.IndicatorRealtimeSnapshot{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "3m",
		OpenTime:  1000,
		Kline:     marketmodel.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m", Close: "101"},
		Values:    map[string]string{"last_price": "101"},
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
		Now:               func() int64 { return 30_000 },
		RealtimeStaleAge:  10 * time.Second,
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

func TestStoreKeepsClosedIndicatorSeparateFromRealtime(t *testing.T) {
	store := New(Options{Now: func() int64 { return 5000 }, MaxMessageAge: time.Minute, ClosedStaleFactor: 100})
	closed := marketbus.NewClosedEnvelope(
		marketmodel.IndicatorSnapshot{
			Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m",
			OpenTime: 1000, CloseTime: 2000, UpdatedAt: 2000,
			Signals: map[string]string{"state": "closed"},
		},
		marketmodel.IndicatorWindowSnapshot{
			Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m",
			OpenTime: 1000, CloseTime: 2000, UpdatedAt: 2000,
		},
		2000,
		time.Minute,
	)
	if applied, err := store.Apply(closed); err != nil || !applied {
		t.Fatalf("Apply(closed) applied=%v error=%v", applied, err)
	}
	realtime := marketbus.NewRealtimeEnvelope(marketmodel.IndicatorRealtimeSnapshot{
		Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m",
		OpenTime: 2001, CloseTime: 3000, UpdatedAt: 3000,
		Signals: map[string]string{"state": "realtime"},
		Kline:   marketmodel.Kline{Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: "3m", Close: "101"},
	}, 3000, time.Minute)
	if applied, err := store.Apply(realtime); err != nil || !applied {
		t.Fatalf("Apply(realtime) applied=%v error=%v", applied, err)
	}
	context, _, _, err := store.BuildContext(testTarget(), nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	snapshot := context.Snapshots["3m"]
	if snapshot.Indicator.Signals["state"] != "closed" {
		t.Fatalf("closed indicator state = %q", snapshot.Indicator.Signals["state"])
	}
	if snapshot.AsOf != 2000 || snapshot.Trigger != strategy.TriggerOnEntryClose {
		t.Fatalf("snapshot timing = as_of %d trigger %q", snapshot.AsOf, snapshot.Trigger)
	}
}

func TestEntryCloseReplayMatchesHistoricalContextAndDecision(t *testing.T) {
	now := int64(0)
	store := New(Options{
		Now:               func() int64 { return now },
		MaxMessageAge:     time.Hour,
		RealtimeStaleAge:  time.Hour,
		ClosedStaleFactor: 100,
	})
	target := testTarget()
	engine := strategy.NewEngine([]strategy.Strategy{parityStrategy{}})

	const base = int64(1_000_000)
	confirm := parityFixture("5m", base-300_000, base-1, "bullish", "100")
	now = confirm.closed.UpdatedAt
	applyParityClosed(t, store, confirm)

	entries := []parityFrame{
		parityFixture("3m", base, base+180_000-1, "bullish", "101"),
		parityFixture("3m", base+180_000, base+360_000-1, "bearish", "99"),
	}
	for index, entry := range entries {
		if index == 1 {
			confirm = parityFixture("5m", base, base+300_000-1, "bearish", "100")
			now = confirm.closed.UpdatedAt
			applyParityClosed(t, store, confirm)
		}
		now = entry.closed.UpdatedAt
		applyParityRealtime(t, store, entry)
		applyParityClosed(t, store, entry)

		online, degraded, reason, err := store.BuildContext(target, []string{"5m"})
		if err != nil {
			t.Fatalf("frame %d online BuildContext() error = %v", index, err)
		}
		if degraded {
			t.Fatalf("frame %d degraded = true reason=%s", index, reason)
		}
		historical, err := parityHistoricalContext(target, entry, confirm)
		if err != nil {
			t.Fatalf("frame %d historical context error = %v", index, err)
		}
		if !reflect.DeepEqual(online, historical) {
			t.Fatalf("frame %d context mismatch\nonline=%#v\nhistorical=%#v", index, online, historical)
		}

		onlineDecision, err := engine.Evaluate(context.Background(), online)
		if err != nil {
			t.Fatalf("frame %d online Evaluate() error = %v", index, err)
		}
		historicalDecision, err := engine.Evaluate(context.Background(), historical)
		if err != nil {
			t.Fatalf("frame %d historical Evaluate() error = %v", index, err)
		}
		if !reflect.DeepEqual(onlineDecision, historicalDecision) {
			t.Fatalf("frame %d decision mismatch\nonline=%#v\nhistorical=%#v", index, onlineDecision, historicalDecision)
		}
	}
}

type parityFrame struct {
	closed   marketmodel.IndicatorSnapshot
	window   marketmodel.IndicatorWindowSnapshot
	realtime marketmodel.IndicatorRealtimeSnapshot
}

func parityFixture(interval string, openTime int64, closeTime int64, trend string, price string) parityFrame {
	kline := marketmodel.Kline{
		Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: interval,
		OpenTime: openTime, CloseTime: closeTime, Open: price, High: price, Low: price, Close: price,
		IsClosed: true, EventTime: closeTime,
	}
	closed := marketmodel.IndicatorSnapshot{
		Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: interval,
		OpenTime: openTime, CloseTime: closeTime, UpdatedAt: closeTime,
		Values:  map[string]string{"last_price": price},
		Signals: map[string]string{"trend": trend},
	}
	return parityFrame{
		closed: closed,
		window: marketmodel.IndicatorWindowSnapshot{
			Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: interval,
			OpenTime: openTime, CloseTime: closeTime, UpdatedAt: closeTime, Version: "fixture-v1",
			Values:  map[string]string{"window_sample_count": "20"},
			Signals: map[string]string{"trend_win_latest": trend},
		},
		realtime: marketmodel.IndicatorRealtimeSnapshot{
			Exchange: "binance", Market: "um", Symbol: "ETHUSDT", Interval: interval,
			OpenTime: openTime, CloseTime: closeTime, UpdatedAt: closeTime,
			Kline: kline, Values: closed.Values, Signals: closed.Signals,
		},
	}
}

func applyParityClosed(t *testing.T, store *Store, frame parityFrame) {
	t.Helper()
	envelope := marketbus.NewClosedEnvelope(frame.closed, frame.window, frame.closed.UpdatedAt, time.Hour)
	applied, err := store.Apply(envelope)
	if err != nil || !applied {
		t.Fatalf("Apply(closed %s/%d) applied=%v error=%v", frame.closed.Interval, frame.closed.OpenTime, applied, err)
	}
}

func applyParityRealtime(t *testing.T, store *Store, frame parityFrame) {
	t.Helper()
	envelope := marketbus.NewRealtimeEnvelope(frame.realtime, frame.realtime.UpdatedAt, time.Hour)
	applied, err := store.Apply(envelope)
	if err != nil || !applied {
		t.Fatalf("Apply(realtime %s/%d) applied=%v error=%v", frame.closed.Interval, frame.closed.OpenTime, applied, err)
	}
}

func parityHistoricalContext(target strategy.Target, entry parityFrame, confirm parityFrame) (strategy.Context, error) {
	entryIndicator := strategyframe.IndicatorView(entry.closed)
	entryWindow, err := strategyframe.WindowView(entry.window)
	if err != nil {
		return strategy.Context{}, err
	}
	confirmIndicator := strategyframe.IndicatorView(confirm.closed)
	confirmWindow, err := strategyframe.WindowView(confirm.window)
	if err != nil {
		return strategy.Context{}, err
	}
	price := strategyframe.PriceView(strategyframe.IndicatorView(marketmodel.IndicatorSnapshot{
		Values: entry.realtime.Values,
	}), entry.realtime.Kline)
	return strategyframe.BuildContext(target, map[string]strategy.Snapshot{
		"3m": {
			Target: target, Current: entry.realtime.Kline, Indicator: entryIndicator, Window: entryWindow,
			Price: price, Health: strategy.HealthView{OK: true, UpdatedAt: entry.closed.UpdatedAt},
			Realtime: &strategy.RealtimeView{
				Current: entry.realtime.Kline, Indicator: strategyframe.IndicatorView(marketmodel.IndicatorSnapshot{
					OpenTime: entry.realtime.OpenTime, CloseTime: entry.realtime.CloseTime,
					Values: entry.realtime.Values, Signals: entry.realtime.Signals, UpdatedAt: entry.realtime.UpdatedAt,
				}), Price: price,
			},
			UpdatedAt: entry.closed.UpdatedAt,
		},
		"5m": {
			Target:    strategy.Target{Exchange: target.Exchange, Market: target.Market, Symbol: target.Symbol, Interval: "5m", Scope: target.Scope},
			Indicator: confirmIndicator, Window: confirmWindow,
			Health: strategy.HealthView{OK: true, UpdatedAt: confirm.closed.UpdatedAt}, UpdatedAt: confirm.closed.UpdatedAt,
		},
	}, entry.closed.CloseTime, strategy.TriggerOnEntryClose)
}

type parityStrategy struct{}

func (parityStrategy) Name() string { return "parity" }

func (parityStrategy) Requirements(target strategy.Target) strategy.Requirements {
	return strategy.Requirements{EntryInterval: target.Interval, ConfirmIntervals: []string{"5m"}, Trigger: strategy.TriggerOnEntryClose}
}

func (parityStrategy) Evaluate(ctx context.Context, snapshot strategy.Snapshot, _ *strategy.Position) (strategy.Result, error) {
	if err := ctx.Err(); err != nil {
		return strategy.Result{}, err
	}
	entryTrend := snapshot.Window.Signals["trend"].Latest
	confirmTrend := snapshot.Timeframes["5m"].Window.Signals["trend"].Latest
	side := strategy.SignalSideHold
	if entryTrend == confirmTrend {
		if entryTrend == "bullish" {
			side = strategy.SignalSideBuy
		} else if entryTrend == "bearish" {
			side = strategy.SignalSideSell
		}
	}
	return strategy.Result{
		StrategyName: "parity",
		Signal: strategy.Signal{
			Strategy: "parity", Side: side, Reason: entryTrend + "/" + confirmTrend,
			OpenTime: snapshot.Window.OpenTime, UpdatedAt: snapshot.UpdatedAt,
		},
	}, nil
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
