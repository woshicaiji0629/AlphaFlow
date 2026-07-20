package indicatorcalc

import (
	"math"
	"reflect"
	"testing"
)

func TestSupertrendCompactMatchesReference(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	for _, item := range []struct {
		period     int
		multiplier float64
	}{{7, 2}, {10, 3}, {10, 3.3}, {14, 4}} {
		got, gotOK := supertrendSeries(highs, lows, closes, item.period, item.multiplier)
		want, wantOK := supertrendSeriesReference(highs, lows, closes, item.period, item.multiplier)
		if gotOK != wantOK || !reflect.DeepEqual(got, want) {
			t.Fatalf("period=%d multiplier=%v compact result differs", item.period, item.multiplier)
		}
	}
}

func TestSupertrendSummaryMatchesFullSeries(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	trueRanges := trueRangeSeries(highs, lows, closes)
	for _, simpleATR := range []bool{false, true} {
		points, pointsOK := supertrendSeriesFromTrueRanges(highs, lows, closes, trueRanges, 10, 3, simpleATR)
		previous, last, summaryOK := supertrendSummaryFromTrueRanges(highs, lows, closes, trueRanges, 10, 3, simpleATR)
		if summaryOK != pointsOK {
			t.Fatalf("simpleATR=%v summary ok = %v, want %v", simpleATR, summaryOK, pointsOK)
		}
		if !summaryOK {
			continue
		}
		if previous != points[len(points)-2] || last != points[len(points)-1] {
			t.Fatalf("simpleATR=%v summary = %#v/%#v, want %#v/%#v", simpleATR, previous, last, points[len(points)-2], points[len(points)-1])
		}
	}
}

func TestSupertrendSummaryRejectsInsufficientInput(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(11, 100, 0.8)
	if _, _, ok := supertrendSummaryFromTrueRanges(highs, lows, closes, trueRangeSeries(highs, lows, closes), 10, 3, true); ok {
		t.Fatal("summary unexpectedly accepted fewer than two points")
	}
}

func TestSupertrendZoneWithATRMatchesFallback(t *testing.T) {
	highs := []float64{105, 103, 101, 99, 104, 110, 118, 116}
	lows := []float64{100, 98, 94, 92, 96, 103, 109, 108}
	closes := []float64{102, 100, 96, 94, 102, 108, 114, 110}
	points := []trendPoint{
		{value: 103, direction: trendDirectionDown},
		{value: 101, direction: trendDirectionDown},
		{value: 97, direction: trendDirectionUp},
		{value: 100, direction: trendDirectionUp},
		{value: 105, direction: trendDirectionUp},
		{value: 112, direction: trendDirectionDown},
	}
	atrValue, ok := atr(highs, lows, closes, 3)
	if !ok {
		t.Fatal("ATR unavailable")
	}
	want, wantOK := supertrendZone(highs, lows, closes, points, 2, 3, 1.5)
	got, gotOK := supertrendZoneWithATR(highs, lows, closes, points, 2, 3, 1.5, atrValue, true)
	if !wantOK {
		t.Fatal("fallback zone unavailable")
	}
	if gotOK != wantOK || got != want {
		t.Fatalf("cached zone = %#v/%v, want %#v/%v", got, gotOK, want, wantOK)
	}
}

func BenchmarkSupertrendCompact300(b *testing.B) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	b.ReportAllocs()
	for range b.N {
		if _, ok := supertrendSeries(highs, lows, closes, 10, 3); !ok {
			b.Fatal("supertrend unavailable")
		}
	}
}

func BenchmarkSupertrendReference300(b *testing.B) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	b.ReportAllocs()
	for range b.N {
		if _, ok := supertrendSeriesReference(highs, lows, closes, 10, 3); !ok {
			b.Fatal("supertrend unavailable")
		}
	}
}

func TestSupertrendWithATRCompactMatchesReference(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	atrValues, ok := atrSeries(highs, lows, closes, 10)
	if !ok {
		t.Fatal("ATR unavailable")
	}
	assigned := make([]float64, len(closes))
	start := len(closes) - len(atrValues)
	copy(assigned[start:], atrValues)
	got, gotOK := supertrendSeriesWithATR(highs, lows, closes, assigned, start, 3)
	want, wantOK := supertrendSeriesWithATRReference(highs, lows, closes, assigned, start, 3)
	if gotOK != wantOK || !reflect.DeepEqual(got, want) {
		t.Fatal("compact ATR result differs")
	}
}

func BenchmarkSupertrendWithATRCompact300(b *testing.B) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	assigned, start := testAssignedATR(highs, lows, closes, 10)
	b.ReportAllocs()
	for range b.N {
		if _, ok := supertrendSeriesWithATR(highs, lows, closes, assigned, start, 3); !ok {
			b.Fatal("supertrend unavailable")
		}
	}
}

func BenchmarkSupertrendWithATRReference300(b *testing.B) {
	highs, lows, closes, _ := trendingSeries(300, 100, 0.8)
	assigned, start := testAssignedATR(highs, lows, closes, 10)
	b.ReportAllocs()
	for range b.N {
		if _, ok := supertrendSeriesWithATRReference(highs, lows, closes, assigned, start, 3); !ok {
			b.Fatal("supertrend unavailable")
		}
	}
}

func testAssignedATR(highs []float64, lows []float64, closes []float64, period int) ([]float64, int) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok {
		panic("ATR unavailable")
	}
	assigned := make([]float64, len(closes))
	start := len(closes) - len(atrValues)
	copy(assigned[start:], atrValues)
	return assigned, start
}

func supertrendSeriesWithATRReference(highs []float64, lows []float64, closes []float64, atrValues []float64, start int, multiplier float64) ([]trendPoint, bool) {
	if start <= 0 || start >= len(closes) || len(atrValues) != len(closes) {
		return nil, false
	}
	finalUpper := make([]float64, len(closes))
	finalLower := make([]float64, len(closes))
	direction := make([]string, len(closes))
	for index := start; index < len(closes); index++ {
		if atrValues[index] <= 0 {
			return nil, false
		}
		mid := (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		if index == start {
			finalUpper[index], finalLower[index] = basicUpper, basicLower
			if closes[index] >= mid {
				direction[index] = "up"
			} else {
				direction[index] = "down"
			}
			continue
		}
		if basicUpper < finalUpper[index-1] || closes[index-1] > finalUpper[index-1] {
			finalUpper[index] = basicUpper
		} else {
			finalUpper[index] = finalUpper[index-1]
		}
		if basicLower > finalLower[index-1] || closes[index-1] < finalLower[index-1] {
			finalLower[index] = basicLower
		} else {
			finalLower[index] = finalLower[index-1]
		}
		direction[index] = direction[index-1]
		if direction[index-1] == "down" && closes[index] > finalUpper[index] {
			direction[index] = "up"
		} else if direction[index-1] == "up" && closes[index] < finalLower[index] {
			direction[index] = "down"
		}
	}
	points := make([]trendPoint, 0, len(closes)-start)
	for index := start; index < len(closes); index++ {
		points = append(points, supertrendPoint(finalUpper[index], finalLower[index], direction[index]))
	}
	return points, len(points) >= 2
}

func supertrendSeriesReference(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
	if period <= 0 || len(closes) <= period {
		return nil, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, false
	}
	atrValues := make([]float64, len(closes))
	firstATR, _ := sma(trs[:period], period)
	atrValues[period] = firstATR
	for index := period + 1; index < len(closes); index++ {
		atrValues[index] = (atrValues[index-1]*float64(period-1) + trs[index-1]) / float64(period)
	}
	finalUpper := make([]float64, len(closes))
	finalLower := make([]float64, len(closes))
	direction := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		mid := (highs[index] + lows[index]) / 2
		basicUpper := mid + multiplier*atrValues[index]
		basicLower := mid - multiplier*atrValues[index]
		if index == period {
			finalUpper[index], finalLower[index] = basicUpper, basicLower
			if closes[index] >= mid {
				direction[index] = "up"
			} else {
				direction[index] = "down"
			}
			continue
		}
		if basicUpper < finalUpper[index-1] || closes[index-1] > finalUpper[index-1] {
			finalUpper[index] = basicUpper
		} else {
			finalUpper[index] = finalUpper[index-1]
		}
		if basicLower > finalLower[index-1] || closes[index-1] < finalLower[index-1] {
			finalLower[index] = basicLower
		} else {
			finalLower[index] = finalLower[index-1]
		}
		direction[index] = direction[index-1]
		if direction[index-1] == "down" && closes[index] > finalUpper[index] {
			direction[index] = "up"
		} else if direction[index-1] == "up" && closes[index] < finalLower[index] {
			direction[index] = "down"
		}
	}
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		points = append(points, supertrendPoint(finalUpper[index], finalLower[index], direction[index]))
	}
	return points, len(points) >= 2
}

func TestRecentTrueRangeMeanMatchesSeries(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.8)
	got, ok := recentTrueRangeMean(highs, lows, closes, 20)
	if !ok {
		t.Fatal("recentTrueRangeMean returned false")
	}
	ranges := trueRangeSeries(highs, lows, closes)
	want, ok := sma(ranges, 20)
	if !ok {
		t.Fatal("sma returned false")
	}
	if got != want {
		t.Fatalf("recent mean = %v, want %v", got, want)
	}
}

func TestAIPerformanceDenominatorMatchesEMA(t *testing.T) {
	_, _, closes, _ := trendingSeries(80, 100, 0.8)
	changes := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		changes = append(changes, absFloat(closes[index]-closes[index-1]))
	}
	want, wantOK := ema(changes, 10)
	got, gotOK := aiPerformanceDenominator(closes, 10)
	if gotOK != wantOK || got != want {
		t.Fatalf("denominator = %v/%v, want %v/%v", got, gotOK, want, wantOK)
	}
}

func TestSupertrendATRFactorSummaryMatchesSeries(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.8)
	atrValues, ok := atrSeries(highs, lows, closes, 10)
	if !ok {
		t.Fatal("atrSeries returned false")
	}
	offset := len(closes) - len(atrValues)
	points, ok := supertrendSeriesWithATRFactor(highs, lows, closes, atrValues, offset, 2.5)
	if !ok {
		t.Fatal("supertrendSeriesWithATRFactor returned false")
	}
	previous, last, ama, ok := supertrendATRFactorSummary(highs, lows, closes, atrValues, offset, 2.5, 0.3)
	if !ok {
		t.Fatal("supertrendATRFactorSummary returned false")
	}
	if previous != points[len(points)-2] || last != points[len(points)-1] || ama != aiSupertrendAMA(points, 0.3) {
		t.Fatalf("summary differs: previous=%#v last=%#v ama=%v", previous, last, ama)
	}
}

func TestSupertrendUsesSeriesDirection(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.8)
	values := map[string]string{}
	signals := map[string]string{}

	addSupertrend(values, signals, highs, lows, closes, 10, 3)
	addAlphaTrend(values, signals, highs, lows, closes, volumes, 14, 1)
	addPSARFeatures(values, signals, highs, lows, closes)
	addChandelierExit(values, signals, highs, lows, closes, 22, 3)

	if values["supertrend"] == "" {
		t.Fatalf("missing supertrend: %#v", values)
	}
	for _, key := range []string{
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"sma_atr_supertrend",
		"sma_atr_supertrend_distance_pct",
		"supertrend_7_2",
		"supertrend_10_3",
		"supertrend_10_3_3",
		"supertrend_14_4",
		"adaptive_supertrend",
		"adaptive_supertrend_distance_pct",
		"adaptive_supertrend_assigned_atr",
		"adaptive_supertrend_high_centroid",
		"adaptive_supertrend_mid_centroid",
		"adaptive_supertrend_low_centroid",
		"ai_supertrend",
		"ai_supertrend_ama",
		"ai_supertrend_distance_pct",
		"ai_supertrend_target_factor",
		"ai_supertrend_performance_index",
		"ai_supertrend_best_centroid",
		"ai_supertrend_average_centroid",
		"ai_supertrend_worst_centroid",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s: %#v", key, values)
		}
	}
	if signals["supertrend_direction"] != "up" {
		t.Fatalf("supertrend_direction = %q, want up", signals["supertrend_direction"])
	}
	if signals["supertrend_flip"] == "" {
		t.Fatalf("missing supertrend flip: %#v", signals)
	}
	if signals["sma_atr_supertrend_direction"] == "" || signals["sma_atr_supertrend_flip"] == "" {
		t.Fatalf("missing SMA-ATR supertrend signals: %#v", signals)
	}
	if signals["supertrend_7_2_direction"] == "" || signals["supertrend_10_3_direction"] == "" || signals["supertrend_10_3_3_direction"] == "" || signals["supertrend_14_4_direction"] == "" {
		t.Fatalf("missing supertrend preset directions: %#v", signals)
	}
	if signals["adaptive_supertrend_direction"] == "" ||
		signals["adaptive_supertrend_flip"] == "" ||
		signals["adaptive_supertrend_volatility_cluster"] == "" {
		t.Fatalf("missing adaptive supertrend signals: %#v", signals)
	}
	if signals["ai_supertrend_direction"] == "" ||
		signals["ai_supertrend_flip"] == "" ||
		signals["ai_supertrend_cluster"] == "" ||
		signals["ai_supertrend_factor_cluster"] == "" {
		t.Fatalf("missing ai supertrend signals: %#v", signals)
	}
	if values["alphatrend"] == "" || values["mfi14"] == "" {
		t.Fatalf("missing alphatrend values: %#v", values)
	}
	if values["alphatrend_distance_pct"] == "" || values["alphatrend_slope_pct"] == "" {
		t.Fatalf("missing alphatrend distance/slope: %#v", values)
	}
	if signals["alphatrend_direction"] == "" {
		t.Fatalf("missing alphatrend direction: %#v", signals)
	}
	if signals["alphatrend_flip"] == "" {
		t.Fatalf("missing alphatrend flip: %#v", signals)
	}
	if signals["alphatrend_cross"] == "" || signals["alphatrend_signal"] == "" {
		t.Fatalf("missing alphatrend crossover signals: %#v", signals)
	}
	if values["psar"] == "" || values["psar_distance_pct"] == "" {
		t.Fatalf("missing psar values: %#v", values)
	}
	if signals["psar_direction"] != "up" {
		t.Fatalf("psar_direction = %q, want up", signals["psar_direction"])
	}
	if values["chandelier_long"] == "" || values["chandelier_short"] == "" || values["chandelier_stop_distance_pct"] == "" {
		t.Fatalf("missing chandelier values: %#v", values)
	}
	if signals["chandelier_direction"] == "" {
		t.Fatalf("missing chandelier direction: %#v", signals)
	}
}

func TestSupertrendZoneUsesRecentFlipPivots(t *testing.T) {
	highs := []float64{105, 103, 101, 99, 104, 110, 118, 116}
	lows := []float64{100, 98, 94, 92, 96, 103, 109, 108}
	closes := []float64{102, 100, 96, 94, 102, 108, 114, 110}
	points := []trendPoint{
		{value: 103, direction: trendDirectionDown},
		{value: 101, direction: trendDirectionDown},
		{value: 97, direction: trendDirectionUp},
		{value: 100, direction: trendDirectionUp},
		{value: 105, direction: trendDirectionUp},
		{value: 112, direction: trendDirectionDown},
	}

	zone, ok := supertrendZone(highs, lows, closes, points, 2, 3, 1.5)

	if !ok {
		t.Fatal("supertrendZone returned false")
	}
	if zone.pivotLow != 92 {
		t.Fatalf("pivotLow = %v, want 92", zone.pivotLow)
	}
	if zone.pivotHigh != 118 {
		t.Fatalf("pivotHigh = %v, want 118", zone.pivotHigh)
	}
	if zone.side != "bear" {
		t.Fatalf("side = %q, want bear", zone.side)
	}
	if zone.area != "premium" {
		t.Fatalf("area = %q, want premium", zone.area)
	}
	if zone.fib618 == 0 || zone.extension == 0 || zone.premiumBand == 0 || zone.discountBand == 0 {
		t.Fatalf("missing zone levels: %#v", zone)
	}
}

func TestAdaptiveVolatilityClusterAssignsLevels(t *testing.T) {
	cluster, ok := adaptiveVolatilityCluster([]float64{1, 1.1, 1.2, 2, 2.1, 3.5, 3.7, 3.9}, 3.8)

	if !ok {
		t.Fatal("adaptiveVolatilityCluster returned false")
	}
	if cluster.cluster != "high" {
		t.Fatalf("cluster = %q, want high", cluster.cluster)
	}
	if cluster.assignedATR != cluster.highCentroid {
		t.Fatalf("assigned ATR = %v, high centroid = %v", cluster.assignedATR, cluster.highCentroid)
	}
	if cluster.highCentroid <= cluster.midCentroid || cluster.midCentroid <= cluster.lowCentroid {
		t.Fatalf("unexpected centroids: %#v", cluster)
	}
}

func TestAIPerformanceClustersRanksBestAverageWorst(t *testing.T) {
	results := []aiSupertrendFactorResult{
		{factor: 1, perf: -1},
		{factor: 1.5, perf: -0.8},
		{factor: 2, perf: 0.1},
		{factor: 2.5, perf: 0.2},
		{factor: 3, perf: 1.2},
		{factor: 3.5, perf: 1.4},
	}

	clusters, ok := aiPerformanceClusters(results)

	if !ok {
		t.Fatal("aiPerformanceClusters returned false")
	}
	if clusters[0].name != "worst" || clusters[1].name != "average" || clusters[2].name != "best" {
		t.Fatalf("unexpected cluster names: %#v", clusters)
	}
	if clusters[2].centroid <= clusters[1].centroid || clusters[1].centroid <= clusters[0].centroid {
		t.Fatalf("unexpected centroids: %#v", clusters)
	}
	if len(clusters[2].factors) == 0 {
		t.Fatalf("best cluster has no factors: %#v", clusters)
	}
}

func TestAlphaTrendSignalsRequirePreviousSameAndOppositeCross(t *testing.T) {
	points := trendPointsFromValues(10, 9, 11, 8, 12)

	cross, signal := alphaTrendSignals(points)

	if cross != "buy" {
		t.Fatalf("alphatrend cross = %q, want buy", cross)
	}
	if signal != "none" {
		t.Fatalf("alphatrend signal = %q, want none", signal)
	}
}

func TestAlphaTrendSignalsAllowAlternatingBuy(t *testing.T) {
	points := trendPointsFromValues(10, 9, 11, 8, 12, 9, 10, 8, 11)

	cross, signal := alphaTrendSignals(points)

	if cross != "buy" {
		t.Fatalf("alphatrend cross = %q, want buy", cross)
	}
	if signal != "buy" {
		t.Fatalf("alphatrend signal = %q, want buy", signal)
	}
}

func TestAlphaTrendSeriesCompactMatchesBatch(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.35)

	gotPoints, gotMFI, ok := alphaTrendSeriesCompact(highs, lows, closes, volumes, 14, 1)
	if !ok {
		t.Fatal("alphaTrendSeriesCompact returned false")
	}
	wantPoints, wantMFI, ok := alphaTrendSeriesBatch(highs, lows, closes, volumes, 14, 1)
	if !ok {
		t.Fatal("alphaTrendSeriesBatch returned false")
	}
	if len(gotPoints) != len(wantPoints) {
		t.Fatalf("alpha trend points = %d, want %d", len(gotPoints), len(wantPoints))
	}
	for index := range gotPoints {
		assertFloatClose(t, "alpha trend point", gotPoints[index].value, wantPoints[index].value)
		if gotPoints[index].direction != wantPoints[index].direction {
			t.Fatalf("alpha trend direction[%d] = %q, want %q", index, gotPoints[index].direction.String(), wantPoints[index].direction.String())
		}
	}
	assertFloatClose(t, "alpha trend mfi", gotMFI, wantMFI)
}

func TestChandelierExitStreamATRMatchesBatchForEveryAppend(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(120, 100, 0.35)
	state := newBasicIndicatorState()
	for index := range closes {
		state.append(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1])
		if index <= 22 {
			continue
		}
		gotLong, gotShort, gotOK := chandelierExitWithState(highs[:index+1], lows[:index+1], closes[:index+1], 22, 3, state)
		wantLong, wantShort, wantOK := chandelierExit(highs[:index+1], lows[:index+1], closes[:index+1], 22, 3)
		if gotOK != wantOK {
			t.Fatalf("chandelier stream ok at index %d = %t, want %t", index, gotOK, wantOK)
		}
		assertFloatClose(t, "chandelier stream long", gotLong, wantLong)
		assertFloatClose(t, "chandelier stream short", gotShort, wantShort)
	}
}

func TestChandelierExitStreamATRCloneContinuesIndependently(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(40, 100, 0.35)
	state := buildBasicIndicatorState(highs[:30], lows[:30], closes[:30], volumes[:30])
	cloned := state.clone()
	state.append(highs[:31], lows[:31], closes[:31], volumes[:31])
	cloned.append(highs[:31], lows[:31], closes[:31], volumes[:31])
	got, gotOK := state.atrValue(22)
	want, wantOK := cloned.atrValue(22)
	if gotOK != wantOK {
		t.Fatalf("cloned atr22 ok = %t, want %t", wantOK, gotOK)
	}
	assertFloatClose(t, "cloned atr22", got, want)
}

func TestAlphaTrendStreamMatchesCompactForEveryAppend(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(160, 100, 0.35)
	state := newBasicIndicatorState()
	for index := range closes {
		state.append(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1])
		if index < 15 {
			continue
		}
		gotPoints, gotMFI, ok := state.alphaTrendValues(14, 1)
		if !ok {
			t.Fatalf("missing stream alpha trend at index %d", index)
		}
		wantPoints, wantMFI, ok := alphaTrendSeriesCompact(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], 14, 1)
		if !ok {
			t.Fatalf("missing compact alpha trend at index %d", index)
		}
		if len(gotPoints) != len(wantPoints) {
			t.Fatalf("stream alpha trend points at index %d = %d, want %d", index, len(gotPoints), len(wantPoints))
		}
		for pointIndex := range wantPoints {
			assertFloatClose(t, "stream alpha trend point", gotPoints[pointIndex].value, wantPoints[pointIndex].value)
			if gotPoints[pointIndex].direction != wantPoints[pointIndex].direction {
				t.Fatalf("stream alpha trend direction[%d] at index %d = %q, want %q", pointIndex, index, gotPoints[pointIndex].direction.String(), wantPoints[pointIndex].direction.String())
			}
		}
		assertFloatClose(t, "stream alpha trend mfi", gotMFI, wantMFI)
		gotCross, gotSignal := alphaTrendSignals(gotPoints)
		wantCross, wantSignal := alphaTrendSignals(wantPoints)
		if gotCross != wantCross || gotSignal != wantSignal {
			t.Fatalf("stream alpha trend signals at index %d = (%q, %q), want (%q, %q)", index, gotCross, gotSignal, wantCross, wantSignal)
		}
	}
}

func TestLivermoreFeaturesOutputForLongSeries(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(430, 100, 0.3)
	opens := make([]float64, 0, len(closes))
	for _, closeValue := range closes {
		opens = append(opens, closeValue-0.1)
	}
	values := map[string]string{}
	signals := map[string]string{}

	addLivermoreFeatures(values, signals, highs, lows, closes, opens)

	if signals["livermore_trend"] == "" || signals["livermore_signal"] == "" {
		t.Fatalf("missing livermore signals: %#v", signals)
	}
	if values["livermore_active_point"] == "" {
		t.Fatalf("missing livermore active point: %#v", values)
	}
}

func TestSqueezeMomentumOutputsDelta(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addSqueezeMomentum(values, signals, highs, lows, closes)

	if values["squeeze_momentum"] == "" {
		t.Fatalf("missing squeeze_momentum: %#v", values)
	}
	if values["squeeze_momentum_delta"] == "" {
		t.Fatalf("missing squeeze_momentum_delta: %#v", values)
	}
	if signals["squeeze"] == "" || signals["momentum_state"] == "" || signals["squeeze_state"] == "" {
		t.Fatalf("missing squeeze signals: %#v", signals)
	}
}

func TestDynamicSwingAnchoredVWAPOutputsState(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(90, 100, 0.4)
	values := map[string]string{}
	signals := map[string]string{}

	addDynamicSwingAnchoredVWAP(values, signals, highs, lows, closes, volumes)

	for _, key := range []string{
		"dynamic_swing_vwap",
		"dynamic_swing_vwap_distance_pct",
		"dynamic_swing_vwap_anchor_price",
		"dynamic_swing_vwap_anchor_age",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"dynamic_swing_vwap_direction",
		"dynamic_swing_vwap_position",
		"dynamic_swing_vwap_anchor_type",
		"dynamic_swing_vwap_swing_label",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestDynamicSwingVWAPPrecomputedAlphaMatchesAPTStep(t *testing.T) {
	const apt = 20.0
	wantP, wantVolume, wantValue := dynamicSwingVWAPStep(1000, 10, 101, 99, 100, 12, apt)
	gotP, gotVolume, gotValue := dynamicSwingVWAPStepAlpha(1000, 10, 101, 99, 100, 12, alphaFromAPT(apt))
	assertFloatClose(t, "dynamic swing p", gotP, wantP)
	assertFloatClose(t, "dynamic swing volume", gotVolume, wantVolume)
	assertFloatClose(t, "dynamic swing value", gotValue, wantValue)
}

func TestDynamicSwingRollingExtremaMatchReference(t *testing.T) {
	testCases := []struct {
		name   string
		values []float64
		period int
	}{
		{name: "oscillating", values: []float64{3, 1, 4, 2, 5, 0, 6, 2}, period: 4},
		{name: "equal extrema", values: []float64{3, 3, 2, 2, 3, 3, 1, 1}, period: 3},
		{name: "partial window", values: []float64{2, 1, 3}, period: 5},
		{name: "increasing", values: []float64{1, 2, 3, 4, 5}, period: 3},
		{name: "decreasing", values: []float64{5, 4, 3, 2, 1}, period: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			highWindow := newFloatMonotonicWindow(true)
			lowWindow := newFloatMonotonicWindow(false)
			for index, value := range testCase.values {
				highWindow.push(index, value)
				lowWindow.push(index, value)
				oldestIndex := index - testCase.period + 1
				highWindow.expireBefore(oldestIndex)
				lowWindow.expireBefore(oldestIndex)
				windowHigh, highOK := highWindow.value()
				windowLow, lowOK := lowWindow.value()
				if got, want := highOK && value == windowHigh, isHighestAt(testCase.values, testCase.period, index); got != want {
					t.Fatalf("highest index %d = %t, want %t", index, got, want)
				}
				if got, want := lowOK && value == windowLow, isLowestAt(testCase.values, testCase.period, index); got != want {
					t.Fatalf("lowest index %d = %t, want %t", index, got, want)
				}
			}
		})
	}
}

func BenchmarkDynamicSwingAnchoredVWAP(b *testing.B) {
	highs, lows, closes, volumes := trendingSeries(268, 100, 0.3)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		state := dynamicSwingAnchoredVWAP(highs, lows, closes, volumes, dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, dynamicSwingVWAPUseAdapt, dynamicSwingVWAPVolBias)
		if !state.ok {
			b.Fatal("dynamicSwingAnchoredVWAP returned invalid state")
		}
	}
}

func TestDynamicSwingAnchoredVWAPStreamMatchesBatchForEveryAppend(t *testing.T) {
	highs := make([]float64, 180)
	lows := make([]float64, len(highs))
	closes := make([]float64, len(highs))
	volumes := make([]float64, len(highs))
	for index := range closes {
		closes[index] = 100 + 12*math.Sin(float64(index)*0.18)
		highs[index] = closes[index] + 1
		lows[index] = closes[index] - 1
		volumes[index] = 100 + float64(index%9)*10
	}
	stream := newStreamDynamicSwingVWAPState(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT)
	for index := range closes {
		start := maxInt(0, index-79)
		stream.append(highs[start:index+1], lows[start:index+1], closes[start:index+1], volumes[start:index+1])
		if index+1 < dynamicSwingVWAPPeriod {
			continue
		}
		got, ok := stream.value(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, false, dynamicSwingVWAPVolBias)
		if !ok {
			t.Fatalf("missing stream dynamic swing vwap at index %d", index)
		}
		want := dynamicSwingAnchoredVWAP(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, false, dynamicSwingVWAPVolBias)
		assertFloatClose(t, "dynamic swing stream value", got.value, want.value)
		assertFloatClose(t, "dynamic swing stream anchor", got.anchor, want.anchor)
		if got.anchorAge != want.anchorAge || got.dir != want.dir || got.anchorType != want.anchorType || got.swingLabel != want.swingLabel || got.ok != want.ok {
			t.Fatalf("dynamic swing stream state at index %d = %#v, want %#v", index, got, want)
		}
	}
}

func TestDynamicSwingAnchoredVWAPStateCloneContinuesIndependently(t *testing.T) {
	highs, lows, closes, volumes := trendingSeries(90, 100, 0.3)
	state := buildBasicIndicatorState(highs[:80], lows[:80], closes[:80], volumes[:80])
	cloned := state.clone()

	state.append(highs[:81], lows[:81], closes[:81], volumes[:81])
	cloned.append(highs[:81], lows[:81], closes[:81], volumes[:81])
	got, gotOK := state.dynamicSwingVWAPValue(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, false, dynamicSwingVWAPVolBias)
	want, wantOK := cloned.dynamicSwingVWAPValue(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT, false, dynamicSwingVWAPVolBias)
	if !gotOK || !wantOK || !reflect.DeepEqual(got, want) {
		t.Fatalf("cloned dynamic swing state = %#v, want %#v", want, got)
	}
}

func trendPointsFromValues(values ...float64) []trendPoint {
	points := make([]trendPoint, 0, len(values))
	for _, value := range values {
		points = append(points, trendPoint{value: value})
	}
	return points
}

func TestSqueezeState(t *testing.T) {
	if got := squeezeState("on", 1, 2); got != "squeeze_on" {
		t.Fatalf("squeezeState on = %q, want squeeze_on", got)
	}
	if got := squeezeState("released", 2, 1); got != "release_up" {
		t.Fatalf("squeezeState release up = %q, want release_up", got)
	}
	if got := squeezeState("released", -2, -1); got != "release_down" {
		t.Fatalf("squeezeState release down = %q, want release_down", got)
	}
	if got := squeezeState("off", 0, 0); got != "off_flat" {
		t.Fatalf("squeezeState off flat = %q, want off_flat", got)
	}
}

func TestBollingerShapeFeatures(t *testing.T) {
	_, _, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addBollingerFeatures(values, signals, closes)

	for _, key := range []string{
		"bb_width_pct",
		"bb_percent_b",
		"bb_width_delta",
		"bb_middle_slope_pct",
		"bb_upper_slope_pct",
		"bb_lower_slope_pct",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["bb_width_state"] == "" || signals["bb_trend"] == "" {
		t.Fatalf("missing bollinger signals: %#v", signals)
	}
}

func TestBollingerStateHelpers(t *testing.T) {
	if got := bollingerWidthState(2, 10); got != "expanding" {
		t.Fatalf("bollingerWidthState expanding = %q", got)
	}
	if got := bollingerWidthState(-2, 10); got != "contracting" {
		t.Fatalf("bollingerWidthState contracting = %q", got)
	}
	if got := bollingerTrend(0.01); got != "flat" {
		t.Fatalf("bollingerTrend flat = %q", got)
	}
}

func TestChannelFeatures(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(80, 100, 0.35)
	values := map[string]string{}
	signals := map[string]string{}

	addChannelFeatures(values, signals, highs, lows, closes)

	for _, key := range []string{
		"donchian_high20",
		"donchian_low20",
		"donchian_mid20",
		"donchian_width_pct20",
		"donchian_position20",
		"keltner_upper20",
		"keltner_middle20",
		"keltner_lower20",
		"keltner_width_pct20",
		"keltner_position20",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	if signals["donchian_breakout"] == "" || signals["keltner_breakout"] == "" {
		t.Fatalf("missing channel signals: %#v", signals)
	}
}

func TestChannelBreakout(t *testing.T) {
	if got := channelBreakout(11, 10, 5); got != "breakout_up" {
		t.Fatalf("channelBreakout up = %q", got)
	}
	if got := channelBreakout(4, 10, 5); got != "breakout_down" {
		t.Fatalf("channelBreakout down = %q", got)
	}
	if got := channelBreakout(7, 10, 5); got != "inside" {
		t.Fatalf("channelBreakout inside = %q", got)
	}
}

func TestDonchianBreakoutUsesPreviousChannel(t *testing.T) {
	highs := linearValues(21, 10, 1)
	lows := linearValues(21, 5, 1)
	closes := linearValues(21, 7, 1)
	closes[len(closes)-1] = highs[len(highs)-2] + 0.5
	values := map[string]string{}
	signals := map[string]string{}

	addDonchianChannelFeatures(values, signals, highs, lows, closes, 20)

	if signals["donchian_breakout"] != "breakout_up" {
		t.Fatalf("donchian_breakout = %q, want breakout_up", signals["donchian_breakout"])
	}
}

func TestSqueezeMomentumAtUsesRangeBaseline(t *testing.T) {
	highs := []float64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	lows := []float64{9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	closes := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}

	value, ok := squeezeMomentumAt(highs, lows, closes, 5, 10)
	if !ok {
		t.Fatal("squeezeMomentumAt returned false")
	}
	if value != 2 {
		t.Fatalf("momentum = %v, want 2", value)
	}
}

func TestSqueezeMomentumAtCompactMatchesBatch(t *testing.T) {
	highs, lows, closes, _ := trendingSeries(120, 100, 0.35)

	got, ok := squeezeMomentumAtCompact(highs, lows, closes, 20, len(closes))
	if !ok {
		t.Fatal("squeezeMomentumAtCompact returned false")
	}
	want, ok := squeezeMomentumAtBatch(highs, lows, closes, 20, len(closes))
	if !ok {
		t.Fatal("squeezeMomentumAtBatch returned false")
	}
	assertFloatClose(t, "squeeze momentum current", got, want)

	gotPrevious, ok := squeezeMomentumAtCompact(highs, lows, closes, 20, len(closes)-1)
	if !ok {
		t.Fatal("previous squeezeMomentumAtCompact returned false")
	}
	wantPrevious, ok := squeezeMomentumAtBatch(highs, lows, closes, 20, len(closes)-1)
	if !ok {
		t.Fatal("previous squeezeMomentumAtBatch returned false")
	}
	assertFloatClose(t, "squeeze momentum previous", gotPrevious, wantPrevious)
}

func trendingSeries(length int, start float64, step float64) ([]float64, []float64, []float64, []float64) {
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	volumes := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		closeValue := start + float64(index)*step
		highs = append(highs, closeValue+1.2)
		lows = append(lows, closeValue-1)
		closes = append(closes, closeValue)
		volumes = append(volumes, 100+float64(index%7))
	}
	return highs, lows, closes, volumes
}
