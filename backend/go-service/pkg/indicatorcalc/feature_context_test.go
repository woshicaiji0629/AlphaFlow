package indicatorcalc

import (
	"testing"
)

func TestFeatureContextCachesATRSeriesByPeriod(t *testing.T) {
	highs, lows, closes := testPriceSeries(120)
	ctx := newFeatureContext(highs, lows, closes, nil)
	first, ok := ctx.atrSeries(14)
	if !ok || len(first) == 0 {
		t.Fatal("first ATR series unavailable")
	}
	second, ok := ctx.atrSeries(14)
	if !ok || len(second) != len(first) {
		t.Fatal("cached ATR series unavailable")
	}
	if &first[0] != &second[0] {
		t.Fatal("ATR series was recalculated instead of reused")
	}
	want, ok := atrSeries(highs, lows, closes, 14)
	if !ok || len(want) != len(first) {
		t.Fatal("reference ATR series unavailable")
	}
	for index := range want {
		if first[index] != want[index] {
			t.Fatalf("ATR[%d] = %v, want %v", index, first[index], want[index])
		}
	}
}

func TestFeatureContextEMAValueMatchesExistingCalculation(t *testing.T) {
	_, _, closes := testPriceSeries(120)
	ctx := newFeatureContext(nil, nil, closes, nil)
	for _, period := range []int{7, 20, 25, 99} {
		got, gotOK := ctx.emaValue(period)
		want, wantOK := ema(closes, period)
		if gotOK != wantOK || got != want {
			t.Fatalf("EMA(%d) = %v,%v want %v,%v", period, got, gotOK, want, wantOK)
		}
	}
}

func TestFeatureContextWindowStatisticsMatchExistingCalculation(t *testing.T) {
	highs, lows, closes := testPriceSeries(120)
	ctx := newFeatureContext(highs, lows, closes, nil)
	upper, middle, lower, ok := ctx.bollinger(20, 2)
	wantUpper, wantMiddle, wantLower, wantOK := bollinger(closes, 20, 2)
	if ok != wantOK || upper != wantUpper || middle != wantMiddle || lower != wantLower {
		t.Fatal("cached Bollinger differs from existing calculation")
	}
	high, low, ok := ctx.donchian(20)
	wantHigh, wantLow, wantOK := donchian(highs, lows, 20)
	if ok != wantOK || high != wantHigh || low != wantLow {
		t.Fatal("cached Donchian differs from existing calculation")
	}
}

func BenchmarkATRSeriesShared10Consumers(b *testing.B) {
	highs, lows, closes := testPriceSeries(300)
	b.ReportAllocs()
	for range b.N {
		ctx := newFeatureContext(highs, lows, closes, nil)
		for range 10 {
			if _, ok := ctx.atrSeries(14); !ok {
				b.Fatal("ATR unavailable")
			}
		}
	}
}

func BenchmarkATRSeriesRepeated10Consumers(b *testing.B) {
	highs, lows, closes := testPriceSeries(300)
	b.ReportAllocs()
	for range b.N {
		for range 10 {
			if _, ok := atrSeries(highs, lows, closes, 14); !ok {
				b.Fatal("ATR unavailable")
			}
		}
	}
}

func testPriceSeries(count int) ([]float64, []float64, []float64) {
	highs := make([]float64, count)
	lows := make([]float64, count)
	closes := make([]float64, count)
	for index := 0; index < count; index++ {
		closes[index] = 100 + float64(index%17)
		highs[index] = closes[index] + 2
		lows[index] = closes[index] - 2
	}
	return highs, lows, closes
}
