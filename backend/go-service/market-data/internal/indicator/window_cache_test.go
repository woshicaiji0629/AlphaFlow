package indicator

import (
	"context"
	"reflect"
	"testing"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
)

var benchmarkRealtimeWindowResult indicatorcalc.Result
var benchmarkClosedSnapshotsResult []model.IndicatorSnapshot

func TestRealtimeWindowForKlineMatchesIsolatedPreview(t *testing.T) {
	klines := minuteKlines(250)
	open := minuteKline(int64(len(klines)), 350)
	open.IsClosed = false
	rule := Rule{Exchange: "binance", Market: "um"}
	runner := NewRunner(&fakeStore{}, RunnerOptions{LookbackPeriods: 250})
	key := windowKey(rule.Exchange, rule.Market, open.Symbol, open.Interval)
	cached := newCalculationWindowFromKlines(klines, 250)
	runner.windowShard(key).windows[key] = cached

	window, ready, err := runner.realtimeWindowForKline(context.Background(), rule, open, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("realtime window is not ready")
	}
	legacyCached := newCalculationWindowFromKlines(klines, 250)
	legacyCached.PrepareAISourcePrefix()
	legacyWindow := windowWithTemporaryKline(legacyCached.Clone(), open, 250)
	got, err := indicatorcalc.CalculateWindow(window, indicatorcalc.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want, err := indicatorcalc.CalculateWindow(legacyWindow, indicatorcalc.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("realtime result differs from isolated preview")
	}
	cachedKlines := runner.cachedWindow(key).Klines()
	if len(cachedKlines) != len(klines) || cachedKlines[len(cachedKlines)-1].OpenTime != klines[len(klines)-1].OpenTime {
		t.Fatalf("cached window was mutated: %#v", cachedKlines)
	}
}

func BenchmarkWindowWithTemporaryKlineRealtime(b *testing.B) {
	klines := minuteKlines(250)
	window := newCalculationWindowFromKlines(klines, 250)
	open := minuteKline(int64(len(klines)), 350)
	open.IsClosed = false
	options := indicatorcalc.DefaultOptions()

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		calcWindow := windowWithTemporaryKline(window, open, 250)
		result, err := indicatorcalc.CalculateWindow(calcWindow, options)
		if err != nil {
			b.Fatalf("CalculateWindow: %v", err)
		}
		benchmarkRealtimeWindowResult = result
	}
}

func BenchmarkCachedCloneWithTemporaryKlineRealtime(b *testing.B) {
	cached := newCalculationWindowFromKlines(minuteKlines(250), 250)
	cached.PrepareAISourcePrefix()
	open := minuteKline(250, 350)
	open.IsClosed = false
	options := indicatorcalc.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		window := cached.Clone()
		calcWindow := windowWithTemporaryKline(window, open, 250)
		result, err := indicatorcalc.CalculateWindow(calcWindow, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRealtimeWindowResult = result
	}
}

func BenchmarkCachedRealtimePreview(b *testing.B) {
	cached := newCalculationWindowFromKlines(minuteKlines(250), 250)
	open := minuteKline(250, 350)
	open.IsClosed = false
	rule := Rule{Exchange: "binance", Market: "um"}
	runner := NewRunner(&fakeStore{}, RunnerOptions{LookbackPeriods: 250})
	key := windowKey(rule.Exchange, rule.Market, open.Symbol, open.Interval)
	runner.windowShard(key).windows[key] = cached
	options := indicatorcalc.DefaultOptions()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		window, ready, err := runner.realtimeWindowForKline(ctx, rule, open, 60*1000)
		if err != nil {
			b.Fatal(err)
		}
		if !ready {
			b.Fatal("realtime window is not ready")
		}
		result, err := indicatorcalc.CalculateWindow(window, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRealtimeWindowResult = result
	}
}

func BenchmarkClosedWindowAppendAndCalculate(b *testing.B) {
	window := newCalculationWindowFromKlines(minuteKlines(250), 250)
	options := indicatorcalc.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		window.Append([]model.Kline{minuteKline(int64(250+index), 350+float64(index%50))})
		result, err := indicatorcalc.CalculateWindow(window, options)
		if err != nil {
			b.Fatalf("CalculateWindow: %v", err)
		}
		benchmarkRealtimeWindowResult = result
	}
}

func BenchmarkCalculateOnlyMissingLatestSnapshot(b *testing.B) {
	klines := minuteKlines(250)
	window := newCalculationWindowFromKlines(klines, 250)
	runner := NewRunner(&fakeStore{}, RunnerOptions{})
	cached := make([]model.IndicatorSnapshot, 0, 19)
	for _, kline := range klines[len(klines)-20 : len(klines)-1] {
		cached = append(cached, testIndicatorSnapshot(kline, "cached"))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		snapshots, err := runner.calculatedIndicatorSnapshotsForWindow(window, cached)
		if err != nil {
			b.Fatalf("calculatedIndicatorSnapshotsForWindow: %v", err)
		}
		benchmarkClosedSnapshotsResult = snapshots
	}
}
