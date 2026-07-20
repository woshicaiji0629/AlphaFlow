package signalresearch

import (
	"fmt"
	"sort"

	"alphaflow/go-service/pkg/strategy"
)

const MarketStructureRegimeFeatureVersion = "market-structure-regime-features.v1"

var marketStructureIntervals = [...]string{"15m", "30m"}

var marketStructureNumericFeatures = [...]string{
	"atr_pct14",
	"adx14",
	"di_plus14",
	"di_minus14",
	"ema25_slope5_pct",
	"ema_spread_pct",
	"bb_width_pct",
	"bb_width_delta",
	"squeeze_momentum",
	"squeeze_momentum_delta",
	"volume_ratio5",
	"volume_ratio20",
	"price_ema25_distance_pct",
}

var marketStructureSignalFeatures = [...]string{
	"ema_alignment",
	"supertrend_direction",
	"market_structure",
	"trend_window_bias",
	"trend_window_continuation",
	"trend_window_reversal_risk",
	"ma_window_bias",
	"ma_window_tangled",
	"structure_window_bias",
	"structure_window_event",
	"smc_window_bias",
	"smc_window_event",
	"smc_window_bos_recent",
	"smc_window_choch_recent",
	"smc_window_liquidity_sweep",
	"volatility_window_state",
	"bb_width_state",
	"squeeze_state",
}

type MarketStructureFeatureSnapshot struct {
	Version        string             `json:"version"`
	AsOfMS         int64              `json:"as_of_ms"`
	Numeric        map[string]float64 `json:"numeric"`
	Signals        map[string]string  `json:"signals"`
	Missing        []string           `json:"missing,omitempty"`
	AvailableCount int                `json:"available_count"`
	ExpectedCount  int                `json:"expected_count"`
}

type MarketStructureObservation struct {
	Features MarketStructureFeatureSnapshot `json:"features"`
	Forward  ForwardMetrics                 `json:"forward"`
}

// ExtractMarketStructureFeatures captures only point-in-time 15m/30m inputs.
// Result-side forward labels and aggregate capability scores are deliberately
// excluded from this protocol.
func ExtractMarketStructureFeatures(snapshot strategy.Snapshot) (MarketStructureFeatureSnapshot, error) {
	if snapshot.AsOf <= 0 {
		return MarketStructureFeatureSnapshot{}, fmt.Errorf("market structure snapshot as_of is required")
	}
	result := MarketStructureFeatureSnapshot{
		Version: MarketStructureRegimeFeatureVersion, AsOfMS: snapshot.AsOf,
		Numeric: map[string]float64{}, Signals: map[string]string{},
		ExpectedCount: len(marketStructureIntervals) * (len(marketStructureNumericFeatures) + len(marketStructureSignalFeatures)),
	}
	for _, interval := range marketStructureIntervals {
		timeframe, ok := snapshot.Timeframes[interval]
		if !ok || timeframe.Indicator.CloseTime <= 0 || timeframe.Indicator.CloseTime > snapshot.AsOf || timeframe.Window.CloseTime > snapshot.AsOf {
			for _, name := range marketStructureNumericFeatures {
				result.Missing = append(result.Missing, interval+"."+name)
			}
			for _, name := range marketStructureSignalFeatures {
				result.Missing = append(result.Missing, interval+"."+name)
			}
			continue
		}
		for _, name := range marketStructureNumericFeatures {
			key := interval + "." + name
			if value, ok := timeframeNumeric(timeframe, name); ok {
				result.Numeric[key] = value
				result.AvailableCount++
			} else {
				result.Missing = append(result.Missing, key)
			}
		}
		for _, name := range marketStructureSignalFeatures {
			key := interval + "." + name
			if value, ok := timeframeSignalValue(timeframe, name); ok {
				result.Signals[key] = value
				result.AvailableCount++
			} else {
				result.Missing = append(result.Missing, key)
			}
		}
	}
	sort.Strings(result.Missing)
	return result, nil
}

func timeframeNumeric(timeframe strategy.TimeframeSnapshot, name string) (float64, bool) {
	if series, ok := timeframe.Window.Numeric(name); ok {
		return series.Latest, true
	}
	return timeframe.Indicator.Float(name)
}

func timeframeSignalValue(timeframe strategy.TimeframeSnapshot, name string) (string, bool) {
	if series, ok := timeframe.Window.Signal(name); ok && series.Latest != "" {
		return series.Latest, true
	}
	value := timeframe.Indicator.Signals[name]
	return value, value != ""
}
