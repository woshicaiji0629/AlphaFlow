package marketstructure

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
)

func marketRowsAndForwards(observations []signalresearch.MarketStructureObservation) ([]signalresearch.MarketStructureObservation, map[int64]map[int]signalresearch.ForwardMetrics) {
	rows := make([]signalresearch.MarketStructureObservation, 0, len(observations)/3)
	forwards := map[int64]map[int]signalresearch.ForwardMetrics{}
	for _, observation := range observations {
		asOfMS := observation.Features.AsOfMS
		if _, ok := forwards[asOfMS]; !ok {
			forwards[asOfMS] = map[int]signalresearch.ForwardMetrics{}
		}
		forwards[asOfMS][observation.Forward.HorizonBars] = observation.Forward
		if observation.Forward.HorizonBars == 80 {
			rows = append(rows, observation)
		}
	}
	return rows, forwards
}

func directionalPath(forward signalresearch.ForwardMetrics, direction int) (float64, float64, float64) {
	if direction > 0 {
		return forward.MaxUpsideBps, forward.MaxDownsideBps, forward.DirectionReturnBps
	}
	return forward.MaxDownsideBps, forward.MaxUpsideBps, -forward.DirectionReturnBps
}

func percentile(values []float64, quantile float64) float64 {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	return ordered[max(0, int(math.Ceil(quantile*float64(len(ordered))))-1)]
}

type rawPathBar struct {
	open, high, low, close, volume float64
}

func extractRawPathFeatures(asOfMS int64, threeMinute []marketmodel.Kline, threeIndex int, fifteenMinute []marketmodel.Kline, fifteenIndex int) (map[string]float64, error) {
	result := map[string]float64{}
	for _, input := range []struct {
		prefix string
		bars   []marketmodel.Kline
		index  int
	}{{"3m", threeMinute, threeIndex}, {"15m", fifteenMinute, fifteenIndex}} {
		features, err := extractRawIntervalFeatures(asOfMS, input.prefix, input.bars, input.index)
		if err != nil {
			return nil, err
		}
		for name, value := range features {
			result[name] = value
		}
	}
	return result, nil
}

func extractRawIntervalFeatures(asOfMS int64, prefix string, bars []marketmodel.Kline, index int) (map[string]float64, error) {
	if index < 40 || index >= len(bars) {
		return nil, fmt.Errorf("%s raw path requires 40 prior bars at index=%d", prefix, index)
	}
	parsed := make([]rawPathBar, 41)
	for offset := 0; offset <= 40; offset++ {
		bar := bars[index-40+offset]
		if !bar.IsClosed || bar.CloseTime > asOfMS || (offset > 0 && bar.CloseTime <= bars[index-40+offset-1].CloseTime) {
			return nil, fmt.Errorf("%s raw path contains unavailable or unordered bar at offset=%d", prefix, offset)
		}
		values := []*float64{&parsed[offset].open, &parsed[offset].high, &parsed[offset].low, &parsed[offset].close, &parsed[offset].volume}
		texts := []string{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume}
		for field := range values {
			value, err := strconv.ParseFloat(texts[field], 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || (field < 4 && value == 0) {
				return nil, fmt.Errorf("%s raw path invalid field=%d at offset=%d", prefix, field, offset)
			}
			*values[field] = value
		}
		if parsed[offset].low > parsed[offset].high || parsed[offset].close < parsed[offset].low || parsed[offset].close > parsed[offset].high {
			return nil, fmt.Errorf("%s raw path inconsistent prices at offset=%d", prefix, offset)
		}
	}
	result := map[string]float64{}
	for _, window := range []int{5, 10, 20} {
		result[fmt.Sprintf("%s.return_%d_bps", prefix, window)] = (parsed[40].close/parsed[40-window].close - 1) * 10000
	}
	for _, window := range []int{10, 20} {
		path := 0.0
		for index := 40 - window + 1; index <= 40; index++ {
			path += math.Abs(parsed[index].close - parsed[index-1].close)
		}
		result[fmt.Sprintf("%s.path_efficiency_%d", prefix, window)] = math.Abs(parsed[40].close-parsed[40-window].close) / math.Max(path, 1e-12)
	}
	upBars, downBars, signedVolume, totalVolume := 0, 0, 0.0, 0.0
	for index := 21; index <= 40; index++ {
		sign := 0.0
		if parsed[index].close > parsed[index].open {
			upBars, sign = upBars+1, 1
		} else if parsed[index].close < parsed[index].open {
			downBars, sign = downBars+1, -1
		}
		signedVolume += sign * parsed[index].volume
		totalVolume += parsed[index].volume
	}
	result[prefix+".direction_balance_20"] = float64(upBars-downBars) / 20
	result[prefix+".volume_direction_balance_20"] = signedVolume / math.Max(totalVolume, 1e-12)
	for _, window := range []int{20, 40} {
		high, low := parsed[40-window+1].high, parsed[40-window+1].low
		for index := 40 - window + 2; index <= 40; index++ {
			high, low = math.Max(high, parsed[index].high), math.Min(low, parsed[index].low)
		}
		result[fmt.Sprintf("%s.range_position_%d", prefix, window)] = (parsed[40].close - low) / math.Max(high-low, 1e-12)
		if window == 20 {
			result[prefix+".range_width_20_bps"] = (high - low) / parsed[40].close * 10000
		}
	}
	averageRange := func(from int) float64 {
		total := 0.0
		for index := from; index <= 40; index++ {
			total += (parsed[index].high - parsed[index].low) / parsed[index-1].close * 10000
		}
		return total / float64(41-from)
	}
	result[prefix+".range_expansion_5_20"] = averageRange(36) / math.Max(averageRange(21), 1e-12)
	averageVolume := func(from int) float64 {
		total := 0.0
		for index := from; index <= 40; index++ {
			total += parsed[index].volume
		}
		return total / float64(41-from)
	}
	result[prefix+".volume_ratio_5_20"] = averageVolume(36) / math.Max(averageVolume(21), 1e-12)
	priorHigh, priorLow := parsed[20].high, parsed[20].low
	for index := 21; index < 40; index++ {
		priorHigh, priorLow = math.Max(priorHigh, parsed[index].high), math.Min(priorLow, parsed[index].low)
	}
	if parsed[40].close > priorHigh {
		result[prefix+".breakout_distance_20_bps"] = (parsed[40].close/priorHigh - 1) * 10000
	} else if parsed[40].close < priorLow {
		result[prefix+".breakout_distance_20_bps"] = -(priorLow/parsed[40].close - 1) * 10000
	} else {
		result[prefix+".breakout_distance_20_bps"] = 0
	}
	result[prefix+".close"], result[prefix+".prior_high_20"], result[prefix+".prior_low_20"] = parsed[40].close, priorHigh, priorLow
	return result, nil
}
