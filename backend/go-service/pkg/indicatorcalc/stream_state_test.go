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
	for _, period := range []int{5, 7, 8, 9, 10, 12, 13, 19, 21, 25, 26, 34, 55, 89, 99, 144, 200} {
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
		for _, offset := range []int{2, 5} {
			gotHistorical, ok := state.emaHistoricalValue(period, offset)
			wantHistorical, wantOK := ema(fullCloses[:len(fullCloses)-offset], period)
			if ok != wantOK {
				t.Fatalf("ema%d offset %d stream ok = %v, want %v", period, offset, ok, wantOK)
			}
			if ok {
				assertFloatClose(t, "historical ema", gotHistorical, wantHistorical)
			}
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

	gotPSAR, gotPSARDirection, ok := state.psarValue()
	if !ok {
		t.Fatal("missing stream PSAR")
	}
	wantPSAR, wantPSARDirection, ok := psar(fullHighs, fullLows, fullCloses, 0.02, 0.2)
	if !ok {
		t.Fatal("missing batch PSAR")
	}
	assertFloatClose(t, "psar", gotPSAR, wantPSAR)
	if gotPSARDirection != wantPSARDirection {
		t.Fatalf("PSAR direction = %q, want %q", gotPSARDirection, wantPSARDirection)
	}

	gotJaw, gotTeeth, gotLips, ok := state.alligatorValue()
	if !ok {
		t.Fatal("missing stream Alligator")
	}
	wantJaw, wantTeeth, wantLips, ok := alligator(fullCloses)
	if !ok {
		t.Fatal("missing batch Alligator")
	}
	assertFloatClose(t, "alligator jaw", gotJaw, wantJaw)
	assertFloatClose(t, "alligator teeth", gotTeeth, wantTeeth)
	assertFloatClose(t, "alligator lips", gotLips, wantLips)

	gotHMA, ok := state.hma21Value()
	if !ok {
		t.Fatal("missing stream HMA21")
	}
	wantHMA, ok := hma(fullCloses, 21)
	if !ok {
		t.Fatal("missing batch HMA21")
	}
	assertFloatClose(t, "hma21", gotHMA, wantHMA)
	gotPreviousHMA, ok := state.hma21Previous3Value()
	if !ok {
		t.Fatal("missing previous stream HMA21")
	}
	wantPreviousHMA, ok := hma(fullCloses[:len(fullCloses)-3], 21)
	if !ok {
		t.Fatal("missing previous batch HMA21")
	}
	assertFloatClose(t, "previous hma21", gotPreviousHMA, wantPreviousHMA)

	gotAverage, gotPreviousAverage, gotEMD, gotPreviousEMD, ok := state.emd25Value(25)
	if !ok {
		t.Fatal("missing stream EMD25")
	}
	wantAverage, wantPreviousAverage, wantEMD, wantPreviousEMD, ok := emdLastTwo(fullCloses, 25)
	if !ok {
		t.Fatal("missing batch EMD25")
	}
	assertFloatClose(t, "emd average", gotAverage, wantAverage)
	assertFloatClose(t, "emd previous average", gotPreviousAverage, wantPreviousAverage)
	assertFloatClose(t, "emd", gotEMD, wantEMD)
	assertFloatClose(t, "previous emd", gotPreviousEMD, wantPreviousEMD)
}

func TestBasicIndicatorStateEMAStorageAndCloneIndependence(t *testing.T) {
	state := newBasicIndicatorState()
	for index, period := range basicEMAPeriods {
		emaState, ok := state.emaState(period)
		if !ok {
			t.Fatalf("missing ema state for period %d", period)
		}
		if emaState != &state.ema[index] || emaState.period != period {
			t.Fatalf("ema%d mapped to the wrong state", period)
		}
	}
	if _, ok := state.emaState(6); ok {
		t.Fatal("unexpected ema state for unsupported period")
	}

	original, _ := state.emaState(21)
	for value := 1.0; value <= 21; value++ {
		original.append(value)
	}
	cloned := state.clone()
	clonedEMA, ok := cloned.emaState(21)
	if !ok {
		t.Fatal("cloned ema21 state is missing")
	}
	clonedEMA.append(100)
	if original.value == clonedEMA.value {
		t.Fatal("cloned ema state mutated the original state")
	}
}

func TestBasicIndicatorStateDynamicSMAAndMACDStorage(t *testing.T) {
	state := newBasicIndicatorState()
	for index, period := range basicSMAPeriods {
		mapped := findBasicSMAState(state.smaStates, period)
		if mapped != &state.smaStates[index] {
			t.Fatalf("sma%d mapped to the wrong dynamic slot", period)
		}
	}
	if mapped := findBasicSMAState(state.smaStates, 21); mapped != nil {
		t.Fatal("unexpected slot for unsupported SMA period")
	}
	for index, config := range basicMACDConfigs {
		if state.macd[index].config != config {
			t.Fatalf("MACD slot %d config = %+v, want %+v", index, state.macd[index].config, config)
		}
	}

	klines := benchmarkKlines(240)
	window := NewCalculationWindowFromKlines(klines, 0)
	_, highs, lows, closes, volumes, err := window.Series()
	if err != nil {
		t.Fatal(err)
	}
	state = buildBasicIndicatorState(highs, lows, closes, volumes)
	cloned := state.clone()
	cloned.macd[0].state.append(closes[len(closes)-1] + 10)
	if len(cloned.macd[0].state.series) == 0 || len(state.macd[0].state.series) == 0 {
		t.Fatal("missing MACD series")
	}
	cloned.macd[0].state.series[0].value++
	if cloned.macd[0].state.series[0].value == state.macd[0].state.series[0].value {
		t.Fatal("cloned MACD series mutated the original state")
	}
}

func TestStreamPSARMatchesBatchAtEveryPoint(t *testing.T) {
	klines := benchmarkKlines(300)
	window := NewCalculationWindowFromKlines(klines, 0)
	_, highs, lows, closes, _, err := window.Series()
	if err != nil {
		t.Fatal(err)
	}
	state := newStreamPSARState(0.02, 0.2)
	for index := range closes {
		state.append(highs[index], lows[index], closes[index])
		if index < 2 {
			continue
		}
		got, gotDirection, gotOK := state.value()
		want, wantDirection, wantOK := psar(highs[:index+1], lows[:index+1], closes[:index+1], 0.02, 0.2)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		assertFloatClose(t, "stream psar", got, want)
		if gotDirection != wantDirection {
			t.Fatalf("index=%d direction = %q, want %q", index, gotDirection, wantDirection)
		}
	}
}

func TestStreamKAMA10MatchesBatchAtEveryPoint(t *testing.T) {
	klines := benchmarkKlines(300)
	window := NewCalculationWindowFromKlines(klines, 0)
	_, _, _, closes, _, err := window.Series()
	if err != nil {
		t.Fatal(err)
	}
	var state streamKAMAState
	for index, closeValue := range closes {
		state.append(closeValue)
		got, gotOK := state.value()
		want, wantOK := kama(closes[:index+1], 10, 2, 30)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream kama10", got, want)
		}
	}
}

func TestStreamDEMATEMA21MatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	first := newStreamEMAState(21)
	state := newStreamDEMATEMAState(21)
	for index, closeValue := range closes {
		first.append(closeValue)
		state.append(first)
		gotDEMA, gotTEMA, gotDEMAOK, gotTEMAOK := state.value(first)
		wantDEMA, wantTEMA, wantDEMAOK, wantTEMAOK := demaTema(closes[:index+1], 21)
		if gotDEMAOK != wantDEMAOK || gotTEMAOK != wantTEMAOK {
			t.Fatalf("index=%d stream ok = %v/%v, want %v/%v", index, gotDEMAOK, gotTEMAOK, wantDEMAOK, wantTEMAOK)
		}
		if gotDEMAOK {
			assertFloatClose(t, "stream dema21", gotDEMA, wantDEMA)
		}
		if gotTEMAOK {
			assertFloatClose(t, "stream tema21", gotTEMA, wantTEMA)
		}
	}
}

func TestStreamEMAClonePreservesHistory(t *testing.T) {
	closes := oscillatingCloses(80)
	state := newStreamEMAState(13)
	for _, closeValue := range closes[:70] {
		state.append(closeValue)
	}
	cloned := state.clone()
	for _, closeValue := range closes[70:] {
		state.append(closeValue)
		cloned.append(closeValue)
	}
	assertFloatClose(t, "cloned ema", cloned.value, state.value)
	for _, offset := range []int{1, 2, 5} {
		got, gotOK := cloned.historicalValue(offset)
		want, wantOK := state.historicalValue(offset)
		if gotOK != wantOK {
			t.Fatalf("offset=%d cloned ok = %v, want %v", offset, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "cloned historical ema", got, want)
		}
	}
}

func TestStreamAlligatorMatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	jaw := newStreamSMMAState(13)
	teeth := newStreamSMMAState(8)
	lips := newStreamSMMAState(5)
	for index, closeValue := range closes {
		jaw.append(closeValue)
		teeth.append(closeValue)
		lips.append(closeValue)
		gotJaw, jawOK := jaw.value()
		gotTeeth, teethOK := teeth.value()
		gotLips, lipsOK := lips.value()
		gotOK := jawOK && teethOK && lipsOK
		wantJaw, wantTeeth, wantLips, wantOK := alligator(closes[:index+1])
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream alligator jaw", gotJaw, wantJaw)
			assertFloatClose(t, "stream alligator teeth", gotTeeth, wantTeeth)
			assertFloatClose(t, "stream alligator lips", gotLips, wantLips)
		}
	}
}

func TestStreamHMA21MatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	var state streamHMA21State
	for index, closeValue := range closes {
		state.append(closeValue)
		got, gotOK := state.value()
		want, wantOK := hma(closes[:index+1], 21)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream hma21", got, want)
		}
		gotPrevious, gotPreviousOK := state.previous3()
		wantPrevious, wantPreviousOK := 0.0, false
		if index >= 3 {
			wantPrevious, wantPreviousOK = hma(closes[:index-2], 21)
		}
		if gotPreviousOK != wantPreviousOK {
			t.Fatalf("index=%d previous stream ok = %v, want %v", index, gotPreviousOK, wantPreviousOK)
		}
		if gotPreviousOK {
			assertFloatClose(t, "stream previous hma21", gotPrevious, wantPrevious)
		}
	}
}

func TestStreamEMD25MatchesBatchAtEveryPoint(t *testing.T) {
	closes := oscillatingCloses(300)
	state := newStreamEMDState(25)
	for index, closeValue := range closes {
		state.append(closeValue)
		gotAvg, gotPreviousAvg, gotEMD, gotPreviousEMD, gotOK := state.value()
		wantAvg, wantPreviousAvg, wantEMD, wantPreviousEMD, wantOK := emdLastTwo(closes[:index+1], 25)
		if gotOK != wantOK {
			t.Fatalf("index=%d stream ok = %v, want %v", index, gotOK, wantOK)
		}
		if gotOK {
			assertFloatClose(t, "stream emd average", gotAvg, wantAvg)
			assertFloatClose(t, "stream emd previous average", gotPreviousAvg, wantPreviousAvg)
			assertFloatClose(t, "stream emd", gotEMD, wantEMD)
			assertFloatClose(t, "stream previous emd", gotPreviousEMD, wantPreviousEMD)
		}
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
