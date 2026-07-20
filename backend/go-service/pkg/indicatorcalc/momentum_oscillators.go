package indicatorcalc

import "math"

func cci(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period {
		return 0, false
	}
	typicals := make([]float64, 0, period)
	for index := len(closes) - period; index < len(closes); index++ {
		typicals = append(typicals, (highs[index]+lows[index]+closes[index])/3)
	}
	mean := sum(typicals) / float64(period)
	var deviation float64
	for _, value := range typicals {
		deviation += math.Abs(value - mean)
	}
	deviation /= float64(period)
	if deviation == 0 {
		return 0, true
	}
	return (typicals[len(typicals)-1] - mean) / (0.015 * deviation), true
}

func williamsR(highs []float64, lows []float64, closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) < period {
		return 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	if highest == lowest {
		return -50, true
	}
	return (highest - closes[len(closes)-1]) / (highest - lowest) * -100, true
}

func roc(closes []float64, period int) (float64, bool) {
	if period <= 0 || len(closes) <= period {
		return 0, false
	}
	previous := closes[len(closes)-period-1]
	if previous == 0 {
		return 0, false
	}
	return (closes[len(closes)-1] - previous) / previous * 100, true
}

func oscillatorState(value float64, upper float64, lower float64) string {
	switch {
	case value >= upper:
		return "overbought"
	case value <= lower:
		return "oversold"
	default:
		return "neutral"
	}
}

func williamsState(value float64) string {
	switch {
	case value >= -20:
		return "overbought"
	case value <= -80:
		return "oversold"
	default:
		return "neutral"
	}
}

func rocState(value float64) string {
	switch {
	case value > 0.1:
		return "positive"
	case value < -0.1:
		return "negative"
	default:
		return "flat"
	}
}
