package simulator

import (
	"context"
	"testing"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func TestSnapshotBuilderUsesLatestClosedConfirmInterval(t *testing.T) {
	const minute = int64(60_000)
	dataset := reader.Dataset{
		Series: []reader.SeriesResult{
			{
				Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "1m"},
				Result: reader.Result{
					Klines:         testKlines("ETHUSDT", "1m", []int64{0, minute, 2 * minute, 3 * minute, 4 * minute, 5 * minute, 6 * minute, 7 * minute}),
					RequestedStart: 6 * minute,
					EffectiveStart: 0,
					End:            8 * minute,
					WarmupBars:     6,
				},
			},
			{
				Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "5m"},
				Result: reader.Result{
					Klines:         testKlines("ETHUSDT", "5m", []int64{0, 5 * minute, 10 * minute}),
					RequestedStart: 6 * minute,
					EffectiveStart: 0,
					End:            8 * minute,
					WarmupBars:     2,
				},
			},
		},
	}
	builder, err := NewSnapshotBuilder(SnapshotBuilderOptions{
		Dataset: dataset,
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Scope:    strategy.PositionScopeBacktest,
			RunID:    "run-1",
		},
		Interval:         "1m",
		ConfirmIntervals: []string{"5m"},
	})
	if err != nil {
		t.Fatalf("NewSnapshotBuilder() error = %v", err)
	}

	contexts, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("contexts len = %d, want 2", len(contexts))
	}
	first := contexts[0]
	entry := first.Snapshots["1m"]
	if entry.Current.OpenTime != 6*minute {
		t.Fatalf("entry current open time = %d, want %d", entry.Current.OpenTime, 6*minute)
	}
	confirm := first.Snapshots["5m"]
	if confirm.Indicator.OpenTime != 0 {
		t.Fatalf("confirm indicator open time = %d, want latest closed %d", confirm.Indicator.OpenTime, 0)
	}
	if confirm.Current.Close != "" {
		t.Fatalf("confirm current close = %q, want empty", confirm.Current.Close)
	}
	if entry.Timeframes["5m"].Indicator.OpenTime != 0 {
		t.Fatalf("entry timeframe confirm open time = %d, want %d", entry.Timeframes["5m"].Indicator.OpenTime, 0)
	}
	if entry.AsOf != 7*minute-1 || entry.Trigger != strategy.TriggerOnEntryClose {
		t.Fatalf("entry timing = as_of %d trigger %q", entry.AsOf, entry.Trigger)
	}
	if entry.Window.SampleCount == 0 {
		t.Fatal("entry window sample count = 0, want analyzer sample count")
	}
	if first.Target.Interval != "1m" {
		t.Fatalf("context target interval = %q, want 1m", first.Target.Interval)
	}
}

func TestSnapshotBuilderRejectsMissingSeries(t *testing.T) {
	builder, err := NewSnapshotBuilder(SnapshotBuilderOptions{
		Dataset: reader.Dataset{},
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
		},
		Interval: "1m",
	})
	if err != nil {
		t.Fatalf("NewSnapshotBuilder() error = %v", err)
	}

	_, err = builder.Build(context.Background())
	if err == nil {
		t.Fatal("Build() error = nil, want missing series error")
	}
}

func TestSnapshotBuilderCalculatesEachClosedKlineOncePerReplay(t *testing.T) {
	original := calculateIndicatorWindow
	t.Cleanup(func() { calculateIndicatorWindow = original })
	calls := 0
	maxWindow := 0
	calculateIndicatorWindow = func(window *indicatorcalc.CalculationWindow, options indicatorcalc.Options) (indicatorcalc.Result, error) {
		calls++
		if len(window.Klines()) > maxWindow {
			maxWindow = len(window.Klines())
		}
		return original(window, options)
	}
	const minute = int64(60_000)
	dataset := reader.Dataset{Series: []reader.SeriesResult{
		{Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "1m"}, Result: reader.Result{
			Klines: testKlines("ETHUSDT", "1m", []int64{0, minute, 2 * minute, 3 * minute}), RequestedStart: 2 * minute, End: 4 * minute,
		}},
		{Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "5m"}, Result: reader.Result{
			Klines: testKlines("ETHUSDT", "5m", []int64{-5 * minute, 0}), RequestedStart: 2 * minute, End: 4 * minute,
		}},
	}}
	builder, err := NewSnapshotBuilder(SnapshotBuilderOptions{
		Dataset:  dataset,
		Target:   strategy.Target{Exchange: "binance", Market: "um", Symbol: "ETHUSDT"},
		Interval: "1m", ConfirmIntervals: []string{"5m"}, CalculationWindow: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 2 {
		t.Fatalf("contexts = %d, want 2", len(contexts))
	}
	if calls != 5 {
		t.Fatalf("CalculateWindow calls = %d, want each of 5 klines closed by the final as-of once", calls)
	}
	if maxWindow != 3 {
		t.Fatalf("maximum calculation window = %d, want 3", maxWindow)
	}
}

func testKlines(symbol string, interval string, openTimes []int64) []marketmodel.Kline {
	intervalMillis, err := marketmodel.IntervalMillis(interval)
	if err != nil {
		panic(err)
	}
	klines := make([]marketmodel.Kline, 0, len(openTimes))
	for _, openTime := range openTimes {
		klines = append(klines, marketmodel.Kline{
			Exchange:    "binance",
			Market:      "um",
			Symbol:      symbol,
			Interval:    interval,
			OpenTime:    openTime,
			CloseTime:   openTime + intervalMillis - 1,
			Open:        "100",
			High:        "110",
			Low:         "90",
			Close:       "105",
			Volume:      "10",
			QuoteVolume: "1050",
			IsClosed:    true,
		})
	}
	return klines
}
