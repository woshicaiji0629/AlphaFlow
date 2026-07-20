package indicatorcalc

import "math"

const (
	waveTrendOverboughtLevel = 60
	waveTrendUpperLevel      = 53
	waveTrendLowerLevel      = -53
	waveTrendOversoldLevel   = -60
)

func addWaveTrendFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, basic *basicIndicatorState) {
	wt1, wt2, previousWT1, previousWT2, previousDelta, ok := basic.waveTrendValue()
	if !ok {
		wt1, wt2, previousWT1, previousWT2, previousDelta, ok = waveTrend(highs, lows, closes, 10, 21)
	}
	if !ok {
		return
	}
	delta := wt1 - wt2
	setValueTarget(target, values, "wavetrend_wt1", wt1, true)
	setValueTarget(target, values, "wavetrend_wt2", wt2, true)
	setValueTarget(target, values, "wavetrend_delta", delta, true)
	signals["wavetrend_cross"] = crossSignal(previousWT1, previousWT2, wt1, wt2)
	signals["wavetrend_zone"] = waveTrendZone(wt1)
	signals["wavetrend_momentum"] = waveTrendMomentum(delta, previousDelta)
}

func waveTrend(highs []float64, lows []float64, closes []float64, channelLength int, averageLength int) (float64, float64, float64, float64, float64, bool) {
	wt1Series, ok := waveTrendWT1Series(highs, lows, closes, channelLength, averageLength)
	if !ok || len(wt1Series) < 5 {
		return 0, 0, 0, 0, 0, false
	}
	wt1 := wt1Series[len(wt1Series)-1]
	previousWT1 := wt1Series[len(wt1Series)-2]
	wt2, ok := sma(wt1Series, 4)
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	previousWT2, ok := sma(wt1Series[:len(wt1Series)-1], 4)
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	previousDelta := previousWT1 - previousWT2
	return wt1, wt2, previousWT1, previousWT2, previousDelta, true
}

func waveTrendWT1Series(highs []float64, lows []float64, closes []float64, channelLength int, averageLength int) ([]float64, bool) {
	if channelLength <= 0 || averageLength <= 0 || len(closes) != len(highs) || len(closes) != len(lows) || len(closes) < channelLength*2+averageLength {
		return nil, false
	}
	ap := make([]float64, len(closes))
	for index := range closes {
		ap[index] = (highs[index] + lows[index] + closes[index]) / 3
	}
	esaSeries, ok := emaSeries(ap, channelLength)
	if !ok {
		return nil, false
	}
	deviations := make([]float64, 0, len(esaSeries))
	for index, esa := range esaSeries {
		apIndex := index + channelLength - 1
		deviations = append(deviations, math.Abs(ap[apIndex]-esa))
	}
	dSeries, ok := emaSeries(deviations, channelLength)
	if !ok {
		return nil, false
	}
	ciSeries := make([]float64, 0, len(dSeries))
	for index, d := range dSeries {
		if d == 0 {
			ciSeries = append(ciSeries, 0)
			continue
		}
		esaIndex := index + channelLength - 1
		apIndex := index + channelLength*2 - 2
		ciSeries = append(ciSeries, (ap[apIndex]-esaSeries[esaIndex])/(0.015*d))
	}
	return emaSeries(ciSeries, averageLength)
}

func waveTrendZone(wt1 float64) string {
	switch {
	case wt1 >= waveTrendOverboughtLevel:
		return "overbought"
	case wt1 <= waveTrendOversoldLevel:
		return "oversold"
	case wt1 >= waveTrendUpperLevel:
		return "upper"
	case wt1 <= waveTrendLowerLevel:
		return "lower"
	case wt1 > 0:
		return "bull"
	case wt1 < 0:
		return "bear"
	default:
		return "neutral"
	}
}

func waveTrendMomentum(delta float64, previousDelta float64) string {
	change := delta - previousDelta
	switch {
	case math.Abs(change) < 0.000001:
		return "flat"
	case math.Abs(delta) > math.Abs(previousDelta):
		return "strengthening"
	default:
		return "weakening"
	}
}
