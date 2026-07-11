package indicatorcalc

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	model "alphaflow/go-service/pkg/marketmodel"
)

type Result struct {
	OpenTime  int64
	CloseTime int64
	Values    map[string]string
	Signals   map[string]string
}

type Options struct {
	SMAPeriods []int
	EMAPeriods []int
	WMAPeriods []int
}

const (
	dataQualityOK           = "ok"
	dataQualityInsufficient = "insufficient"
	dataQualityGap          = "gap"
	dataQualityInvalidOHLC  = "invalid_ohlc"
	dataQualityZeroVolume   = "zero_volume"
	resultValuesCapacity    = 300
	resultSignalsCapacity   = 160
)

func DefaultOptions() Options {
	return Options{
		SMAPeriods: []int{7, 25, 99},
		EMAPeriods: []int{7, 25, 99},
		WMAPeriods: []int{7, 25, 99},
	}
}

func Calculate(klines []model.Kline, options Options) (Result, error) {
	window := NewCalculationWindowFromKlines(klines, 0)
	return CalculateWindow(window, options)
}

func CalculateWindows(klines []model.Kline, start int, warmup int, options Options) ([]Result, error) {
	return CalculateWindowsContext(context.Background(), klines, start, warmup, options, nil)
}

// CalculateWindowsContext calculates a result for every kline from start while
// keeping only warmup bars in the rolling calculation window. progress is
// called after each completed result when it is not nil.
func CalculateWindowsContext(
	ctx context.Context,
	klines []model.Kline,
	start int,
	warmup int,
	options Options,
	progress func(processed int, total int),
) ([]Result, error) {
	if start < 0 || start > len(klines) {
		return nil, fmt.Errorf("invalid calculation start: %d", start)
	}
	if start == len(klines) {
		return nil, nil
	}
	if warmup <= 0 || warmup > len(klines) {
		warmup = len(klines)
	}
	seedStart := start - (warmup - 1)
	if seedStart < 0 {
		seedStart = 0
	}
	window := NewCalculationWindowFromKlines(klines[seedStart:start], warmup)
	window.EnableBasicState()
	results := make([]Result, 0, len(klines)-start)
	total := len(klines) - start
	for index := start; index < len(klines); index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		window.Append([]model.Kline{klines[index]})
		result, err := CalculateWindow(window, options)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		if progress != nil {
			progress(len(results), total)
		}
	}
	return results, nil
}

func CalculateWindow(window *CalculationWindow, options Options) (Result, error) {
	if window == nil {
		return Result{}, fmt.Errorf("nil calculation window")
	}
	closed := window.Klines()
	if len(closed) == 0 {
		return Result{}, fmt.Errorf("no closed klines")
	}
	if len(options.SMAPeriods) == 0 && len(options.EMAPeriods) == 0 && len(options.WMAPeriods) == 0 {
		options = DefaultOptions()
	}

	values := make(map[string]string, resultValuesCapacity)
	signals := make(map[string]string, resultSignalsCapacity)
	requiredSamples := requiredSampleCount(options)
	values["sample_count"] = strconv.Itoa(len(closed))
	values["required_count"] = strconv.Itoa(requiredSamples)
	opens, highs, lows, closes, volumes, err := window.Series()
	if err != nil {
		quality, reason := assessDataQuality(closed, requiredSamples)
		signals["data_quality"] = quality
		if reason != "" {
			signals["data_quality_reason"] = reason
		}
		if quality == dataQualityInvalidOHLC {
			last := closed[len(closed)-1]
			return Result{
				OpenTime:  last.OpenTime,
				CloseTime: last.CloseTime,
				Values:    values,
				Signals:   signals,
			}, nil
		}
		return Result{}, err
	}
	quality, reason := assessDataQualityFromSeries(closed, requiredSamples, opens, highs, lows, closes, volumes)
	signals["data_quality"] = quality
	if reason != "" {
		signals["data_quality_reason"] = reason
	}
	last := closed[len(closed)-1]
	if quality == dataQualityInvalidOHLC {
		return Result{
			OpenTime:  last.OpenTime,
			CloseTime: last.CloseTime,
			Values:    values,
			Signals:   signals,
		}, nil
	}
	basic := window.basic
	if window.aiPreview == nil {
		window.prepareAISourcePrefix()
	}
	features := newFeatureContext(highs, lows, closes, basic)

	for _, period := range options.SMAPeriods {
		value, ok := basic.sma(period)
		if !ok {
			value, ok = sma(closes, period)
		}
		setValue(values, fmt.Sprintf("sma%d", period), value, ok)
	}
	for _, period := range options.EMAPeriods {
		value, ok := basic.emaValue(period)
		if !ok {
			value, ok = ema(closes, period)
		}
		setValue(values, fmt.Sprintf("ema%d", period), value, ok)
	}
	for _, period := range options.WMAPeriods {
		value, ok := wma(closes, period)
		setValue(values, fmt.Sprintf("wma%d", period), value, ok)
	}
	addMovingAverageFeatures(values, signals, closes, volumes, basic)

	rsi14Series, ok := basic.rsiSeries14()
	if !ok {
		rsi14Series, _ = rsiSeries(closes, 14)
	}
	addRSIFeaturesFromSeries(values, signals, closes, rsi14Series)
	if series, ok := basic.macdSeries(macdConfig{fast: 12, slow: 26, signal: 9}); ok {
		addMACDSeriesFeatures(values, signals, closes, series, "macd")
	} else if series, ok := macdSeries(closes, 12, 26, 9); ok {
		addMACDSeriesFeatures(values, signals, closes, series, "macd")
	}
	if series, ok := basic.macdSeries(macdConfig{fast: 7, slow: 19, signal: 9}); ok {
		addMACDSeriesFeatures(values, signals, closes, series, "macd_fast")
	} else if series, ok := macdSeries(closes, 7, 19, 9); ok {
		addMACDSeriesFeatures(values, signals, closes, series, "macd_fast")
	}
	addOscillatorFeaturesWithRSI(values, signals, highs, lows, closes, rsi14Series, basic)
	if atr14Series, ok := features.atrSeries(14); ok {
		addVolatilityCoreFeaturesWithATR(values, signals, highs, lows, closes, 14, atr14Series, basic)
	} else {
		addVolatilityCoreFeatures(values, signals, highs, lows, closes, 14)
	}
	upper, middle, lower, ok := features.bollinger(20, 2)
	if ok {
		setValue(values, "bb_upper", upper, true)
		setValue(values, "bb_middle", middle, true)
		setValue(values, "bb_lower", lower, true)
	}
	volumeMA, ok := basic.volumeSMAValue(20)
	if !ok {
		volumeMA, ok = sma(volumes, 20)
	}
	setValue(values, "volume_ma20", volumeMA, ok)
	if obvValue, ok := basic.obvValue(); ok {
		setValue(values, "obv", obvValue, true)
	} else {
		setValue(values, "obv", obv(closes, volumes), true)
	}
	donchianHigh, donchianLow, ok := features.donchian(20)
	if ok {
		setValue(values, "donchian_high20", donchianHigh, true)
		setValue(values, "donchian_low20", donchianLow, true)
	}
	if vwapValue, ok := basic.vwapValue(closes[len(closes)-1]); ok {
		setValue(values, "vwap", vwapValue, true)
	} else {
		setValue(values, "vwap", vwap(highs, lows, closes, volumes), true)
	}
	addDerived(values, opens, highs, lows, closes, volumes)
	addEnhanced(values, signals, opens, highs, lows, closes, volumes, basic, features, window.aiPreview)

	return Result{
		OpenTime:  last.OpenTime,
		CloseTime: last.CloseTime,
		Values:    values,
		Signals:   signals,
	}, nil
}

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

func sma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	var sum float64
	for _, value := range values[len(values)-period:] {
		sum += value
	}
	return sum / float64(period), true
}

func ema(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	seed, _ := sma(values[:period], period)
	multiplier := 2 / float64(period+1)
	result := seed
	for _, value := range values[period:] {
		result = (value-result)*multiplier + result
	}
	return result, true
}

func emaSeries(values []float64, period int) ([]float64, bool) {
	if period <= 0 || len(values) < period {
		return nil, false
	}
	result := make([]float64, 0, len(values)-period+1)
	seed, _ := sma(values[:period], period)
	result = append(result, seed)
	multiplier := 2 / float64(period+1)
	current := seed
	for _, value := range values[period:] {
		current = (value-current)*multiplier + current
		result = append(result, current)
	}
	return result, true
}

func wma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	var weighted float64
	var weightSum float64
	window := values[len(values)-period:]
	for index, value := range window {
		weight := float64(index + 1)
		weighted += value * weight
		weightSum += weight
	}
	return weighted / weightSum, true
}

func bollinger(values []float64, period int, multiplier float64) (float64, float64, float64, bool) {
	middle, ok := sma(values, period)
	if !ok {
		return 0, 0, 0, false
	}
	window := values[len(values)-period:]
	var variance float64
	for _, value := range window {
		diff := value - middle
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(period))
	return middle + multiplier*stddev, middle, middle - multiplier*stddev, true
}

func obv(closes []float64, volumes []float64) float64 {
	var result float64
	for index := 1; index < len(closes); index++ {
		switch {
		case closes[index] > closes[index-1]:
			result += volumes[index]
		case closes[index] < closes[index-1]:
			result -= volumes[index]
		}
	}
	return result
}

func donchian(highs []float64, lows []float64, period int) (float64, float64, bool) {
	if len(highs) < period || len(lows) < period {
		return 0, 0, false
	}
	highest, lowest := highLow(highs[len(highs)-period:], lows[len(lows)-period:])
	return highest, lowest, true
}

func vwap(highs []float64, lows []float64, closes []float64, volumes []float64) float64 {
	var weighted float64
	var volumeSum float64
	for index := range closes {
		typical := (highs[index] + lows[index] + closes[index]) / 3
		weighted += typical * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return closes[len(closes)-1]
	}
	return weighted / volumeSum
}

func addDerived(values map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64) {
	last := len(closes) - 1
	open := opens[last]
	high := highs[last]
	low := lows[last]
	closeValue := closes[last]
	if open != 0 {
		setValue(values, "change_pct", (closeValue-open)/open*100, true)
	}
	if closeValue != 0 {
		setValue(values, "amplitude_pct", (high-low)/closeValue*100, true)
	}
	rangeValue := high - low
	if rangeValue != 0 {
		setValue(values, "body_ratio", math.Abs(closeValue-open)/rangeValue, true)
		setValue(values, "upper_shadow_ratio", (high-math.Max(open, closeValue))/rangeValue, true)
		setValue(values, "lower_shadow_ratio", (math.Min(open, closeValue)-low)/rangeValue, true)
	}
	volumeMA, ok := sma(volumes, 20)
	if ok && volumeMA != 0 {
		setValue(values, "volume_ratio20", volumes[last]/volumeMA, true)
	}
}

func trueRanges(highs []float64, lows []float64, closes []float64) []float64 {
	values := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		values = append(values, math.Max(
			highs[index]-lows[index],
			math.Max(math.Abs(highs[index]-closes[index-1]), math.Abs(lows[index]-closes[index-1])),
		))
	}
	return values
}

func highLow(highs []float64, lows []float64) (float64, float64) {
	highest := highs[0]
	lowest := lows[0]
	for index := range highs {
		if highs[index] > highest {
			highest = highs[index]
		}
		if lows[index] < lowest {
			lowest = lows[index]
		}
	}
	return highest, lowest
}

func setValue(values map[string]string, name string, value float64, ok bool) {
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	values[name] = format(value)
}

func sum(values []float64) float64 {
	var result float64
	for _, value := range values {
		result += value
	}
	return result
}

func parse(value string) (float64, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, nil
	}
	return strconv.ParseFloat(text, 64)
}

func format(value float64) string {
	text := strconv.FormatFloat(value, 'f', 8, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}
