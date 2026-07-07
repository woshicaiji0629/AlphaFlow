package indicatorcalc

import (
	"math"
	"testing"
)

func TestBasicIndicatorStateMatchesBatchCalculations(t *testing.T) {
	klines := benchmarkKlines(140)
	window := NewCalculationWindowFromKlines(klines[:100], 200)
	window.EnableBasicState()
	window.Append(klines[100:])
	_, highs, lows, closes, volumes, err := window.Series()
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	state := window.basic
	if state == nil {
		t.Fatal("missing basic indicator state")
	}

	for _, period := range []int{7, 20, 25, 99} {
		got, ok := state.sma(period)
		if !ok {
			t.Fatalf("missing stream sma%d", period)
		}
		want, ok := sma(closes, period)
		if !ok {
			t.Fatalf("missing batch sma%d", period)
		}
		assertFloatClose(t, "sma", got, want)
	}
	for _, period := range []int{7, 12, 19, 25, 26, 99} {
		got, ok := state.emaValue(period)
		if !ok {
			t.Fatalf("missing stream ema%d", period)
		}
		want, ok := ema(closes, period)
		if !ok {
			t.Fatalf("missing batch ema%d", period)
		}
		assertFloatClose(t, "ema", got, want)
	}

	gotRSI, ok := state.rsiSeries14()
	if !ok {
		t.Fatal("missing stream rsi14 series")
	}
	wantRSI, ok := rsiSeries(closes, 14)
	if !ok {
		t.Fatal("missing batch rsi14 series")
	}
	assertFloatSeriesClose(t, "rsi14", gotRSI, wantRSI)

	gotATR, ok := state.atrSeries14()
	if !ok {
		t.Fatal("missing stream atr14 series")
	}
	wantATR, ok := atrSeries(highs, lows, closes, 14)
	if !ok {
		t.Fatal("missing batch atr14 series")
	}
	assertFloatSeriesClose(t, "atr14", gotATR, wantATR)

	for _, config := range []macdConfig{{fast: 12, slow: 26, signal: 9}, {fast: 7, slow: 19, signal: 9}} {
		gotMACD, ok := state.macdSeries(config)
		if !ok {
			t.Fatalf("missing stream macd %#v", config)
		}
		wantMACD, ok := macdSeries(closes, config.fast, config.slow, config.signal)
		if !ok {
			t.Fatalf("missing batch macd %#v", config)
		}
		if len(gotMACD) != len(wantMACD) {
			t.Fatalf("macd len = %d, want %d", len(gotMACD), len(wantMACD))
		}
		for index := range gotMACD {
			assertFloatClose(t, "macd value", gotMACD[index].value, wantMACD[index].value)
			assertFloatClose(t, "macd signal", gotMACD[index].signal, wantMACD[index].signal)
			assertFloatClose(t, "macd hist", gotMACD[index].hist, wantMACD[index].hist)
		}
	}

	gotOBV, ok := state.obvValue()
	if !ok {
		t.Fatal("missing stream obv")
	}
	assertFloatClose(t, "obv", gotOBV, obv(closes, volumes))

	gotVWAP, ok := state.vwapValue(closes[len(closes)-1])
	if !ok {
		t.Fatal("missing stream vwap")
	}
	assertFloatClose(t, "vwap", gotVWAP, vwap(highs, lows, closes, volumes))

	gotVolumeMA, ok := state.volumeSMAValue(20)
	if !ok {
		t.Fatal("missing stream volume sma20")
	}
	wantVolumeMA, ok := sma(volumes, 20)
	if !ok {
		t.Fatal("missing batch volume sma20")
	}
	assertFloatClose(t, "volume sma20", gotVolumeMA, wantVolumeMA)
}

func assertFloatSeriesClose(t *testing.T, name string, got []float64, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d", name, len(got), len(want))
	}
	for index := range got {
		assertFloatClose(t, name, got[index], want[index])
	}
}

func assertFloatClose(t *testing.T, name string, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.00000001 {
		t.Fatalf("%s = %.12f, want %.12f", name, got, want)
	}
}
