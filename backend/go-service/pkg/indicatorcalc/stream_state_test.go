package indicatorcalc

import (
	"math"
	"testing"
)

func TestBasicIndicatorStateMatchesBatchCalculations(t *testing.T) {
	klines := benchmarkKlines(240)
	window := NewCalculationWindowFromKlines(klines[:100], 200)
	window.EnableBasicState()
	window.Append(klines[100:])
	_, _, _, closes, volumes, err := window.Series()
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	state := window.basic
	if state == nil {
		t.Fatal("missing basic indicator state")
	}
	fullWindow := NewCalculationWindowFromKlines(klines, 0)
	_, fullHighs, fullLows, fullCloses, fullVolumes, err := fullWindow.Series()
	if err != nil {
		t.Fatal(err)
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
	for _, period := range []int{5, 7, 8, 9, 10, 12, 13, 19, 25, 26, 34, 55, 89, 99, 144, 200} {
		got, ok := state.emaValue(period)
		if !ok {
			t.Fatalf("missing stream ema%d", period)
		}
		want, ok := ema(fullCloses, period)
		if !ok {
			t.Fatalf("missing batch ema%d", period)
		}
		assertFloatClose(t, "ema", got, want)
		if len(fullCloses) > period {
			gotPrevious, ok := state.previousEMAValue(period)
			if !ok {
				t.Fatalf("missing stream previous ema%d", period)
			}
			wantPrevious, ok := ema(fullCloses[:len(fullCloses)-1], period)
			if !ok {
				t.Fatalf("missing batch previous ema%d", period)
			}
			assertFloatClose(t, "previous ema", gotPrevious, wantPrevious)
		}
	}

	gotRSI, ok := state.rsiSeries14()
	if !ok {
		t.Fatal("missing stream rsi14 series")
	}
	wantRSI, ok := rsiSeries(fullCloses, 14)
	if !ok {
		t.Fatal("missing batch rsi14 series")
	}
	assertFloatSeriesClose(t, "rsi14", gotRSI, wantRSI[len(wantRSI)-len(gotRSI):])

	gotATR, ok := state.atrSeries14()
	if !ok {
		t.Fatal("missing stream atr14 series")
	}
	wantATR, ok := atrSeries(fullHighs, fullLows, fullCloses, 14)
	if !ok {
		t.Fatal("missing batch atr14 series")
	}
	assertFloatSeriesClose(t, "atr14", gotATR, wantATR[len(wantATR)-len(gotATR):])

	gotADX, gotPlusDI, gotMinusDI, ok := state.adx14Value()
	if !ok {
		t.Fatal("missing stream adx14")
	}
	wantADX, wantPlusDI, wantMinusDI, ok := adx(fullHighs, fullLows, fullCloses, 14)
	if !ok {
		t.Fatal("missing batch adx14")
	}
	assertFloatClose(t, "adx14", gotADX, wantADX)
	assertFloatClose(t, "di plus14", gotPlusDI, wantPlusDI)
	assertFloatClose(t, "di minus14", gotMinusDI, wantMinusDI)

	gotWT1, gotWT2, gotPreviousWT1, gotPreviousWT2, gotPreviousDelta, ok := state.waveTrendValue()
	if !ok {
		t.Fatal("missing stream wavetrend")
	}
	wantWT1, wantWT2, wantPreviousWT1, wantPreviousWT2, wantPreviousDelta, ok := waveTrend(fullHighs, fullLows, fullCloses, 10, 21)
	if !ok {
		t.Fatal("missing batch wavetrend")
	}
	assertFloatClose(t, "wavetrend wt1", gotWT1, wantWT1)
	assertFloatClose(t, "wavetrend wt2", gotWT2, wantWT2)
	assertFloatClose(t, "wavetrend previous wt1", gotPreviousWT1, wantPreviousWT1)
	assertFloatClose(t, "wavetrend previous wt2", gotPreviousWT2, wantPreviousWT2)
	assertFloatClose(t, "wavetrend previous delta", gotPreviousDelta, wantPreviousDelta)

	for _, config := range []macdConfig{{fast: 12, slow: 26, signal: 9}, {fast: 7, slow: 19, signal: 9}} {
		gotMACD, ok := state.macdSeries(config)
		if !ok {
			t.Fatalf("missing stream macd %#v", config)
		}
		wantMACD, ok := macdSeries(fullCloses, config.fast, config.slow, config.signal)
		if !ok {
			t.Fatalf("missing batch macd %#v", config)
		}
		wantMACD = wantMACD[len(wantMACD)-len(gotMACD):]
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
	assertFloatClose(t, "obv", gotOBV, obv(fullCloses, fullVolumes))

	_, gotOBVSlope, gotPVT, gotPVTSlope, gotADLine, gotADLineSlope, ok := state.moneyFlowValues()
	if !ok {
		t.Fatal("missing stream money flow")
	}
	wantOBVSeries := obvSeries(fullCloses, fullVolumes)
	wantPVTSeries := priceVolumeTrendSeries(fullCloses, fullVolumes)
	wantADSeries := accumulationDistributionSeries(fullHighs, fullLows, fullCloses, fullVolumes)
	assertFloatClose(t, "money flow obv slope", gotOBVSlope, slope(wantOBVSeries, 5))
	assertFloatClose(t, "money flow pvt", gotPVT, wantPVTSeries[len(wantPVTSeries)-1])
	assertFloatClose(t, "money flow pvt slope", gotPVTSlope, slope(wantPVTSeries, 5))
	assertFloatClose(t, "money flow ad line", gotADLine, wantADSeries[len(wantADSeries)-1])
	assertFloatClose(t, "money flow ad line slope", gotADLineSlope, slope(wantADSeries, 5))

	gotVWAP, ok := state.vwapValue(closes[len(closes)-1])
	if !ok {
		t.Fatal("missing stream vwap")
	}
	assertFloatClose(t, "vwap", gotVWAP, vwap(fullHighs, fullLows, fullCloses, fullVolumes))

	gotVolumeMA, ok := state.volumeSMAValue(20)
	if !ok {
		t.Fatal("missing stream volume sma20")
	}
	wantVolumeMA, ok := sma(volumes, 20)
	if !ok {
		t.Fatal("missing batch volume sma20")
	}
	assertFloatClose(t, "volume sma20", gotVolumeMA, wantVolumeMA)

	gotAdaptive, ok := state.adaptiveSupertrendValue()
	if !ok {
		t.Fatal("missing stream adaptive supertrend")
	}
	wantAdaptive, ok := adaptiveSupertrend(fullHighs, fullLows, fullCloses, 10, 3, 100)
	if !ok {
		t.Fatal("missing batch adaptive supertrend")
	}
	gotLast := gotAdaptive.points[len(gotAdaptive.points)-1]
	wantLast := wantAdaptive.points[len(wantAdaptive.points)-1]
	assertFloatClose(t, "adaptive supertrend", gotLast.value, wantLast.value)
	assertFloatClose(t, "adaptive assigned atr", gotAdaptive.assignedATR, wantAdaptive.assignedATR)
	if gotLast.direction != wantLast.direction || gotAdaptive.cluster != wantAdaptive.cluster {
		t.Fatalf("adaptive state = %s/%s, want %s/%s", gotLast.direction, gotAdaptive.cluster, wantLast.direction, wantAdaptive.cluster)
	}
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
