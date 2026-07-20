package indicatorcalc

func moneyFlowSignal(mfi float64, pressure float64) string {
	switch {
	case mfi >= 60 && pressure > 0.15:
		return "inflow"
	case mfi <= 40 && pressure < -0.15:
		return "outflow"
	default:
		return "neutral"
	}
}

func volumeState(zscore float64, ok bool) string {
	if !ok {
		return "normal"
	}
	switch {
	case zscore >= 3:
		return "climax"
	case zscore >= 2:
		return "spike"
	case zscore <= -1.8:
		return "dry_up"
	case zscore <= -1:
		return "dry"
	default:
		return "normal"
	}
}

func cmfState(value float64, ok bool) string {
	if !ok {
		return "neutral"
	}
	switch {
	case value >= 0.05:
		return "inflow"
	case value <= -0.05:
		return "outflow"
	default:
		return "neutral"
	}
}

func priceVolumeAction(closes []float64, volumes []float64, volumeRatio5 float64, ok bool) string {
	if !ok || len(closes) < 6 || len(volumes) < 6 {
		return "neutral"
	}
	priceChange := percentDistance(closes[len(closes)-1], closes[len(closes)-2])
	fiveBarChange := percentDistance(closes[len(closes)-1], closes[len(closes)-6])
	switch {
	case volumeRatio5 >= 1.8 && priceChange > 0:
		return "volume_expansion_up"
	case volumeRatio5 >= 1.8 && priceChange < 0:
		return "volume_expansion_down"
	case volumeRatio5 <= 0.7 && fiveBarChange < 0:
		return "volume_shrink_pullback"
	default:
		return "neutral"
	}
}

func breakoutVolumeConfirm(highs []float64, closes []float64, volumeBreakoutRatio float64, ok bool) string {
	if !ok || len(closes) < 21 {
		return "none"
	}
	previousHigh, _ := highLow(highs[len(highs)-21:len(highs)-1], highs[len(highs)-21:len(highs)-1])
	last := len(closes) - 1
	if closes[last] > previousHigh {
		if volumeBreakoutRatio >= 0.9 {
			return "confirm"
		}
		return "failed"
	}
	return "none"
}

func breakoutVolumeStrength(volumeBreakoutRatio float64, ok bool) string {
	if !ok {
		return "none"
	}
	switch {
	case volumeBreakoutRatio >= 1.5:
		return "strong"
	case volumeBreakoutRatio >= 0.9:
		return "weak"
	default:
		return "none"
	}
}

func volumeDivergence(closes []float64, volumes []float64, period int) string {
	return volumeDivergenceFromScore(volumeDivergenceScore(closes, volumes, period))
}

func volumeDivergenceFromScore(score float64) string {
	switch {
	case score > 0:
		return "bearish"
	case score < 0:
		return "bullish"
	default:
		return "none"
	}
}

func volumeDivergenceScore(closes []float64, volumes []float64, period int) float64 {
	if period <= 0 || len(closes) < period || len(volumes) != len(closes) {
		return 0
	}
	start := len(closes) - period
	priceHigh, priceLow := highLow(closes[start:], closes[start:])
	volumeHigh, volumeLow := highLow(volumes[start:], volumes[start:])
	last := len(closes) - 1
	switch {
	case closes[last] >= priceHigh && volumes[last] < volumeHigh*0.8:
		return 1
	case closes[last] <= priceLow && volumes[last] > volumeLow*1.2:
		return -1
	default:
		return 0
	}
}

func volumePhase(pressure float64, cmf float64, ok bool) string {
	if !ok {
		return "neutral"
	}
	switch {
	case pressure > 0.15 && cmf > 0.05:
		return "accumulation"
	case pressure < -0.15 && cmf < -0.05:
		return "distribution"
	default:
		return "neutral"
	}
}

func priceVolumeConfirmation(closes []float64, obvValues []float64, pvtValues []float64) string {
	if len(closes) < 20 || len(obvValues) != len(closes) || len(pvtValues) != len(closes) {
		return "neutral"
	}
	priceSlope := slope(closes, 5)
	obvSlope := slope(obvValues, 5)
	pvtSlope := slope(pvtValues, 5)
	return priceVolumeConfirmationFromSlopes(closes, obvSlope, pvtSlope, priceSlope)
}

func priceVolumeConfirmationFromSlopes(closes []float64, obvSlope float64, pvtSlope float64, priceSlope ...float64) string {
	if len(closes) < 20 {
		return "neutral"
	}
	currentPriceSlope := 0.0
	if len(priceSlope) > 0 {
		currentPriceSlope = priceSlope[0]
	} else {
		currentPriceSlope = slope(closes, 5)
	}
	last := len(closes) - 1
	recentHigh, recentLow := highLow(closes[last-19:last], closes[last-19:last])
	switch {
	case closes[last] > recentHigh && (obvSlope < 0 || pvtSlope < 0):
		return "divergence_bear"
	case closes[last] < recentLow && (obvSlope > 0 || pvtSlope > 0):
		return "divergence_bull"
	case currentPriceSlope > 0 && obvSlope > 0 && pvtSlope > 0:
		return "confirm_up"
	case currentPriceSlope < 0 && obvSlope < 0 && pvtSlope < 0:
		return "confirm_down"
	default:
		return "neutral"
	}
}
