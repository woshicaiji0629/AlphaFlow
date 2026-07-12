package indicatorcalc

import "math"

type heikinAshiCandle struct {
	open  float64
	high  float64
	low   float64
	close float64
}

func addHeikinAshiFeatures(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	addHeikinAshiFeaturesToSet(nil, values, signals, opens, highs, lows, closes)
}

func addHeikinAshiFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	last, ok := heikinAshiLast(opens, highs, lows, closes)
	if !ok {
		return
	}
	setValueTarget(target, values, "ha_open", last.open, true)
	setValueTarget(target, values, "ha_high", last.high, true)
	setValueTarget(target, values, "ha_low", last.low, true)
	setValueTarget(target, values, "ha_close", last.close, true)
	signals["ha_trend"] = heikinAshiTrend([]heikinAshiCandle{last})
	signals["ha_strength"] = heikinAshiStrength(last)
}

func heikinAshiLast(opens []float64, highs []float64, lows []float64, closes []float64) (heikinAshiCandle, bool) {
	if len(closes) == 0 || len(opens) != len(closes) || len(highs) != len(closes) || len(lows) != len(closes) {
		return heikinAshiCandle{}, false
	}
	previousOpen := 0.0
	previousClose := 0.0
	last := heikinAshiCandle{}
	for index := range closes {
		closeValue := (opens[index] + highs[index] + lows[index] + closes[index]) / 4
		openValue := (opens[index] + closes[index]) / 2
		if index > 0 {
			openValue = (previousOpen + previousClose) / 2
		}
		last = heikinAshiCandle{
			open:  openValue,
			high:  maxFloat(highs[index], openValue, closeValue),
			low:   minFloat(lows[index], openValue, closeValue),
			close: closeValue,
		}
		previousOpen, previousClose = openValue, closeValue
	}
	return last, true
}

func heikinAshiSeries(opens []float64, highs []float64, lows []float64, closes []float64) ([]heikinAshiCandle, bool) {
	if len(closes) == 0 || len(opens) != len(closes) || len(highs) != len(closes) || len(lows) != len(closes) {
		return nil, false
	}
	series := make([]heikinAshiCandle, len(closes))
	for index := range closes {
		closeValue := (opens[index] + highs[index] + lows[index] + closes[index]) / 4
		openValue := (opens[index] + closes[index]) / 2
		if index > 0 {
			openValue = (series[index-1].open + series[index-1].close) / 2
		}
		series[index] = heikinAshiCandle{
			open:  openValue,
			high:  maxFloat(highs[index], openValue, closeValue),
			low:   minFloat(lows[index], openValue, closeValue),
			close: closeValue,
		}
	}
	return series, true
}

func heikinAshiTrend(series []heikinAshiCandle) string {
	if len(series) == 0 {
		return "neutral"
	}
	last := series[len(series)-1]
	switch {
	case last.close > last.open:
		return "bull"
	case last.close < last.open:
		return "bear"
	default:
		return "neutral"
	}
}

func heikinAshiStrength(candle heikinAshiCandle) string {
	rangeValue := candle.high - candle.low
	if rangeValue <= 0 {
		return "weak"
	}
	body := math.Abs(candle.close - candle.open)
	if body/rangeValue >= 0.55 {
		return "strong"
	}
	return "weak"
}
