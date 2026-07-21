package marketstructure

import (
	"math"
	"sort"

	"alphaflow/go-service/pkg/signalresearch"
)

type numericFeatureComparison struct {
	Count                  int     `json:"count"`
	Mean                   float64 `json:"mean"`
	Median                 float64 `json:"median"`
	DeltaFromConsolidation float64 `json:"delta_from_consolidation"`
	StandardizedDifference float64 `json:"standardized_difference"`
}

type signalValueComparison struct {
	Count             int     `json:"count"`
	Rate              float64 `json:"rate"`
	ConsolidationRate float64 `json:"consolidation_rate"`
	RateDelta         float64 `json:"rate_delta"`
}

type featureEvaluation struct {
	Version           string                                                 `json:"version"`
	CohortDefinitions map[string]string                                      `json:"cohort_definitions"`
	CohortSamples     map[string]int                                         `json:"cohort_samples"`
	Numeric           map[string]map[string]numericFeatureComparison         `json:"numeric"`
	Signals           map[string]map[string]map[string]signalValueComparison `json:"signals"`
}

func buildFeatureEvaluation(observations []signalresearch.MarketStructureObservation, episodes []episode) featureEvaluation {
	rows := make([]signalresearch.MarketStructureObservation, 0, len(observations)/3)
	labels := make([]string, 0, len(observations)/3)
	directions := make([]int, 0, len(observations)/3)
	for _, observation := range observations {
		if observation.Forward.HorizonBars != 80 {
			continue
		}
		label, direction := classifyEpisodeObservation(observation.Forward, augustEpisodeThresholds)
		rows = append(rows, observation)
		labels = append(labels, label)
		directions = append(directions, direction)
	}
	cohortMasks := map[string][]bool{
		"episode_start":            make([]bool, len(rows)),
		"pre_episode_60m":          make([]bool, len(rows)),
		"consolidation":            make([]bool, len(rows)),
		"rejected_direction_start": make([]bool, len(rows)),
	}
	accepted := make([]bool, len(rows))
	nearEpisode := make([]bool, len(rows))
	for _, item := range episodes {
		cohortMasks["episode_start"][item.startIndex] = true
		for index := item.startIndex; index < item.endIndex; index++ {
			accepted[index] = true
		}
		for index := max(0, item.startIndex-4); index < item.startIndex; index++ {
			cohortMasks["pre_episode_60m"][index] = true
		}
		for index := max(0, item.startIndex-4); index < min(len(rows), item.endIndex+4); index++ {
			nearEpisode[index] = true
		}
	}
	for index, label := range labels {
		if label == "low_volatility_consolidation" && !nearEpisode[index] {
			cohortMasks["consolidation"][index] = true
		}
	}
	markRejectedStarts(directions, accepted, cohortMasks["rejected_direction_start"])

	result := featureEvaluation{
		Version: "market-structure-feature-evaluation.v1",
		CohortDefinitions: map[string]string{
			"episode_start":            "first 15m observation of each accepted 80-bar episode",
			"pre_episode_60m":          "up to four observations immediately before an accepted episode",
			"consolidation":            "low-volatility consolidation outside four observations of an accepted episode",
			"rejected_direction_start": "first observation of a directional run rejected by the episode duration rule",
		},
		CohortSamples: map[string]int{}, Numeric: map[string]map[string]numericFeatureComparison{},
		Signals: map[string]map[string]map[string]signalValueComparison{},
	}
	cohortIndexes := map[string][]int{}
	for name, mask := range cohortMasks {
		for index, included := range mask {
			if included {
				cohortIndexes[name] = append(cohortIndexes[name], index)
			}
		}
		result.CohortSamples[name] = len(cohortIndexes[name])
	}
	buildNumericComparisons(rows, cohortIndexes, &result)
	buildSignalComparisons(rows, cohortIndexes, &result)
	return result
}

func markRejectedStarts(directions []int, accepted []bool, target []bool) {
	bridged := append([]int(nil), directions...)
	for index := 1; index+1 < len(bridged); index++ {
		if bridged[index] == 0 && bridged[index-1] != 0 && bridged[index-1] == bridged[index+1] {
			bridged[index] = bridged[index-1]
		}
	}
	for start := 0; start < len(bridged); {
		if bridged[start] == 0 {
			start++
			continue
		}
		end := start + 1
		for end < len(bridged) && bridged[end] == bridged[start] {
			end++
		}
		if !accepted[start] {
			target[start] = true
		}
		start = end
	}
}

func buildNumericComparisons(rows []signalresearch.MarketStructureObservation, cohorts map[string][]int, result *featureEvaluation) {
	keys := map[string]struct{}{}
	for _, row := range rows {
		for key := range row.Features.Numeric {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		baseline := numericValues(rows, cohorts["consolidation"], key)
		baselineMean, _, baselineDeviation := numericMoments(baseline)
		result.Numeric[key] = map[string]numericFeatureComparison{}
		for cohort, indexes := range cohorts {
			values := numericValues(rows, indexes, key)
			mean, median, _ := numericMoments(values)
			comparison := numericFeatureComparison{Count: len(values), Mean: mean, Median: median, DeltaFromConsolidation: mean - baselineMean}
			if baselineDeviation > 0 {
				comparison.StandardizedDifference = (mean - baselineMean) / baselineDeviation
			}
			result.Numeric[key][cohort] = comparison
		}
	}
}

func buildSignalComparisons(rows []signalresearch.MarketStructureObservation, cohorts map[string][]int, result *featureEvaluation) {
	keys := map[string]struct{}{}
	for _, row := range rows {
		for key := range row.Features.Signals {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		baseline := signalCounts(rows, cohorts["consolidation"], key)
		result.Signals[key] = map[string]map[string]signalValueComparison{}
		for cohort, indexes := range cohorts {
			counts := signalCounts(rows, indexes, key)
			result.Signals[key][cohort] = map[string]signalValueComparison{}
			values := map[string]struct{}{}
			for value := range baseline {
				values[value] = struct{}{}
			}
			for value := range counts {
				values[value] = struct{}{}
			}
			for value := range values {
				rate := ratio(counts[value], len(indexes))
				baselineRate := ratio(baseline[value], len(cohorts["consolidation"]))
				result.Signals[key][cohort][value] = signalValueComparison{
					Count: counts[value], Rate: rate, ConsolidationRate: baselineRate, RateDelta: rate - baselineRate,
				}
			}
		}
	}
}

func numericValues(rows []signalresearch.MarketStructureObservation, indexes []int, key string) []float64 {
	values := make([]float64, 0, len(indexes))
	for _, index := range indexes {
		if value, ok := rows[index].Features.Numeric[key]; ok {
			values = append(values, value)
		}
	}
	return values
}

func numericMoments(values []float64) (float64, float64, float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	mean := 0.0
	for _, value := range ordered {
		mean += value
	}
	mean /= float64(len(ordered))
	variance := 0.0
	for _, value := range ordered {
		variance += (value - mean) * (value - mean)
	}
	return mean, ordered[(len(ordered)-1)/2], math.Sqrt(variance / float64(len(ordered)))
}

func signalCounts(rows []signalresearch.MarketStructureObservation, indexes []int, key string) map[string]int {
	counts := map[string]int{}
	for _, index := range indexes {
		if value, ok := rows[index].Features.Signals[key]; ok {
			counts[value]++
		}
	}
	return counts
}

func ratio(numerator int, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
