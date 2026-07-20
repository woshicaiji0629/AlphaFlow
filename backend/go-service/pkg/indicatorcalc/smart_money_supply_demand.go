package indicatorcalc

const momentumBodyAveragePeriod = 20

type momentumBodyAverageWindow struct {
	values [momentumBodyAveragePeriod]float64
	count  int
	next   int
	sum    float64
}

func (w *momentumBodyAverageWindow) append(value float64) {
	if w.count == len(w.values) {
		w.sum -= w.values[w.next]
	} else {
		w.count++
	}
	w.values[w.next] = value
	w.next = (w.next + 1) % len(w.values)
	w.sum += value
}

func (w *momentumBodyAverageWindow) average() float64 {
	if w == nil || w.count == 0 {
		return 0
	}
	return w.sum / float64(w.count)
}

func addMomentumSupplyDemandValues(target *ValueSet, values map[string]string, signals map[string]string, state momentumSupplyDemandState, last int) {
	if state.supply.ok {
		setValueTarget(target, values, "momentum_supply_top", state.supply.top, true)
		setValueTarget(target, values, "momentum_supply_bottom", state.supply.bottom, true)
		setValueTarget(target, values, "momentum_supply_mid", (state.supply.top+state.supply.bottom)/2, true)
		setValueTarget(target, values, "momentum_supply_age", float64(last-state.supply.startIndex), true)
	}
	if state.demand.ok {
		setValueTarget(target, values, "momentum_demand_top", state.demand.top, true)
		setValueTarget(target, values, "momentum_demand_bottom", state.demand.bottom, true)
		setValueTarget(target, values, "momentum_demand_mid", (state.demand.top+state.demand.bottom)/2, true)
		setValueTarget(target, values, "momentum_demand_age", float64(last-state.demand.startIndex), true)
	}
	signals["momentum_sd_position"] = state.position
	signals["momentum_sd_retest"] = state.retestEvent
	signals["momentum_sd_break"] = state.breakEvent
}

func detectMomentumSupplyDemandZones(
	opens []float64,
	highs []float64,
	lows []float64,
	closes []float64,
	lookback int,
	momentumSpan int,
	momentumCount int,
	bodyMultiplier float64,
	atrPeriod int,
	maxZoneATR float64,
) (momentumSupplyDemandState, bool) {
	if lookback <= 0 || momentumSpan <= 0 || momentumCount <= 0 || bodyMultiplier <= 0 ||
		len(closes) < lookback || len(opens) != len(closes) || len(highs) != len(closes) || len(lows) != len(closes) {
		return momentumSupplyDemandState{}, false
	}
	atrValue, ok := atr(highs, lows, closes, atrPeriod)
	if !ok || atrValue <= 0 {
		return momentumSupplyDemandState{}, false
	}
	start := len(closes) - lookback
	last := len(closes) - 1
	minDistanceBetweenZones := 5
	lastDemandZone := start - minDistanceBetweenZones
	lastSupplyZone := start - minDistanceBetweenZones
	state := momentumSupplyDemandState{
		position:    "unknown",
		retestEvent: "none",
		breakEvent:  "none",
	}
	firstIndex := start + momentumSpan + 1
	if firstIndex > last {
		return state, false
	}
	bodyAverage := momentumBodyAverageWindow{}
	warmupStart := maxInt(0, firstIndex-momentumBodyAveragePeriod+1)
	for index := warmupStart; index < firstIndex; index++ {
		bodyAverage.append(absFloat(closes[index] - opens[index]))
	}
	for index := firstIndex; index <= last; index++ {
		bodyAverage.append(absFloat(closes[index] - opens[index]))
		bullish, bearish := momentumCandleCountsWithAverage(opens, closes, index, momentumSpan, bodyMultiplier, bodyAverage.average())
		if bullish >= momentumCount && index-lastDemandZone > minDistanceBetweenZones {
			anchor := index - momentumSpan - 1
			state.demand = momentumSupplyDemandZoneFromRange(highs[anchor], lows[anchor], anchor, atrValue, maxZoneATR)
			lastDemandZone = index
		}
		if bearish >= momentumCount && index-lastSupplyZone > minDistanceBetweenZones {
			anchor := index - momentumSpan - 1
			state.supply = momentumSupplyDemandZoneFromRange(highs[anchor], lows[anchor], anchor, atrValue, maxZoneATR)
			lastSupplyZone = index
		}
		if state.demand.ok && state.demand.breakIndex < 0 && index > state.demand.startIndex && closes[index] < state.demand.bottom {
			state.demand.breakIndex = index
		}
		if state.supply.ok && state.supply.breakIndex < 0 && index > state.supply.startIndex && closes[index] > state.supply.top {
			state.supply.breakIndex = index
		}
	}
	state.position = momentumSupplyDemandPosition(closes[last], state)
	state.retestEvent = momentumSupplyDemandRetest(highs[last], lows[last], last, state)
	state.breakEvent = momentumSupplyDemandBreak(last, state)
	return state, state.supply.ok || state.demand.ok
}

func momentumCandleCounts(opens []float64, closes []float64, index int, momentumSpan int, bodyMultiplier float64) (int, int) {
	averageBody := averageBodySizeAt(opens, closes, index, momentumBodyAveragePeriod)
	return momentumCandleCountsWithAverage(opens, closes, index, momentumSpan, bodyMultiplier, averageBody)
}

func momentumCandleCountsWithAverage(opens []float64, closes []float64, index int, momentumSpan int, bodyMultiplier float64, averageBody float64) (int, int) {
	if averageBody <= 0 {
		return 0, 0
	}
	bullish := 0
	bearish := 0
	start := index - momentumSpan + 1
	for bodyIndex := start; bodyIndex <= index; bodyIndex++ {
		bodySize := absFloat(closes[bodyIndex] - opens[bodyIndex])
		if bodySize < averageBody*bodyMultiplier {
			continue
		}
		if closes[bodyIndex] > opens[bodyIndex] {
			bullish++
		} else if closes[bodyIndex] < opens[bodyIndex] {
			bearish++
		}
	}
	return bullish, bearish
}

func averageBodySizeAt(opens []float64, closes []float64, index int, period int) float64 {
	if period <= 0 || index < 0 || index >= len(closes) || len(opens) != len(closes) {
		return 0
	}
	start := index - period + 1
	if start < 0 {
		start = 0
	}
	var total float64
	for bodyIndex := start; bodyIndex <= index; bodyIndex++ {
		total += absFloat(closes[bodyIndex] - opens[bodyIndex])
	}
	return total / float64(index-start+1)
}

func momentumSupplyDemandZoneFromRange(high float64, low float64, startIndex int, atrValue float64, maxZoneATR float64) momentumSupplyDemandZone {
	top := high
	bottom := low
	maxSize := atrValue * maxZoneATR
	if maxSize > 0 && top-bottom > maxSize {
		diff := top - bottom - maxSize
		top -= diff / 2
		bottom += diff / 2
	}
	return momentumSupplyDemandZone{
		top:        top,
		bottom:     bottom,
		startIndex: startIndex,
		breakIndex: -1,
		ok:         top > bottom,
	}
}

func momentumSupplyDemandPosition(price float64, state momentumSupplyDemandState) string {
	if state.supply.ok {
		if price > state.supply.top {
			return "above_supply"
		}
		if price >= state.supply.bottom {
			return "in_supply"
		}
	}
	if state.demand.ok {
		if price < state.demand.bottom {
			return "below_demand"
		}
		if price <= state.demand.top {
			return "in_demand"
		}
	}
	if state.supply.ok || state.demand.ok {
		return "between_zones"
	}
	return "unknown"
}

func momentumSupplyDemandRetest(high float64, low float64, last int, state momentumSupplyDemandState) string {
	if state.supply.ok && state.supply.breakIndex < 0 && last > state.supply.startIndex && high > state.supply.bottom {
		return "supply_retest"
	}
	if state.demand.ok && state.demand.breakIndex < 0 && last > state.demand.startIndex && low < state.demand.top {
		return "demand_retest"
	}
	return "none"
}

func momentumSupplyDemandBreak(last int, state momentumSupplyDemandState) string {
	if state.supply.ok && state.supply.breakIndex == last {
		return "supply_break"
	}
	if state.demand.ok && state.demand.breakIndex == last {
		return "demand_break"
	}
	return "none"
}
