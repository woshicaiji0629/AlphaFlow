package indicatorcalc

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
	addLivermoreFeaturesToSet(nil, values, signals, highs, lows, closes, opens)
}

func addLivermoreFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, opens []float64) {
	state, ok := livermoreStructure(highs, lows, closes, opens, 365, 4)
	if !ok {
		return
	}
	if state.hasC {
		setValueTarget(target, values, "livermore_key_point", state.c, true)
	}
	if state.hasF {
		setValueTarget(target, values, "livermore_pullback_point", state.f, true)
	}
	if state.hasG {
		setValueTarget(target, values, "livermore_breakout_line", state.g, true)
	}
	if state.hasH {
		setValueTarget(target, values, "livermore_previous_key_point", state.h, true)
	}
	if state.keyPoint != 0 {
		setValueTarget(target, values, "livermore_active_point", state.keyPoint, true)
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
