package indicatorcalc

import model "alphaflow/go-service/pkg/marketmodel"

func requiredSampleCount(options Options) int {
	required := 1
	for _, period := range options.SMAPeriods {
		if period > required {
			required = period
		}
	}
	for _, period := range options.EMAPeriods {
		if period > required {
			required = period
		}
	}
	for _, period := range options.WMAPeriods {
		if period > required {
			required = period
		}
	}
	return required
}

func assessDataQuality(klines []model.Kline, requiredSamples int) (string, string) {
	hasGap := false
	hasZeroVolume := false
	for index, kline := range klines {
		open, err := parse(kline.Open)
		if err != nil {
			return dataQualityInvalidOHLC, "invalid_open"
		}
		high, err := parse(kline.High)
		if err != nil {
			return dataQualityInvalidOHLC, "invalid_high"
		}
		low, err := parse(kline.Low)
		if err != nil {
			return dataQualityInvalidOHLC, "invalid_low"
		}
		closeValue, err := parse(kline.Close)
		if err != nil {
			return dataQualityInvalidOHLC, "invalid_close"
		}
		volume, err := parse(kline.Volume)
		if err != nil {
			return dataQualityInvalidOHLC, "invalid_volume"
		}
		if high < low || open > high || open < low || closeValue > high || closeValue < low {
			return dataQualityInvalidOHLC, "price_out_of_range"
		}
		if kline.CloseTime > 0 && kline.CloseTime < kline.OpenTime {
			return dataQualityInvalidOHLC, "invalid_time_range"
		}
		if volume == 0 {
			hasZeroVolume = true
		}
		if index > 0 {
			previous := klines[index-1]
			if previous.CloseTime > 0 && kline.OpenTime != previous.CloseTime+1 {
				hasGap = true
			}
		}
	}
	switch {
	case hasGap:
		return dataQualityGap, "non_contiguous_klines"
	case hasZeroVolume:
		return dataQualityZeroVolume, "zero_volume"
	case len(klines) < requiredSamples:
		return dataQualityInsufficient, "insufficient_samples"
	default:
		return dataQualityOK, ""
	}
}

func assessDataQualityFromSeries(
	klines []model.Kline,
	requiredSamples int,
	opens []float64,
	highs []float64,
	lows []float64,
	closes []float64,
	volumes []float64,
) (string, string) {
	hasGap := false
	hasZeroVolume := false
	for index, kline := range klines {
		if highs[index] < lows[index] ||
			opens[index] > highs[index] ||
			opens[index] < lows[index] ||
			closes[index] > highs[index] ||
			closes[index] < lows[index] {
			return dataQualityInvalidOHLC, "price_out_of_range"
		}
		if kline.CloseTime > 0 && kline.CloseTime < kline.OpenTime {
			return dataQualityInvalidOHLC, "invalid_time_range"
		}
		if volumes[index] == 0 {
			hasZeroVolume = true
		}
		if index > 0 {
			previous := klines[index-1]
			if previous.CloseTime > 0 && kline.OpenTime != previous.CloseTime+1 {
				hasGap = true
			}
		}
	}
	switch {
	case hasGap:
		return dataQualityGap, "non_contiguous_klines"
	case hasZeroVolume:
		return dataQualityZeroVolume, "zero_volume"
	case len(klines) < requiredSamples:
		return dataQualityInsufficient, "insufficient_samples"
	default:
		return dataQualityOK, ""
	}
}
