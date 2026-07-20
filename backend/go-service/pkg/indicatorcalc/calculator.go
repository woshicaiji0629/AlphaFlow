package indicatorcalc

import (
	"context"
	"fmt"

	model "alphaflow/go-service/pkg/marketmodel"
)

type Result struct {
	OpenTime      int64
	CloseTime     int64
	Values        map[string]string
	NumericValues map[string]float64
	Signals       map[string]string
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
	return calculateWindow(window, options, true)
}

// CalculateWindowNumeric calculates indicators without materializing the
// legacy string value map. It is intended for in-process consumers such as
// backtests that read NumericValues directly.
func CalculateWindowNumeric(window *CalculationWindow, options Options) (Result, error) {
	return calculateWindow(window, options, false)
}

func calculateWindow(window *CalculationWindow, options Options, encodeValues bool) (Result, error) {
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

	var values map[string]string
	numericSet := NewValueSet(resultValuesCapacity)
	signals := make(map[string]string, resultSignalsCapacity)
	requiredSamples := requiredSampleCount(options)
	setValueSet(numericSet, "sample_count", float64(len(closed)), true)
	setValueSet(numericSet, "required_count", float64(requiredSamples), true)
	opens, highs, lows, closes, volumes, err := window.Series()
	if err != nil {
		quality, reason := assessDataQuality(closed, requiredSamples)
		signals["data_quality"] = quality
		if reason != "" {
			signals["data_quality_reason"] = reason
		}
		if quality == dataQualityInvalidOHLC {
			last := closed[len(closed)-1]
			encodedValues, numericValues := finalizeValueSet(numericSet, values, encodeValues)
			return Result{
				OpenTime:      last.OpenTime,
				CloseTime:     last.CloseTime,
				Values:        encodedValues,
				NumericValues: numericValues,
				Signals:       signals,
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
		encodedValues, numericValues := finalizeValueSet(numericSet, values, encodeValues)
		return Result{
			OpenTime:      last.OpenTime,
			CloseTime:     last.CloseTime,
			Values:        encodedValues,
			NumericValues: numericValues,
			Signals:       signals,
		}, nil
	}
	setValueSet(numericSet, "close", closes[len(closes)-1], true)
	basic := window.basic
	if window.aiPreview == nil {
		window.prepareAISourcePrefix()
	}
	features := newFeatureContextWithWindow(highs, lows, closes, basic, window)

	for _, period := range options.SMAPeriods {
		value, ok := basic.sma(period)
		if !ok {
			value, ok = sma(closes, period)
		}
		setValueSet(numericSet, fmt.Sprintf("sma%d", period), value, ok)
	}
	for _, period := range options.EMAPeriods {
		value, ok := basic.emaValue(period)
		if !ok {
			value, ok = ema(closes, period)
		}
		setValueSet(numericSet, fmt.Sprintf("ema%d", period), value, ok)
	}
	for _, period := range options.WMAPeriods {
		value, ok := wma(closes, period)
		setValueSet(numericSet, fmt.Sprintf("wma%d", period), value, ok)
	}
	addMovingAverageFeaturesToSet(numericSet, values, signals, closes, volumes, basic)

	rsi14Series, ok := basic.rsiSeries14()
	if !ok {
		rsi14Series, _ = rsiSeries(closes, 14)
	}
	addRSIFeaturesFromSeriesToSet(numericSet, values, signals, closes, rsi14Series)
	if series, ok := basic.macdSeries(macdConfig{fast: 12, slow: 26, signal: 9}); ok {
		addMACDSeriesFeaturesToSet(numericSet, values, signals, closes, series, "macd")
	} else if series, ok := macdSeries(closes, 12, 26, 9); ok {
		addMACDSeriesFeaturesToSet(numericSet, values, signals, closes, series, "macd")
	}
	if series, ok := basic.macdSeries(macdConfig{fast: 7, slow: 19, signal: 9}); ok {
		addMACDSeriesFeaturesToSet(numericSet, values, signals, closes, series, "macd_fast")
	} else if series, ok := macdSeries(closes, 7, 19, 9); ok {
		addMACDSeriesFeaturesToSet(numericSet, values, signals, closes, series, "macd_fast")
	}
	if current, previous, ok := basic.stcValue(); ok {
		addSTCFeaturesToSet(numericSet, values, signals, current, previous)
	} else if current, previous, ok := stcValue(closes); ok {
		addSTCFeaturesToSet(numericSet, values, signals, current, previous)
	}
	addOscillatorFeaturesWithRSIToSet(numericSet, values, signals, highs, lows, closes, rsi14Series, basic)
	if atr14Series, ok := features.atrSeries(14); ok {
		addVolatilityCoreFeaturesWithATRToSet(numericSet, values, signals, highs, lows, closes, 14, atr14Series, basic)
	} else {
		addVolatilityCoreFeaturesToSet(numericSet, values, signals, highs, lows, closes, 14)
	}
	upper, middle, lower, ok := features.bollinger(20, 2)
	if ok {
		setValueSet(numericSet, "bb_upper", upper, true)
		setValueSet(numericSet, "bb_middle", middle, true)
		setValueSet(numericSet, "bb_lower", lower, true)
	}
	volumeMA, ok := basic.volumeSMAValue(20)
	if !ok {
		volumeMA, ok = sma(volumes, 20)
	}
	setValueSet(numericSet, "volume_ma20", volumeMA, ok)
	if obvValue, ok := basic.obvValue(); ok {
		setValueSet(numericSet, "obv", obvValue, true)
	} else {
		setValueSet(numericSet, "obv", obv(closes, volumes), true)
	}
	donchianHigh, donchianLow, ok := features.donchian(20)
	if ok {
		setValueSet(numericSet, "donchian_high20", donchianHigh, true)
		setValueSet(numericSet, "donchian_low20", donchianLow, true)
	}
	if vwapValue, ok := basic.vwapValue(closes[len(closes)-1]); ok {
		setValueSet(numericSet, "vwap", vwapValue, true)
	} else {
		setValueSet(numericSet, "vwap", vwap(highs, lows, closes, volumes), true)
	}
	addDerivedToSet(numericSet, values, opens, highs, lows, closes, volumes)
	addEnhancedToSet(numericSet, values, signals, opens, highs, lows, closes, volumes, basic, features, window.aiPreview)

	encodedValues, numericResult := finalizeValueSet(numericSet, values, encodeValues)
	return Result{
		OpenTime:      last.OpenTime,
		CloseTime:     last.CloseTime,
		Values:        encodedValues,
		NumericValues: numericResult,
		Signals:       signals,
	}, nil
}
