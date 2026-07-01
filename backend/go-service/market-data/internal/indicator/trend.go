package indicator

func addTrendFeatures(values map[string]string, signals map[string]string, closes []float64) {
	ema7, ok7 := ema(closes, 7)
	ema25, ok25 := ema(closes, 25)
	ema99, ok99 := ema(closes, 99)
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		setValue(values, "price_ema7_distance_pct", percentDistance(last, ema7), true)
		setValue(values, "price_ema25_distance_pct", percentDistance(last, ema25), true)
		setValue(values, "price_ema99_distance_pct", percentDistance(last, ema99), true)
		switch {
		case ema7 > ema25 && ema25 > ema99:
			signals["ema_alignment"] = "bull"
		case ema7 < ema25 && ema25 < ema99:
			signals["ema_alignment"] = "bear"
		default:
			signals["ema_alignment"] = "mixed"
		}
	}
	if len(closes) >= 35 {
		recent, okRecent := ema(closes, 25)
		prev, okPrev := ema(closes[:len(closes)-5], 25)
		if okRecent && okPrev && prev != 0 {
			slope := (recent - prev) / prev * 100
			setValue(values, "ema25_slope5_pct", slope, true)
			switch {
			case slope > 0.15:
				signals["trend_direction"] = "up"
			case slope < -0.15:
				signals["trend_direction"] = "down"
			default:
				signals["trend_direction"] = "range"
			}
		}
	}
}

func addSupertrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	lastClose := closes[len(closes)-1]
	setValue(values, "supertrend", lastPoint.value, true)
	setValue(values, "supertrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "supertrend_stop_distance_pct", absFloat(percentDistance(lastClose, lastPoint.value)), lastPoint.value != 0)
	signals["supertrend_direction"] = lastPoint.direction
	signals["supertrend_flip"] = trendFlip(points[lastIndex-1].direction, lastPoint.direction)

	for _, preset := range []struct {
		name       string
		period     int
		multiplier float64
	}{
		{name: "supertrend_7_2", period: 7, multiplier: 2},
		{name: "supertrend_10_3", period: 10, multiplier: 3},
		{name: "supertrend_10_3_3", period: 10, multiplier: 3.3},
		{name: "supertrend_14_4", period: 14, multiplier: 4},
	} {
		presetValue, presetDirection, presetOK := supertrend(highs, lows, closes, preset.period, preset.multiplier)
		if !presetOK {
			continue
		}
		setValue(values, preset.name, presetValue, true)
		signals[preset.name+"_direction"] = presetDirection
	}
}

func addAlphaTrend(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return
	}
	lastIndex := len(points) - 1
	lastPoint := points[lastIndex]
	prevPoint := points[lastIndex-1]
	lastClose := closes[len(closes)-1]
	setValue(values, "alphatrend", lastPoint.value, true)
	setValue(values, "mfi14", lastMFI, true)
	setValue(values, "alphatrend_distance_pct", percentDistance(lastClose, lastPoint.value), lastPoint.value != 0)
	setValue(values, "alphatrend_slope_pct", percentDistance(lastPoint.value, prevPoint.value), prevPoint.value != 0)
	signals["alphatrend_direction"] = lastPoint.direction
	signals["alphatrend_flip"] = trendFlip(prevPoint.direction, lastPoint.direction)
	cross, signal := alphaTrendSignals(points)
	signals["alphatrend_cross"] = cross
	signals["alphatrend_signal"] = signal
}

func addPSARFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	value, direction, ok := psar(highs, lows, closes, 0.02, 0.2)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "psar", value, true)
	setValue(values, "psar_distance_pct", percentDistance(last, value), value != 0)
	signals["psar_direction"] = direction
}

func addChandelierExit(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	longStop, shortStop, ok := chandelierExit(highs, lows, closes, period, multiplier)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValue(values, "chandelier_long", longStop, true)
	setValue(values, "chandelier_short", shortStop, true)
	switch {
	case last >= longStop:
		signals["chandelier_direction"] = "up"
		setValue(values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, longStop)), longStop != 0)
	case last <= shortStop:
		signals["chandelier_direction"] = "down"
		setValue(values, "chandelier_stop_distance_pct", absFloat(percentDistance(last, shortStop)), shortStop != 0)
	default:
		signals["chandelier_direction"] = "neutral"
	}
}

func chandelierExit(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, float64, bool) {
	if period <= 0 || len(closes) < period || multiplier <= 0 {
		return 0, 0, false
	}
	atrValue, ok := atr(highs, lows, closes, period)
	if !ok {
		return 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	return highest - multiplier*atrValue, lowest + multiplier*atrValue, true
}

func supertrend(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, bool) {
	points, ok := supertrendSeries(highs, lows, closes, period, multiplier)
	if !ok {
		return 0, "", false
	}
	last := points[len(points)-1]
	return last.value, last.direction, true
}

type trendPoint struct {
	value     float64
	direction string
}

func supertrendSeries(highs []float64, lows []float64, closes []float64, period int, multiplier float64) ([]trendPoint, bool) {
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
			finalUpper[index] = basicUpper
			finalLower[index] = basicLower
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
		switch {
		case direction[index-1] == "down" && closes[index] > finalUpper[index]:
			direction[index] = "up"
		case direction[index-1] == "up" && closes[index] < finalLower[index]:
			direction[index] = "down"
		default:
			direction[index] = direction[index-1]
		}
	}
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		if direction[index] == "down" {
			points = append(points, trendPoint{value: finalUpper[index], direction: "down"})
			continue
		}
		points = append(points, trendPoint{value: finalLower[index], direction: "up"})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}

func alphaTrend(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) (float64, float64, string, bool) {
	points, lastMFI, ok := alphaTrendSeries(highs, lows, closes, volumes, period, multiplier)
	if !ok {
		return 0, 0, "", false
	}
	last := points[len(points)-1]
	return last.value, lastMFI, last.direction, true
}

func alphaTrendSeries(highs []float64, lows []float64, closes []float64, volumes []float64, period int, multiplier float64) ([]trendPoint, float64, bool) {
	if period <= 0 || len(closes) <= period || len(volumes) != len(closes) {
		return nil, 0, false
	}
	trs := trueRanges(highs, lows, closes)
	if len(trs) < period {
		return nil, 0, false
	}
	trend := make([]float64, len(closes))
	directions := make([]string, len(closes))
	for index := period; index < len(closes); index++ {
		atrValue, _ := sma(trs[index-period:index], period)
		mfi := moneyFlowIndex(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1], period)
		up := lows[index] - multiplier*atrValue
		down := highs[index] + multiplier*atrValue
		if index == period {
			if mfi >= 50 {
				trend[index] = up
				directions[index] = "up"
			} else {
				trend[index] = down
				directions[index] = "down"
			}
			continue
		}
		if mfi >= 50 {
			if up < trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = up
			}
			directions[index] = "up"
		} else {
			if down > trend[index-1] {
				trend[index] = trend[index-1]
			} else {
				trend[index] = down
			}
			directions[index] = "down"
		}
	}
	lastMFI := moneyFlowIndex(highs, lows, closes, volumes, period)
	points := make([]trendPoint, 0, len(closes)-period)
	for index := period; index < len(closes); index++ {
		points = append(points, trendPoint{value: trend[index], direction: directions[index]})
	}
	if len(points) < 2 {
		return nil, 0, false
	}
	return points, lastMFI, true
}

func alphaTrendSignals(points []trendPoint) (string, string) {
	if len(points) < 4 {
		return "none", "none"
	}
	buys := make([]bool, len(points))
	sells := make([]bool, len(points))
	for index := 3; index < len(points); index++ {
		current := points[index].value
		twoBack := points[index-2].value
		previous := points[index-1].value
		threeBack := points[index-3].value
		buys[index] = current > twoBack && previous <= threeBack
		sells[index] = current < twoBack && previous >= threeBack
	}

	last := len(points) - 1
	cross := "none"
	signal := "none"
	if buys[last] {
		cross = "buy"
		if alphaTrendSignalAllowed(previousCrossDistance(buys, last), crossDistance(sells, last)) {
			signal = "buy"
		}
	}
	if sells[last] {
		cross = "sell"
		if alphaTrendSignalAllowed(previousCrossDistance(sells, last), crossDistance(buys, last)) {
			signal = "sell"
		}
	}
	return cross, signal
}

func alphaTrendSignalAllowed(previousSame int, opposite int) bool {
	return previousSame >= 0 && opposite >= 0 && previousSame > opposite
}

type livermoreState struct {
	trend    int
	a        float64
	b        float64
	c        float64
	d        float64
	e        float64
	f        float64
	g        float64
	h        float64
	hasA     bool
	hasB     bool
	hasC     bool
	hasD     bool
	hasE     bool
	hasF     bool
	hasG     bool
	hasH     bool
	buy      bool
	sell     bool
	keyPoint float64
}

func addLivermoreFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, opens []float64) {
	state, ok := livermoreStructure(highs, lows, closes, opens, 365, 4)
	if !ok {
		return
	}
	if state.hasC {
		setValue(values, "livermore_key_point", state.c, true)
	}
	if state.hasF {
		setValue(values, "livermore_pullback_point", state.f, true)
	}
	if state.hasG {
		setValue(values, "livermore_breakout_line", state.g, true)
	}
	if state.hasH {
		setValue(values, "livermore_previous_key_point", state.h, true)
	}
	if state.keyPoint != 0 {
		setValue(values, "livermore_active_point", state.keyPoint, true)
	}
	switch state.trend {
	case 1:
		signals["livermore_trend"] = "up"
	case 2:
		signals["livermore_trend"] = "down"
	default:
		signals["livermore_trend"] = "range"
	}
	switch {
	case state.buy:
		signals["livermore_signal"] = "buy"
	case state.sell:
		signals["livermore_signal"] = "sell"
	default:
		signals["livermore_signal"] = "none"
	}
}

func livermoreStructure(highs []float64, lows []float64, closes []float64, opens []float64, period int, multiplier float64) (livermoreState, bool) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok || len(opens) != len(closes) {
		return livermoreState{}, false
	}
	offset := len(closes) - len(atrValues)
	var state livermoreState
	for atrIndex, atrValue := range atrValues {
		index := atrIndex + offset
		da := atrValue * multiplier
		db := da * 0.5
		previousTrend := state.trend
		if state.trend == 0 {
			if closes[index] > opens[index] {
				state.trend = 1
				state.a = highs[index]
				state.d = state.a - da
			} else {
				state.trend = 2
				state.a = lows[index]
				state.d = state.a + da
			}
			state.hasA = true
			state.hasD = true
			state.keyPoint = state.a
			continue
		}
		state.buy = false
		state.sell = false
		if state.trend == 1 {
			updateLivermoreUp(&state, highs[index], lows[index], da, db)
		} else if state.trend == 2 {
			updateLivermoreDown(&state, highs[index], lows[index], da, db)
		}
		state.buy = state.trend == 1 && previousTrend == 2
		state.sell = state.trend == 2 && previousTrend == 1
		if state.hasA {
			state.keyPoint = state.a
		}
	}
	return state, state.trend != 0
}

func updateLivermoreUp(state *livermoreState, high float64, low float64, da float64, db float64) {
	if high > state.a {
		state.a = high
		state.b = state.a - da
		state.hasA = true
		state.hasB = true
		if !state.hasC || high > state.c {
			state.hasC = false
		}
	} else if state.hasB && low < state.b {
		state.hasB = false
		state.c = state.a
		state.d = low
		state.e = state.d + da
		state.hasC = true
		state.hasD = true
		state.hasE = true
	}
	if !state.hasD || low < state.d {
		state.d = low
		state.e = state.d + da
		state.hasD = true
		state.hasE = true
	} else if state.hasE && high > state.e {
		state.hasE = false
		state.f = state.d
		state.g = state.d - db
		state.hasF = true
		state.hasG = true
		if state.hasH && state.g > state.h {
			state.hasH = false
		}
	}
	if (state.hasG && low < state.g) || (state.hasH && low < state.h) {
		state.trend = 2
		if state.hasC {
			state.h = state.c
			state.hasH = true
		}
		state.a = low
		state.b = state.a + da
		state.hasA = true
		state.hasB = true
		state.hasC = false
		state.hasD = false
		state.hasF = false
		state.hasG = false
	}
}

func updateLivermoreDown(state *livermoreState, high float64, low float64, da float64, db float64) {
	if low < state.a {
		state.a = low
		state.b = state.a + da
		state.hasA = true
		state.hasB = true
		if !state.hasC || low < state.c {
			state.hasC = false
		}
	} else if state.hasB && high > state.b {
		state.hasB = false
		state.c = state.a
		state.d = high
		state.e = state.d - da
		state.hasC = true
		state.hasD = true
		state.hasE = true
	}
	if !state.hasD || high > state.d {
		state.d = high
		state.e = state.d - da
		state.hasD = true
		state.hasE = true
	} else if state.hasE && low < state.e {
		state.hasE = false
		state.f = state.d
		state.g = state.d + db
		state.hasF = true
		state.hasG = true
		if state.hasH && state.g < state.h {
			state.hasH = false
		}
	}
	if (state.hasG && high > state.g) || (state.hasH && high > state.h) {
		state.trend = 1
		if state.hasC {
			state.h = state.c
			state.hasH = true
		}
		state.a = high
		state.b = state.a - da
		state.hasA = true
		state.hasB = true
		state.hasC = false
		state.hasD = false
		state.hasF = false
		state.hasG = false
	}
}

func crossDistance(series []bool, current int) int {
	for index := current; index >= 0; index-- {
		if series[index] {
			return current - index
		}
	}
	return -1
}

func previousCrossDistance(series []bool, current int) int {
	for index := current - 1; index >= 0; index-- {
		if series[index] {
			return current - index - 1
		}
	}
	return -1
}

func psar(highs []float64, lows []float64, closes []float64, step float64, maxStep float64) (float64, string, bool) {
	if len(closes) < 3 || step <= 0 || maxStep < step {
		return 0, "", false
	}
	uptrend := closes[1] >= closes[0]
	sar := lows[0]
	ep := highs[0]
	if !uptrend {
		sar = highs[0]
		ep = lows[0]
	}
	acceleration := step
	for index := 1; index < len(closes); index++ {
		sar = sar + acceleration*(ep-sar)
		if uptrend {
			if index >= 2 {
				sar = minFloat(sar, lows[index-1], lows[index-2])
			}
			if lows[index] < sar {
				uptrend = false
				sar = ep
				ep = lows[index]
				acceleration = step
				continue
			}
			if highs[index] > ep {
				ep = highs[index]
				acceleration = minFloat(acceleration+step, maxStep)
			}
			continue
		}
		if index >= 2 {
			sar = maxFloat(sar, highs[index-1], highs[index-2])
		}
		if highs[index] > sar {
			uptrend = true
			sar = ep
			ep = highs[index]
			acceleration = step
			continue
		}
		if lows[index] < ep {
			ep = lows[index]
			acceleration = minFloat(acceleration+step, maxStep)
		}
	}
	if uptrend {
		return sar, "up", true
	}
	return sar, "down", true
}

func minFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value < result {
			result = value
		}
	}
	return result
}

func maxFloat(first float64, values ...float64) float64 {
	result := first
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func trendFlip(previous string, current string) string {
	if previous == "" || current == "" || previous == current {
		return "none"
	}
	return current
}
