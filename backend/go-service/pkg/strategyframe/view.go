package strategyframe

import (
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

func IndicatorView(snapshot marketmodel.IndicatorSnapshot) strategy.IndicatorView {
	return strategy.IndicatorView{
		OpenTime:  snapshot.OpenTime,
		CloseTime: snapshot.CloseTime,
		Values:    snapshot.Values,
		Signals:   snapshot.Signals,
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func PriceView(indicator strategy.IndicatorView, current marketmodel.Kline) strategy.PriceView {
	price := strategy.PriceView{
		LastPrice: indicator.Values["last_price"],
		MarkPrice: indicator.Values["mark_price"],
	}
	if price.LastPrice == "" {
		price.LastPrice = current.Close
	}
	return price
}

func WindowView(snapshot marketmodel.IndicatorWindowSnapshot) (strategy.IndicatorWindowView, error) {
	values, sampleCount, err := numericSeries(snapshot.Values)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	signals, err := signalSeries(snapshot.Signals)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	return strategy.IndicatorWindowView{
		OpenTime:    snapshot.OpenTime,
		CloseTime:   snapshot.CloseTime,
		Version:     snapshot.Version,
		SampleCount: sampleCount,
		Values:      values,
		Signals:     signals,
		UpdatedAt:   snapshot.UpdatedAt,
	}, nil
}

func WindowViewFromResult(result indicatorwindow.Result, updatedAt int64) (strategy.IndicatorWindowView, error) {
	if updatedAt == 0 {
		updatedAt = result.CloseTime
	}
	return WindowView(marketmodel.IndicatorWindowSnapshot{
		OpenTime:  result.OpenTime,
		CloseTime: result.CloseTime,
		Version:   result.Version,
		Values:    result.Values,
		Signals:   result.Signals,
		UpdatedAt: updatedAt,
	})
}

func numericSeries(fields map[string]string) (map[string]strategy.NumericSeries, int, error) {
	values := map[string]strategy.NumericSeries{}
	sampleCount := 0
	for field, value := range fields {
		if field == "sample_count" || field == "window_sample_count" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, 0, fmt.Errorf("parse %s: %w", field, err)
			}
			sampleCount = parsed
			continue
		}
		base, suffix := splitNumericSuffix(field)
		series := values[base]
		if err := applyNumericValue(&series, suffix, value); err != nil {
			return nil, 0, fmt.Errorf("parse %s: %w", field, err)
		}
		values[base] = series
	}
	return values, sampleCount, nil
}

func splitNumericSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_range_position_pct", "_win_falling_count", "_win_rising_count",
		"_win_change_pct", "_win_direction", "_win_previous", "_win_latest",
		"_win_change", "_win_slope", "_win_min", "_win_max",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix), suffix
		}
	}
	return key, "_win_latest"
}

func applyNumericValue(series *strategy.NumericSeries, suffix string, value string) error {
	if suffix == "_win_direction" {
		series.Direction = value
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	switch suffix {
	case "_win_latest":
		series.Latest = parsed
	case "_win_previous":
		series.Previous = parsed
	case "_win_change":
		series.Change = parsed
	case "_win_change_pct":
		series.ChangePct = parsed
	case "_win_slope":
		series.Slope = parsed
	case "_win_rising_count":
		series.RisingCount = int(parsed)
	case "_win_falling_count":
		series.FallingCount = int(parsed)
	case "_win_min":
		series.Minimum = parsed
	case "_win_max":
		series.Maximum = parsed
	case "_win_range_position_pct":
		series.RangePositionPct = parsed
	default:
		return fmt.Errorf("unsupported numeric suffix %q", suffix)
	}
	return nil
}

func signalSeries(fields map[string]string) (map[string]strategy.SignalSeries, error) {
	signals := map[string]strategy.SignalSeries{}
	for field, value := range fields {
		base, suffix := splitSignalSuffix(field)
		series := signals[base]
		if err := applySignalValue(&series, suffix, value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", field, err)
		}
		signals[base] = series
	}
	return signals, nil
}

func splitSignalSuffix(key string) (string, string) {
	suffixes := []string{
		"_win_last_changed_ago", "_win_stable_count", "_win_previous",
		"_win_changed", "_win_latest",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix), suffix
		}
	}
	return key, "_win_latest"
}

func applySignalValue(series *strategy.SignalSeries, suffix string, value string) error {
	switch suffix {
	case "_win_latest":
		series.Latest = value
	case "_win_previous":
		series.Previous = value
	case "_win_changed":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		series.Changed = parsed
	case "_win_stable_count":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		series.StableCount = parsed
	case "_win_last_changed_ago":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		series.LastChangedAgo = parsed
	default:
		return fmt.Errorf("unsupported signal suffix %q", suffix)
	}
	return nil
}
