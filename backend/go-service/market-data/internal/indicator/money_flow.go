package indicator

import "math"

func addMoneyFlowFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, volumes []float64) {
	if len(closes) < 20 || len(volumes) != len(closes) {
		return
	}
	last := len(closes) - 1
	mfi := moneyFlowIndex(highs, lows, closes, volumes, 14)
	setValue(values, "mfi14", mfi, true)

	vwapValue := vwap(highs, lows, closes, volumes)
	setValue(values, "vwap_distance_pct", percentDistance(closes[last], vwapValue), vwapValue != 0)

	obvSeries := obvSeries(closes, volumes)
	obvSlope := slope(obvSeries, 5)
	setValue(values, "obv_slope5", obvSlope, len(obvSeries) >= 5)

	volumeZScore, ok := zScore(volumes, 20)
	setValue(values, "volume_zscore20", volumeZScore, ok)
	pressure := volumePressure(closes, volumes, 20)
	setValue(values, "volume_pressure20", pressure, true)

	pvtSeries := priceVolumeTrendSeries(closes, volumes)
	pvt := pvtSeries[len(pvtSeries)-1]
	setValue(values, "price_volume_trend", pvt, true)

	signals["money_flow"] = moneyFlowSignal(mfi, pressure)
	signals["volume_state"] = volumeState(volumeZScore, ok)
	signals["price_volume_confirmation"] = priceVolumeConfirmation(closes, obvSeries, pvtSeries)
}

func moneyFlowIndex(highs []float64, lows []float64, closes []float64, volumes []float64, period int) float64 {
	if len(closes) <= period {
		return 50
	}
	var positive float64
	var negative float64
	for index := len(closes) - period; index < len(closes); index++ {
		current := (highs[index] + lows[index] + closes[index]) / 3
		previous := (highs[index-1] + lows[index-1] + closes[index-1]) / 3
		flow := current * volumes[index]
		if current >= previous {
			positive += flow
		} else {
			negative += flow
		}
	}
	if negative == 0 {
		return 100
	}
	ratio := positive / negative
	return 100 - 100/(1+ratio)
}

func obvSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		switch {
		case closes[index] > closes[index-1]:
			values[index] += volumes[index]
		case closes[index] < closes[index-1]:
			values[index] -= volumes[index]
		}
	}
	return values
}

func priceVolumeTrendSeries(closes []float64, volumes []float64) []float64 {
	values := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		values[index] = values[index-1]
		if closes[index-1] != 0 {
			values[index] += (closes[index] - closes[index-1]) / closes[index-1] * volumes[index]
		}
	}
	return values
}

func volumePressure(closes []float64, volumes []float64, period int) float64 {
	if period <= 0 || len(closes) < period {
		return 0
	}
	start := len(closes) - period
	var upVolume float64
	var downVolume float64
	for index := start; index < len(closes); index++ {
		switch {
		case index > 0 && closes[index] > closes[index-1]:
			upVolume += volumes[index]
		case index > 0 && closes[index] < closes[index-1]:
			downVolume += volumes[index]
		}
	}
	total := upVolume + downVolume
	if total == 0 {
		return 0
	}
	return (upVolume - downVolume) / total
}

func zScore(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	window := values[len(values)-period:]
	mean := sum(window) / float64(period)
	var variance float64
	for _, value := range window {
		diff := value - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(period))
	if stddev == 0 {
		return 0, true
	}
	return (values[len(values)-1] - mean) / stddev, true
}

func slope(values []float64, period int) float64 {
	if period <= 1 || len(values) < period {
		return 0
	}
	window := values[len(values)-period:]
	return window[len(window)-1] - window[0]
}

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
	case zscore >= 2:
		return "spike"
	case zscore <= -1:
		return "dry"
	default:
		return "normal"
	}
}

func priceVolumeConfirmation(closes []float64, obvValues []float64, pvtValues []float64) string {
	if len(closes) < 20 || len(obvValues) != len(closes) || len(pvtValues) != len(closes) {
		return "neutral"
	}
	priceSlope := slope(closes, 5)
	obvSlope := slope(obvValues, 5)
	pvtSlope := slope(pvtValues, 5)
	last := len(closes) - 1
	recentHigh, recentLow := highLow(closes[last-19:last], closes[last-19:last])
	switch {
	case closes[last] > recentHigh && (obvSlope < 0 || pvtSlope < 0):
		return "divergence_bear"
	case closes[last] < recentLow && (obvSlope > 0 || pvtSlope > 0):
		return "divergence_bull"
	case priceSlope > 0 && obvSlope > 0 && pvtSlope > 0:
		return "confirm_up"
	case priceSlope < 0 && obvSlope < 0 && pvtSlope < 0:
		return "confirm_down"
	default:
		return "neutral"
	}
}
