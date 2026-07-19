package signalresearch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

const ForwardDistributionVersion = "forward-market-distribution.v1"

var distributionPercentiles = [...]float64{0.10, 0.20, 0.25, 0.40, 0.50, 0.60, 0.75, 0.80, 0.90}

type ForwardDistributionReport struct {
	Version          string                `json:"version"`
	LabelVersion     string                `json:"label_version"`
	SampleStartMS    int64                 `json:"sample_start_ms"`
	SampleEndMS      int64                 `json:"sample_end_ms"`
	CandidateSamples int                   `json:"candidate_samples"`
	PercentileMethod string                `json:"percentile_method"`
	Horizons         []HorizonDistribution `json:"horizons"`
}

type HorizonDistribution struct {
	HorizonBars    int                                 `json:"horizon_bars"`
	ValidSamples   int                                 `json:"valid_samples"`
	InvalidSamples int                                 `json:"invalid_samples"`
	MonthlySamples map[string]int                      `json:"monthly_samples"`
	Metrics        map[string]MetricSummary            `json:"metrics"`
	MonthlyMetrics map[string]map[string]MetricSummary `json:"monthly_metrics"`
	Rates          map[string]RateSummary              `json:"rates"`
	MonthlyRates   map[string]map[string]RateSummary   `json:"monthly_rates"`
}

type MetricSummary struct {
	Count       int                `json:"count"`
	Min         float64            `json:"min"`
	Max         float64            `json:"max"`
	Mean        float64            `json:"mean"`
	Median      float64            `json:"median"`
	Percentiles map[string]float64 `json:"percentiles"`
}

type RateSummary struct {
	Count     int     `json:"count"`
	TrueCount int     `json:"true_count"`
	Rate      float64 `json:"rate"`
}

// BuildForwardDistribution summarizes every closed bar whose open time is in
// [sampleStartMS, sampleEndMS). Future bars must be supplied after the sample
// range so the final candidate can complete each requested horizon.
func BuildForwardDistribution(klines []marketmodel.Kline, sampleStartMS int64, sampleEndMS int64, intervalMillis int64, horizons []int) (ForwardDistributionReport, error) {
	if sampleEndMS <= sampleStartMS {
		return ForwardDistributionReport{}, fmt.Errorf("sample end must be after sample start")
	}
	if intervalMillis <= 0 {
		return ForwardDistributionReport{}, fmt.Errorf("interval milliseconds must be positive")
	}
	horizons = normalizedHorizons(horizons)
	if len(horizons) == 0 {
		return ForwardDistributionReport{}, fmt.Errorf("at least one positive horizon is required")
	}

	report := ForwardDistributionReport{
		Version: ForwardDistributionVersion, LabelVersion: ForwardLabelVersion,
		SampleStartMS: sampleStartMS, SampleEndMS: sampleEndMS,
		PercentileMethod: "nearest_rank", Horizons: make([]HorizonDistribution, len(horizons)),
	}
	collectors := make([]map[string][]float64, len(horizons))
	monthlyCollectors := make([]map[string]map[string][]float64, len(horizons))
	rateCollectors := make([]map[string]*RateSummary, len(horizons))
	monthlyRateCollectors := make([]map[string]map[string]*RateSummary, len(horizons))
	for index, horizon := range horizons {
		report.Horizons[index] = HorizonDistribution{
			HorizonBars: horizon, MonthlySamples: map[string]int{}, Metrics: map[string]MetricSummary{},
			MonthlyMetrics: map[string]map[string]MetricSummary{}, Rates: map[string]RateSummary{},
			MonthlyRates: map[string]map[string]RateSummary{},
		}
		collectors[index] = newMetricCollector()
		monthlyCollectors[index] = map[string]map[string][]float64{}
		rateCollectors[index] = newRateCollector()
		monthlyRateCollectors[index] = map[string]map[string]*RateSummary{}
	}

	for index, kline := range klines {
		if kline.OpenTime < sampleStartMS || kline.OpenTime >= sampleEndMS {
			continue
		}
		report.CandidateSamples++
		entryPrice, err := strconv.ParseFloat(kline.Close, 64)
		if err != nil || entryPrice <= 0 || !kline.IsClosed {
			for horizonIndex := range report.Horizons {
				report.Horizons[horizonIndex].InvalidSamples++
			}
			continue
		}
		month := time.UnixMilli(kline.OpenTime).UTC().Format("2006-01")
		for horizonIndex, horizon := range horizons {
			if !hasContinuousFuture(klines, index, horizon, intervalMillis) {
				report.Horizons[horizonIndex].InvalidSamples++
				continue
			}
			metrics, err := CalculateForwardMetrics(entryPrice, klines[index+1:], horizon)
			if err != nil {
				report.Horizons[horizonIndex].InvalidSamples++
				continue
			}
			report.Horizons[horizonIndex].ValidSamples++
			report.Horizons[horizonIndex].MonthlySamples[month]++
			appendMetrics(collectors[horizonIndex], metrics)
			appendRates(rateCollectors[horizonIndex], metrics)
			if _, ok := monthlyCollectors[horizonIndex][month]; !ok {
				monthlyCollectors[horizonIndex][month] = newMetricCollector()
				monthlyRateCollectors[horizonIndex][month] = newRateCollector()
			}
			appendMetrics(monthlyCollectors[horizonIndex][month], metrics)
			appendRates(monthlyRateCollectors[horizonIndex][month], metrics)
		}
	}

	for index := range report.Horizons {
		for name, values := range collectors[index] {
			if len(values) == 0 {
				continue
			}
			report.Horizons[index].Metrics[name] = summarizeMetric(values)
		}
		for month, monthCollectors := range monthlyCollectors[index] {
			monthMetrics := make(map[string]MetricSummary, len(monthCollectors))
			for name, values := range monthCollectors {
				if len(values) > 0 {
					monthMetrics[name] = summarizeMetric(values)
				}
			}
			report.Horizons[index].MonthlyMetrics[month] = monthMetrics
			report.Horizons[index].MonthlyRates[month] = summarizeRates(monthlyRateCollectors[index][month])
		}
		report.Horizons[index].Rates = summarizeRates(rateCollectors[index])
	}
	return report, nil
}

func hasContinuousFuture(klines []marketmodel.Kline, originIndex int, horizon int, intervalMillis int64) bool {
	if originIndex+horizon >= len(klines) {
		return false
	}
	originOpenTime := klines[originIndex].OpenTime
	for offset := 1; offset <= horizon; offset++ {
		if klines[originIndex+offset].OpenTime != originOpenTime+int64(offset)*intervalMillis {
			return false
		}
	}
	return true
}

func normalizedHorizons(values []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func newMetricCollector() map[string][]float64 {
	return map[string][]float64{
		"direction_return_bps": {}, "midpoint_return_bps": {}, "late_return_bps": {},
		"absolute_direction_return_bps": {}, "absolute_midpoint_return_bps": {}, "absolute_late_return_bps": {},
		"max_upside_bps": {}, "max_downside_bps": {}, "path_efficiency": {},
		"total_excursion_bps": {}, "absolute_directional_advantage": {},
		"realized_volatility_bps": {}, "max_close_drawdown_bps": {}, "max_close_recovery_bps": {},
		"dominant_excursion_bps": {}, "dominant_giveback_bps": {}, "dominant_giveback_ratio": {},
		"dominant_excursion_position": {}, "directional_advantage": {}, "dominant_retention": {},
		"phase_expansion": {},
	}
}

func appendMetrics(values map[string][]float64, metrics ForwardMetrics) {
	values["direction_return_bps"] = append(values["direction_return_bps"], metrics.DirectionReturnBps)
	values["midpoint_return_bps"] = append(values["midpoint_return_bps"], metrics.MidpointReturnBps)
	values["late_return_bps"] = append(values["late_return_bps"], metrics.LateReturnBps)
	values["absolute_direction_return_bps"] = append(values["absolute_direction_return_bps"], math.Abs(metrics.DirectionReturnBps))
	values["absolute_midpoint_return_bps"] = append(values["absolute_midpoint_return_bps"], math.Abs(metrics.MidpointReturnBps))
	values["absolute_late_return_bps"] = append(values["absolute_late_return_bps"], math.Abs(metrics.LateReturnBps))
	values["max_upside_bps"] = append(values["max_upside_bps"], metrics.MaxUpsideBps)
	values["max_downside_bps"] = append(values["max_downside_bps"], metrics.MaxDownsideBps)
	values["total_excursion_bps"] = append(values["total_excursion_bps"], metrics.MaxUpsideBps+metrics.MaxDownsideBps)
	values["absolute_directional_advantage"] = append(values["absolute_directional_advantage"], math.Abs(metrics.DirectionalAdvantage))
	values["path_efficiency"] = append(values["path_efficiency"], metrics.PathEfficiency)
	values["realized_volatility_bps"] = append(values["realized_volatility_bps"], metrics.RealizedVolatilityBps)
	values["max_close_drawdown_bps"] = append(values["max_close_drawdown_bps"], metrics.MaxCloseDrawdownBps)
	values["max_close_recovery_bps"] = append(values["max_close_recovery_bps"], metrics.MaxCloseRecoveryBps)
	values["dominant_excursion_bps"] = append(values["dominant_excursion_bps"], metrics.DominantExcursionBps)
	values["dominant_giveback_bps"] = append(values["dominant_giveback_bps"], metrics.DominantGivebackBps)
	values["dominant_giveback_ratio"] = append(values["dominant_giveback_ratio"], metrics.DominantGivebackRatio)
	values["dominant_excursion_position"] = append(values["dominant_excursion_position"], metrics.DominantExcursionPosition)
	values["directional_advantage"] = append(values["directional_advantage"], metrics.DirectionalAdvantage)
	values["dominant_retention"] = append(values["dominant_retention"], metrics.DominantRetention)
	values["phase_expansion"] = append(values["phase_expansion"], metrics.PhaseExpansion)
}

func newRateCollector() map[string]*RateSummary {
	return map[string]*RateSummary{"midpoint_late_same_direction": {}}
}

func appendRates(values map[string]*RateSummary, metrics ForwardMetrics) {
	summary := values["midpoint_late_same_direction"]
	summary.Count++
	if metrics.MidpointReturnBps != 0 && metrics.LateReturnBps != 0 && math.Signbit(metrics.MidpointReturnBps) == math.Signbit(metrics.LateReturnBps) {
		summary.TrueCount++
	}
}

func summarizeRates(values map[string]*RateSummary) map[string]RateSummary {
	result := make(map[string]RateSummary, len(values))
	for name, item := range values {
		summary := *item
		if summary.Count > 0 {
			summary.Rate = float64(summary.TrueCount) / float64(summary.Count)
		}
		result[name] = summary
	}
	return result
}

func summarizeMetric(values []float64) MetricSummary {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	summary := MetricSummary{
		Count: len(sorted), Min: sorted[0], Max: sorted[len(sorted)-1],
		Percentiles: make(map[string]float64, len(distributionPercentiles)),
	}
	for _, value := range sorted {
		summary.Mean += value
	}
	summary.Mean /= float64(len(sorted))
	summary.Median = nearestRank(sorted, 0.50)
	for _, percentile := range distributionPercentiles {
		name := fmt.Sprintf("p%02d", int(math.Round(percentile*100)))
		summary.Percentiles[name] = nearestRank(sorted, percentile)
	}
	return summary
}

func nearestRank(sorted []float64, percentile float64) float64 {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	index = max(0, min(index, len(sorted)-1))
	return sorted[index]
}
