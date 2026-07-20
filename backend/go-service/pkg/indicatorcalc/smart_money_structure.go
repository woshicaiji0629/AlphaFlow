package indicatorcalc

func detectLiquiditySweep(highs []float64, lows []float64, closes []float64, pivotHighs []priceLevel, pivotLows []priceLevel) (liquiditySweepState, bool) {
	if len(closes) == 0 || len(highs) != len(closes) || len(lows) != len(closes) {
		return liquiditySweepState{}, false
	}
	best := liquiditySweepState{age: len(closes) + 1}
	found := false
	for _, pivot := range pivotHighs {
		broken := false
		for index := pivot.recency + 1; index < len(closes); index++ {
			if !broken {
				if highs[index] > pivot.price && closes[index] < pivot.price {
					best, found = newerLiquiditySweep(best, found, liquiditySweepState{
						kind:   "wick_high",
						level:  pivot.price,
						top:    highs[index],
						bottom: pivot.price,
						age:    len(closes) - 1 - index,
					})
				}
				if closes[index] > pivot.price {
					broken = true
				}
				continue
			}
			if lows[index] < pivot.price && closes[index] > pivot.price {
				best, found = newerLiquiditySweep(best, found, liquiditySweepState{
					kind:   "retest_high",
					level:  pivot.price,
					top:    pivot.price,
					bottom: lows[index],
					age:    len(closes) - 1 - index,
				})
			}
			if closes[index] < pivot.price {
				broken = false
			}
		}
	}
	for _, pivot := range pivotLows {
		broken := false
		for index := pivot.recency + 1; index < len(closes); index++ {
			if !broken {
				if lows[index] < pivot.price && closes[index] > pivot.price {
					best, found = newerLiquiditySweep(best, found, liquiditySweepState{
						kind:   "wick_low",
						level:  pivot.price,
						top:    pivot.price,
						bottom: lows[index],
						age:    len(closes) - 1 - index,
					})
				}
				if closes[index] < pivot.price {
					broken = true
				}
				continue
			}
			if highs[index] > pivot.price && closes[index] < pivot.price {
				best, found = newerLiquiditySweep(best, found, liquiditySweepState{
					kind:   "retest_low",
					level:  pivot.price,
					top:    highs[index],
					bottom: pivot.price,
					age:    len(closes) - 1 - index,
				})
			}
			if closes[index] > pivot.price {
				broken = false
			}
		}
	}
	return best, found
}

func newerLiquiditySweep(current liquiditySweepState, found bool, candidate liquiditySweepState) (liquiditySweepState, bool) {
	if !found || candidate.age < current.age {
		return candidate, true
	}
	return current, found
}

func liquiditySweepIsHigh(kind string) bool {
	return kind == "wick_high" || kind == "retest_high"
}

func liquiditySweepIsLow(kind string) bool {
	return kind == "wick_low" || kind == "retest_low"
}

func recentSwing(levels []priceLevel) (priceLevel, bool) {
	if len(levels) == 0 {
		return priceLevel{}, false
	}
	recent := levels[0]
	for _, level := range levels[1:] {
		if level.recency > recent.recency {
			recent = level
		}
	}
	return recent, true
}

func detectSwingTrend(highs []priceLevel, lows []priceLevel) swingTrend {
	if len(highs) < 2 || len(lows) < 2 {
		return swingTrendRange
	}
	prevHigh, lastHigh := lastTwoSwings(highs)
	prevLow, lastLow := lastTwoSwings(lows)
	switch {
	case lastHigh.price > prevHigh.price && lastLow.price > prevLow.price:
		return swingTrendUp
	case lastHigh.price < prevHigh.price && lastLow.price < prevLow.price:
		return swingTrendDown
	default:
		return swingTrendRange
	}
}

func structureBias(trend swingTrend) string {
	switch trend {
	case swingTrendUp:
		return "bull"
	case swingTrendDown:
		return "bear"
	default:
		return "range"
	}
}

func lastTwoSwings(levels []priceLevel) (priceLevel, priceLevel) {
	first := levels[0]
	second := levels[1]
	if first.recency > second.recency {
		first, second = second, first
	}
	for _, level := range levels[2:] {
		if level.recency > second.recency {
			first = second
			second = level
		} else if level.recency > first.recency {
			first = level
		}
	}
	return first, second
}

func orderBlock(opens []float64, highs []float64, lows []float64, closes []float64, start int, last int, direction string) (float64, float64, bool) {
	if last <= 0 {
		return 0, 0, false
	}
	for index := last - 1; index >= start; index-- {
		switch {
		case direction == "up" && closes[index] < opens[index]:
			return highs[index], lows[index], true
		case direction == "down" && closes[index] > opens[index]:
			return highs[index], lows[index], true
		}
	}
	return highs[last-1], lows[last-1], true
}
