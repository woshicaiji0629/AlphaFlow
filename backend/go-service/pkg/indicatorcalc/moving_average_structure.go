package indicatorcalc

import (
	"math"
	"strconv"
)

func addMovingAverageStructureFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, basic *basicIndicatorState, ema7 float64, ema25 float64, ema99 float64) {
	if len(closes) < 110 {
		return
	}
	prevEMA7, ok7 := previousEMAFromStateOrSeries(basic, closes, 7, 1)
	prevEMA25, ok25 := previousEMAFromStateOrSeries(basic, closes, 25, 1)
	prevEMA99, ok99 := previousEMAFromStateOrSeries(basic, closes, 99, 5)
	if ok7 && ok25 {
		signals["ma_cross"] = crossSignal(prevEMA7, prevEMA25, ema7, ema25)
	}
	if ok7 && ok25 && ok99 {
		currentSpread := maxFloat(ema7, ema25, ema99) - minFloat(ema7, ema25, ema99)
		previousSpread := maxFloat(prevEMA7, prevEMA25, prevEMA99) - minFloat(prevEMA7, prevEMA25, prevEMA99)
		setValueTarget(target, values, "ma_group_spread_pct", currentSpread/closes[len(closes)-1]*100, closes[len(closes)-1] != 0)
		signals["ma_spread_state"] = spreadState(currentSpread, previousSpread)
		signals["ma_compression"] = compressionState(currentSpread, closes[len(closes)-1])
	}
	prevEMA25, okPrevSlope := previousEMAFromStateOrSeries(basic, closes, 25, 5)
	if okPrevSlope && prevEMA25 != 0 {
		slopePct := percentDistance(ema25, prevEMA25)
		signals["ma_slope_state"] = slopeState(slopePct)
	}
	signals["ma_breakout"] = movingAverageBreakout(closes[len(closes)-1], ema7, ema25, ema99)
}

func movingAverageArrangement(ema7 float64, ema25 float64, ema99 float64) string {
	switch {
	case ema7 > ema25 && ema25 > ema99:
		return "bull"
	case ema7 < ema25 && ema25 < ema99:
		return "bear"
	default:
		return "mixed"
	}
}

func spreadState(current float64, previous float64) string {
	threshold := 0.00000001
	if previous > 0 {
		threshold = previous * 0.08
	}
	switch {
	case current > previous+threshold:
		return "expanding"
	case current < previous-threshold:
		return "contracting"
	default:
		return "flat"
	}
}

func compressionState(spread float64, price float64) string {
	if price == 0 {
		return "normal"
	}
	if spread/price*100 <= 0.25 {
		return "compressed"
	}
	return "normal"
}

func slopeState(slopePct float64) string {
	switch {
	case slopePct > 0.08:
		return "up"
	case slopePct < -0.08:
		return "down"
	default:
		return "flat"
	}
}

func movingAverageBreakout(price float64, ema7 float64, ema25 float64, ema99 float64) string {
	upper := maxFloat(ema7, ema25, ema99)
	lower := minFloat(ema7, ema25, ema99)
	switch {
	case price > upper:
		return "above_group"
	case price < lower:
		return "below_group"
	default:
		return "inside_group"
	}
}

func movingAverageState(ema7 float64, ema25 float64, ema99 float64, last float64) string {
	switch {
	case last > ema7 && ema7 > ema25 && ema25 > ema99:
		return "bull"
	case last < ema7 && ema7 < ema25 && ema25 < ema99:
		return "bear"
	default:
		return "mixed"
	}
}

func addEZEMASuiteFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, basic *basicIndicatorState) {
	periods := [...]int{5, 8, 9, 34, 55, 89, 144, 200}
	if len(closes) < periods[len(periods)-1]+1 {
		return
	}

	var current [len(periods)]float64
	var previous [len(periods)]float64
	for index, period := range periods {
		value, ok := emaFromStateOrSeries(basic, closes, period)
		if !ok {
			return
		}
		prev, ok := previousEMAFromStateOrSeries(basic, closes, period, 1)
		if !ok {
			return
		}
		current[index] = value
		previous[index] = prev
		setValueTarget(target, values, "ez_ema_"+strconv.Itoa(period), value, true)
	}

	fast := current[2]
	slow := current[4]
	prevFast := previous[2]
	prevSlow := previous[4]
	last := closes[len(closes)-1]
	prevClose := closes[len(closes)-2]

	setValueTarget(target, values, "ez_ema_fast", fast, true)
	setValueTarget(target, values, "ez_ema_slow", slow, true)
	setValueTarget(target, values, "ez_ema_spread_pct", percentDistance(fast, slow), slow != 0)
	signals["ez_ema_cross"] = crossSignal(prevFast, prevSlow, fast, slow)
	signals["ez_price_cross_ema_pair"] = priceCrossEMAPair(prevClose, last, prevFast, prevSlow, fast, slow)
	signals["ez_price_above_ema_pair"] = boolText(last > fast && last > slow)
	signals["ez_price_below_ema_pair"] = boolText(last < fast && last < slow)
	signals["ez_ema_stack"] = ezEMAStack(current)

	currentSpread := ezEMASpread(current)
	previousSpread := ezEMASpread(previous)
	setValueTarget(target, values, "ez_ema_group_spread_pct", currentSpread/last*100, last != 0)
	signals["ez_ema_spread_state"] = spreadState(currentSpread, previousSpread)
	signals["ez_ema_compression"] = compressionState(currentSpread, last)
}

func priceCrossEMAPair(
	previousClose float64,
	currentClose float64,
	previousFast float64,
	previousSlow float64,
	currentFast float64,
	currentSlow float64,
) string {
	switch {
	case previousClose <= previousFast && previousClose <= previousSlow &&
		currentClose > currentFast && currentClose > currentSlow:
		return "up"
	case previousClose >= previousFast && previousClose >= previousSlow &&
		currentClose < currentFast && currentClose < currentSlow:
		return "down"
	default:
		return "none"
	}
}

func ezEMAStack(values [8]float64) string {
	bull := values[0] > values[1] &&
		values[1] > values[2] &&
		values[2] > values[3] &&
		values[3] > values[4] &&
		values[4] > values[5] &&
		values[5] > values[6] &&
		values[6] > values[7]
	bear := values[0] < values[1] &&
		values[1] < values[2] &&
		values[2] < values[3] &&
		values[3] < values[4] &&
		values[4] < values[5] &&
		values[5] < values[6] &&
		values[6] < values[7]
	switch {
	case bull:
		return "bull"
	case bear:
		return "bear"
	default:
		return "mixed"
	}
}

func ezEMASpread(values [8]float64) float64 {
	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		minValue = math.Min(minValue, value)
		maxValue = math.Max(maxValue, value)
	}
	return maxValue - minValue
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
