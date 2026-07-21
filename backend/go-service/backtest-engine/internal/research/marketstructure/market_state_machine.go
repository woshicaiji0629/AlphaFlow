package marketstructure

import (
	"time"

	"alphaflow/go-service/pkg/signalresearch"
)

type stateMachineThresholds struct {
	CompressionWidthBPS  float64 `json:"compression_width_p40_bps"`
	CompressionExpansion float64 `json:"compression_expansion_p50"`
	BreakoutVolumeRatio  float64 `json:"breakout_volume_ratio_p50"`
	ConfirmRangePosition float64 `json:"confirm_range_position_p60"`
	FailureReturnBPS     float64 `json:"failure_direction_return_p40_bps"`
	TrendPathEfficiency  float64 `json:"trend_path_efficiency_p50"`
	ExhaustionExpansion  float64 `json:"exhaustion_expansion_p75"`
}

type stateMachineTrade struct {
	Direction           string  `json:"direction"`
	EntryMS             int64   `json:"entry_ms"`
	ExitMS              int64   `json:"exit_ms"`
	ExitReason          string  `json:"exit_reason"`
	HoldingObservations int     `json:"holding_observations"`
	ReturnBPS           float64 `json:"return_bps"`
	Forward40MFEBPS     float64 `json:"forward_40_mfe_bps"`
	Forward40MAEBPS     float64 `json:"forward_40_mae_bps"`
}

type stateMachineSplit struct {
	Observations        int                 `json:"observations"`
	CompressionEntries  int                 `json:"compression_entries"`
	BreakoutCandidates  int                 `json:"breakout_candidates"`
	FailedBreakouts     int                 `json:"failed_breakouts"`
	ConfirmedTrends     int                 `json:"confirmed_trends"`
	Trades              int                 `json:"trades"`
	LongTrades          int                 `json:"long_trades"`
	ShortTrades         int                 `json:"short_trades"`
	WinningTrades       int                 `json:"winning_trades"`
	WinRate             float64             `json:"win_rate"`
	AverageReturnBPS    float64             `json:"average_return_bps"`
	AverageMFEBPS       float64             `json:"average_forward_40_mfe_bps"`
	AverageMAEBPS       float64             `json:"average_forward_40_mae_bps"`
	ProfitFactor        float64             `json:"profit_factor"`
	TradesPerDay        float64             `json:"trades_per_day"`
	Episodes            int                 `json:"episodes"`
	CoveredEpisodes     int                 `json:"covered_episodes"`
	EpisodeCoverageRate float64             `json:"episode_coverage_rate"`
	ExitReasons         map[string]int      `json:"exit_reasons"`
	TradesDetail        []stateMachineTrade `json:"trades_detail"`
}

type marketStateMachineEvaluation struct {
	Version           string                   `json:"version"`
	Method            string                   `json:"method"`
	TrainStartMS      int64                    `json:"train_start_ms"`
	TrainEndMS        int64                    `json:"train_end_ms"`
	ValidationStartMS int64                    `json:"validation_start_ms"`
	ValidationEndMS   int64                    `json:"validation_end_ms"`
	Thresholds        stateMachineThresholds   `json:"thresholds"`
	Train             stateMachineSplit        `json:"train"`
	Validation        stateMachineSplit        `json:"validation"`
	TrainFunnel       stateMachineFunnel       `json:"train_episode_funnel"`
	ValidationFunnel  stateMachineFunnel       `json:"validation_episode_funnel"`
	ShapePrototypes   shapePrototypeEvaluation `json:"shape_prototypes"`
}

type stateMachineFunnelStages struct {
	Episodes                  int `json:"episodes"`
	CompressionSeen           int `json:"compression_seen"`
	DirectionalBreakoutSeen   int `json:"directional_breakout_seen"`
	VolumeConfirmed           int `json:"volume_confirmed"`
	BoundaryHeld              int `json:"boundary_held"`
	PositiveDirectionReturn   int `json:"positive_direction_return"`
	RangePositionConfirmed    int `json:"range_position_confirmed"`
	DirectionBalanceConfirmed int `json:"direction_balance_confirmed"`
	StateMachineEntry         int `json:"state_machine_entry"`
}

type stateMachineRejectionSummary struct {
	Count                       int     `json:"count"`
	AverageDominantExcursionBPS float64 `json:"average_dominant_excursion_bps"`
	AverageDurationMinutes      float64 `json:"average_duration_minutes"`
}

type stateMachineFunnel struct {
	All                   stateMachineFunnelStages                `json:"all"`
	Up                    stateMachineFunnelStages                `json:"up"`
	Down                  stateMachineFunnelStages                `json:"down"`
	FirstRejectionReasons map[string]stateMachineRejectionSummary `json:"first_rejection_reasons"`
}

func buildMarketStateMachineEvaluation(observations []signalresearch.MarketStructureObservation, episodes []episode, pathFeatures map[int64]map[string]float64) marketStateMachineEvaluation {
	rows, forwards := marketRowsAndForwards(observations)
	result := marketStateMachineEvaluation{Version: "market-structure-state-machine.v1", Method: "compression -> breakout candidate -> confirmed trend -> exhaustion/trend loss/4h timeout; train-only percentile thresholds"}
	if len(rows) == 0 {
		return result
	}
	first := time.UnixMilli(rows[0].Features.AsOfMS).UTC()
	start := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := start.AddDate(0, 0, 20)
	end := time.UnixMilli(rows[len(rows)-1].Features.AsOfMS).UTC().Add(15 * time.Minute)
	result.TrainStartMS, result.TrainEndMS = start.UnixMilli(), cutoff.UnixMilli()
	result.ValidationStartMS, result.ValidationEndMS = cutoff.UnixMilli(), end.UnixMilli()
	cutoffIndex := 0
	for cutoffIndex < len(rows) && rows[cutoffIndex].Features.AsOfMS < cutoff.UnixMilli() {
		cutoffIndex++
	}
	result.Thresholds = fitStateMachineThresholds(rows[:cutoffIndex], pathFeatures)
	result.Train = replayStateMachine(rows, forwards, episodes, pathFeatures, 0, cutoffIndex, result.Thresholds, 20)
	result.Validation = replayStateMachine(rows, forwards, episodes, pathFeatures, cutoffIndex, len(rows), result.Thresholds, end.Sub(cutoff).Hours()/24)
	result.TrainFunnel = buildStateMachineFunnel(rows, episodes, pathFeatures, 0, cutoffIndex, result.Thresholds)
	result.ValidationFunnel = buildStateMachineFunnel(rows, episodes, pathFeatures, cutoffIndex, len(rows), result.Thresholds)
	result.ShapePrototypes = evaluateShapePrototypes(rows, forwards, episodes, pathFeatures, cutoffIndex)
	return result
}

type shapeThresholds struct {
	NarrowWidthBPS       float64 `json:"narrow_width_p40_bps"`
	ExpansionP60         float64 `json:"expansion_p60"`
	ExpansionP75         float64 `json:"expansion_p75"`
	DirectionalReturnP60 float64 `json:"absolute_return_20_p60_bps"`
	VolumeRatioP60       float64 `json:"volume_ratio_p60"`
	PathEfficiencyP60    float64 `json:"path_efficiency_p60"`
}

type shapeOutcome struct {
	Samples                  int     `json:"samples"`
	AverageMFEBPS            float64 `json:"average_mfe_bps"`
	AverageMAEBPS            float64 `json:"average_mae_bps"`
	AverageReturnBPS         float64 `json:"average_return_bps"`
	PositiveReturnRate       float64 `json:"positive_return_rate"`
	AverageNetOpportunityBPS float64 `json:"average_mfe_minus_16bps_cost"`
}

type shapePrototypeSplit struct {
	Episodes            int                  `json:"episodes"`
	Alerts              int                  `json:"alerts"`
	TrueAlerts          int                  `json:"true_alerts"`
	FalseAlerts         int                  `json:"false_alerts"`
	CoveredEpisodes     int                  `json:"covered_episodes"`
	EpisodeCoverageRate float64              `json:"episode_coverage_rate"`
	DirectionEvaluated  int                  `json:"direction_evaluated"`
	DirectionCorrect    int                  `json:"direction_correct"`
	DirectionAccuracy   float64              `json:"direction_accuracy"`
	AverageLeadMinutes  float64              `json:"average_lead_minutes"`
	Outcomes            map[int]shapeOutcome `json:"outcomes"`
}

type shapePrototypeResult struct {
	Name       string              `json:"name"`
	Definition string              `json:"definition"`
	Train      shapePrototypeSplit `json:"train"`
	Validation shapePrototypeSplit `json:"validation"`
}

type shapePrototypeEvaluation struct {
	Version                   string                 `json:"version"`
	Method                    string                 `json:"method"`
	LookbackObservations      int                    `json:"lookback_observations"`
	RoundTripCostBPS          float64                `json:"round_trip_cost_bps"`
	Thresholds                shapeThresholds        `json:"thresholds"`
	TrainControlBaseline      shapeControlBaseline   `json:"train_control_baseline"`
	ValidationControlBaseline shapeControlBaseline   `json:"validation_control_baseline"`
	Prototypes                []shapePrototypeResult `json:"prototypes"`
}

type shapeControlBaseline struct {
	Samples           int                  `json:"samples"`
	DirectionCorrect  int                  `json:"direction_correct"`
	DirectionAccuracy float64              `json:"direction_accuracy"`
	Outcomes          map[int]shapeOutcome `json:"outcomes"`
}

type shapeAlert struct{ index, direction int }

func evaluateShapePrototypes(rows []signalresearch.MarketStructureObservation, forwards map[int64]map[int]signalresearch.ForwardMetrics, episodes []episode, features map[int64]map[string]float64, cutoff int) shapePrototypeEvaluation {
	result := shapePrototypeEvaluation{Version: "market-shape-prototypes.v1", Method: "train-only thresholds; causal 8x15m lookback; episode labels used only for evaluation", LookbackObservations: 8, RoundTripCostBPS: 16}
	widths, expansions, returns, volumes, efficiencies := []float64{}, []float64{}, []float64{}, []float64{}, []float64{}
	for index := 0; index < cutoff; index++ {
		f := features[rows[index].Features.AsOfMS]
		widths = append(widths, f["15m.range_width_20_bps"])
		expansions = append(expansions, f["15m.range_expansion_5_20"])
		returns = append(returns, abs(f["15m.return_20_bps"]))
		volumes = append(volumes, f["15m.volume_ratio_5_20"])
		efficiencies = append(efficiencies, f["15m.path_efficiency_20"])
	}
	result.Thresholds = shapeThresholds{
		NarrowWidthBPS: percentile(widths, .40), ExpansionP60: percentile(expansions, .60), ExpansionP75: percentile(expansions, .75),
		DirectionalReturnP60: percentile(returns, .60), VolumeRatioP60: percentile(volumes, .60), PathEfficiencyP60: percentile(efficiencies, .60),
	}
	result.TrainControlBaseline = buildShapeControlBaseline(rows, forwards, episodes, features, 0, cutoff)
	result.ValidationControlBaseline = buildShapeControlBaseline(rows, forwards, episodes, features, cutoff, len(rows))
	definitions := []string{
		"compression_breakout: prior narrow range, current directional boundary break and expansion",
		"pullback_restart: established directional move, shallow counter move, then directional recovery",
		"edge_rejection: rejection from opposite range edge with direction and volume balance recovery",
		"direct_expansion: non-compressed directional volatility expansion with persistent path and volume",
	}
	for kind, definition := range definitions {
		trainAlerts := collectShapeAlerts(rows, features, 0, cutoff, kind, result.Thresholds)
		validationAlerts := collectShapeAlerts(rows, features, cutoff, len(rows), kind, result.Thresholds)
		result.Prototypes = append(result.Prototypes, shapePrototypeResult{
			Name: []string{"compression_breakout", "pullback_restart", "edge_rejection", "direct_expansion"}[kind], Definition: definition,
			Train:      evaluateShapeSplit(rows, forwards, episodes, trainAlerts, 0, cutoff),
			Validation: evaluateShapeSplit(rows, forwards, episodes, validationAlerts, cutoff, len(rows)),
		})
	}
	return result
}

func buildShapeControlBaseline(rows []signalresearch.MarketStructureObservation, forwards map[int64]map[int]signalresearch.ForwardMetrics, episodes []episode, features map[int64]map[string]float64, start, end int) shapeControlBaseline {
	result := shapeControlBaseline{Outcomes: map[int]shapeOutcome{}}
	positive := make([]bool, len(rows))
	for _, item := range episodes {
		if item.startIndex < start || item.startIndex >= end {
			continue
		}
		for index := max(start, item.startIndex-8); index <= item.startIndex; index++ {
			positive[index] = true
		}
	}
	for index := max(start, 8); index < end; index += 4 {
		if positive[index] {
			continue
		}
		metrics := forwards[rows[index].Features.AsOfMS]
		forward20, ok := metrics[20]
		if !ok {
			continue
		}
		direction := 1
		if features[rows[index].Features.AsOfMS]["15m.return_5_bps"] < 0 {
			direction = -1
		}
		result.Samples++
		if float64(direction)*forward20.DirectionReturnBps > 0 {
			result.DirectionCorrect++
		}
		for _, horizon := range []int{20, 40, 80} {
			forward, ok := metrics[horizon]
			if !ok {
				continue
			}
			mfe, mae, ret := directionalPath(forward, direction)
			outcome := result.Outcomes[horizon]
			outcome.Samples++
			count := float64(outcome.Samples)
			outcome.AverageMFEBPS += (mfe - outcome.AverageMFEBPS) / count
			outcome.AverageMAEBPS += (mae - outcome.AverageMAEBPS) / count
			outcome.AverageReturnBPS += (ret - outcome.AverageReturnBPS) / count
			if ret > 0 {
				outcome.PositiveReturnRate += (1 - outcome.PositiveReturnRate) / count
			} else {
				outcome.PositiveReturnRate += (0 - outcome.PositiveReturnRate) / count
			}
			outcome.AverageNetOpportunityBPS = outcome.AverageMFEBPS - 16
			result.Outcomes[horizon] = outcome
		}
	}
	result.DirectionAccuracy = ratio(result.DirectionCorrect, result.Samples)
	return result
}

func collectShapeAlerts(rows []signalresearch.MarketStructureObservation, features map[int64]map[string]float64, start, end, kind int, thresholds shapeThresholds) []shapeAlert {
	alerts, previous := []shapeAlert{}, -100
	for index := max(start, 8); index < end; index++ {
		current := features[rows[index].Features.AsOfMS]
		direction := 1
		if current["15m.return_5_bps"] < 0 {
			direction = -1
		}
		if index-previous < 4 || !matchesShape(features, rows, index, direction, kind, thresholds) {
			continue
		}
		alerts, previous = append(alerts, shapeAlert{index: index, direction: direction}), index
	}
	return alerts
}

func matchesShape(features map[int64]map[string]float64, rows []signalresearch.MarketStructureObservation, index, direction, kind int, thresholds shapeThresholds) bool {
	f := func(offset int) map[string]float64 { return features[rows[index+offset].Features.AsOfMS] }
	directionalPosition := func(row map[string]float64) float64 {
		if direction > 0 {
			return row["15m.range_position_20"]
		}
		return 1 - row["15m.range_position_20"]
	}
	current := f(0)
	switch kind {
	case 0:
		narrow := false
		for offset := -8; offset < 0; offset++ {
			narrow = narrow || f(offset)["15m.range_width_20_bps"] <= thresholds.NarrowWidthBPS
		}
		return narrow && float64(direction)*current["15m.breakout_distance_20_bps"] > 0 && current["15m.range_expansion_5_20"] >= thresholds.ExpansionP60 && directionalPosition(current) >= .6
	case 1:
		prior, pullback := f(-3), f(-1)
		return float64(direction)*prior["15m.return_20_bps"] >= thresholds.DirectionalReturnP60 && float64(direction)*pullback["15m.return_5_bps"] < 0 && abs(pullback["15m.return_5_bps"]) < thresholds.DirectionalReturnP60 && float64(direction)*current["15m.return_5_bps"] > 0 && directionalPosition(current) >= .55
	case 2:
		edge := directionalPosition(f(-2)) <= .25
		return edge && float64(direction)*current["15m.return_5_bps"] > 0 && float64(direction)*current["15m.direction_balance_20"] > 0 && float64(direction)*current["15m.volume_direction_balance_20"] > 0
	default:
		continuousNarrow := true
		for offset := -2; offset <= 0; offset++ {
			continuousNarrow = continuousNarrow && f(offset)["15m.range_width_20_bps"] <= thresholds.NarrowWidthBPS
		}
		return !continuousNarrow && abs(current["15m.return_20_bps"]) >= thresholds.DirectionalReturnP60 && current["15m.range_expansion_5_20"] >= thresholds.ExpansionP75 && current["15m.volume_ratio_5_20"] >= thresholds.VolumeRatioP60 && current["15m.path_efficiency_20"] >= thresholds.PathEfficiencyP60 && float64(direction)*current["15m.return_20_bps"] > 0
	}
}

func evaluateShapeSplit(rows []signalresearch.MarketStructureObservation, forwards map[int64]map[int]signalresearch.ForwardMetrics, episodes []episode, alerts []shapeAlert, start, end int) shapePrototypeSplit {
	result := shapePrototypeSplit{Alerts: len(alerts), Outcomes: map[int]shapeOutcome{}}
	splitEpisodes := []episode{}
	for _, item := range episodes {
		if item.startIndex >= start && item.startIndex < end {
			splitEpisodes = append(splitEpisodes, item)
		}
	}
	result.Episodes = len(splitEpisodes)
	covered := make([]bool, len(splitEpisodes))
	leadSum := 0.0
	for _, alert := range alerts {
		matched := false
		for episodeIndex, item := range splitEpisodes {
			direction := map[string]int{"up": 1, "down": -1}[item.Direction]
			if alert.direction == direction && alert.index >= max(start, item.startIndex-8) && alert.index <= item.startIndex {
				matched = true
				if !covered[episodeIndex] {
					covered[episodeIndex], leadSum = true, leadSum+float64(item.startIndex-alert.index)*15
				}
			}
		}
		if matched {
			result.TrueAlerts++
		} else {
			result.FalseAlerts++
		}
		metrics := forwards[rows[alert.index].Features.AsOfMS]
		if forward, ok := metrics[20]; ok {
			result.DirectionEvaluated++
			if float64(alert.direction)*forward.DirectionReturnBps > 0 {
				result.DirectionCorrect++
			}
		}
		for _, horizon := range []int{20, 40, 80} {
			forward, ok := metrics[horizon]
			if !ok {
				continue
			}
			mfe, mae, ret := directionalPath(forward, alert.direction)
			outcome := result.Outcomes[horizon]
			outcome.Samples++
			count := float64(outcome.Samples)
			outcome.AverageMFEBPS += (mfe - outcome.AverageMFEBPS) / count
			outcome.AverageMAEBPS += (mae - outcome.AverageMAEBPS) / count
			outcome.AverageReturnBPS += (ret - outcome.AverageReturnBPS) / count
			if ret > 0 {
				outcome.PositiveReturnRate += (1 - outcome.PositiveReturnRate) / count
			} else {
				outcome.PositiveReturnRate += (0 - outcome.PositiveReturnRate) / count
			}
			outcome.AverageNetOpportunityBPS = outcome.AverageMFEBPS - 16
			result.Outcomes[horizon] = outcome
		}
	}
	for _, value := range covered {
		if value {
			result.CoveredEpisodes++
		}
	}
	result.EpisodeCoverageRate = ratio(result.CoveredEpisodes, result.Episodes)
	result.DirectionAccuracy = ratio(result.DirectionCorrect, result.DirectionEvaluated)
	if result.CoveredEpisodes > 0 {
		result.AverageLeadMinutes = leadSum / float64(result.CoveredEpisodes)
	}
	return result
}

func fitStateMachineThresholds(rows []signalresearch.MarketStructureObservation, features map[int64]map[string]float64) stateMachineThresholds {
	widths, expansions, volumes, positions, returns, efficiencies := []float64{}, []float64{}, []float64{}, []float64{}, []float64{}, []float64{}
	for _, row := range rows {
		f := features[row.Features.AsOfMS]
		widths = append(widths, f["15m.range_width_20_bps"])
		expansions = append(expansions, f["15m.range_expansion_5_20"])
		volumes = append(volumes, f["15m.volume_ratio_5_20"])
		positions = append(positions, f["15m.range_position_20"], 1-f["15m.range_position_20"])
		returns = append(returns, f["15m.return_5_bps"], -f["15m.return_5_bps"])
		efficiencies = append(efficiencies, f["15m.path_efficiency_20"])
	}
	return stateMachineThresholds{
		CompressionWidthBPS: percentile(widths, .40), CompressionExpansion: percentile(expansions, .50),
		BreakoutVolumeRatio: percentile(volumes, .50), ConfirmRangePosition: percentile(positions, .60),
		FailureReturnBPS: percentile(returns, .40), TrendPathEfficiency: percentile(efficiencies, .50),
		ExhaustionExpansion: percentile(expansions, .75),
	}
}

type episodeFunnelResult struct {
	compression, breakout, volume, hold, positiveReturn, position, balance, entry bool
	reason                                                                        string
}

func buildStateMachineFunnel(rows []signalresearch.MarketStructureObservation, episodes []episode, features map[int64]map[string]float64, splitStart int, splitEnd int, thresholds stateMachineThresholds) stateMachineFunnel {
	result := stateMachineFunnel{FirstRejectionReasons: map[string]stateMachineRejectionSummary{}}
	for _, item := range episodes {
		if item.startIndex < splitStart || item.startIndex >= splitEnd {
			continue
		}
		direction := map[string]int{"up": 1, "down": -1}[item.Direction]
		diagnostic := diagnoseEpisodeFunnel(rows, features, max(splitStart, item.startIndex-8), min(splitEnd, item.endIndex), direction, thresholds)
		appendFunnelStages(&result.All, diagnostic)
		if direction > 0 {
			appendFunnelStages(&result.Up, diagnostic)
		} else {
			appendFunnelStages(&result.Down, diagnostic)
		}
		summary := result.FirstRejectionReasons[diagnostic.reason]
		summary.Count++
		summary.AverageDominantExcursionBPS += (rows[item.startIndex].Forward.DominantExcursionBps - summary.AverageDominantExcursionBPS) / float64(summary.Count)
		duration := float64(item.GridObservations * 15)
		summary.AverageDurationMinutes += (duration - summary.AverageDurationMinutes) / float64(summary.Count)
		result.FirstRejectionReasons[diagnostic.reason] = summary
	}
	return result
}

func diagnoseEpisodeFunnel(rows []signalresearch.MarketStructureObservation, features map[int64]map[string]float64, start int, end int, direction int, thresholds stateMachineThresholds) episodeFunnelResult {
	result := episodeFunnelResult{reason: "no_compression"}
	compressionCount, compressionActive := 0, false
	for index := start; index < end; index++ {
		f := features[rows[index].Features.AsOfMS]
		compressed := f["15m.range_width_20_bps"] <= thresholds.CompressionWidthBPS && f["15m.range_expansion_5_20"] <= thresholds.CompressionExpansion
		if compressed {
			compressionCount++
			if compressionCount >= 2 {
				compressionActive, result.compression = true, true
			}
		}
		breakout := f["15m.breakout_distance_20_bps"]
		if !compressionActive || float64(direction)*breakout <= 0 {
			if !compressed {
				compressionCount, compressionActive = 0, false
			}
			continue
		}
		result.breakout = true
		if f["15m.volume_ratio_5_20"] < thresholds.BreakoutVolumeRatio {
			continue
		}
		result.volume = true
		boundary := f["15m.prior_high_20"]
		if direction < 0 {
			boundary = f["15m.prior_low_20"]
		}
		for confirmIndex := index + 1; confirmIndex < min(end, index+5); confirmIndex++ {
			confirm := features[rows[confirmIndex].Features.AsOfMS]
			holds := (direction > 0 && confirm["15m.close"] >= boundary) || (direction < 0 && confirm["15m.close"] <= boundary)
			if !holds {
				continue
			}
			result.hold = true
			if float64(direction)*confirm["15m.return_5_bps"] <= 0 {
				continue
			}
			result.positiveReturn = true
			position := confirm["15m.range_position_20"]
			if direction < 0 {
				position = 1 - position
			}
			if position < thresholds.ConfirmRangePosition {
				continue
			}
			result.position = true
			if float64(direction)*confirm["15m.direction_balance_20"] <= 0 {
				continue
			}
			result.balance, result.entry = true, true
		}
	}
	switch {
	case result.entry:
		result.reason = "confirmed"
	case result.position:
		result.reason = "direction_balance_rejected"
	case result.positiveReturn:
		result.reason = "range_position_rejected"
	case result.hold:
		result.reason = "return_rejected"
	case result.volume:
		result.reason = "boundary_not_held"
	case result.breakout:
		result.reason = "volume_rejected"
	case result.compression:
		result.reason = "no_directional_breakout"
	}
	return result
}

func appendFunnelStages(stages *stateMachineFunnelStages, result episodeFunnelResult) {
	stages.Episodes++
	if result.compression {
		stages.CompressionSeen++
	}
	if result.breakout {
		stages.DirectionalBreakoutSeen++
	}
	if result.volume {
		stages.VolumeConfirmed++
	}
	if result.hold {
		stages.BoundaryHeld++
	}
	if result.positiveReturn {
		stages.PositiveDirectionReturn++
	}
	if result.position {
		stages.RangePositionConfirmed++
	}
	if result.balance {
		stages.DirectionBalanceConfirmed++
	}
	if result.entry {
		stages.StateMachineEntry++
	}
}

func replayStateMachine(rows []signalresearch.MarketStructureObservation, forwards map[int64]map[int]signalresearch.ForwardMetrics, episodes []episode, pathFeatures map[int64]map[string]float64, start int, end int, thresholds stateMachineThresholds, days float64) stateMachineSplit {
	result := stateMachineSplit{Observations: end - start, ExitReasons: map[string]int{}, TradesDetail: []stateMachineTrade{}}
	state, compressionCount, direction, candidateAge := "idle", 0, 0, 0
	boundary, entryPrice, entryMFE, entryMAE := 0.0, 0.0, 0.0, 0.0
	entryIndex := -1
	covered := make([]bool, len(episodes))
	closeTrade := func(index int, reason string) {
		closePrice := pathFeatures[rows[index].Features.AsOfMS]["15m.close"]
		tradeReturn := float64(direction) * (closePrice/entryPrice - 1) * 10000
		trade := stateMachineTrade{Direction: map[int]string{-1: "down", 1: "up"}[direction], EntryMS: rows[entryIndex].Features.AsOfMS, ExitMS: rows[index].Features.AsOfMS, ExitReason: reason, HoldingObservations: index - entryIndex, ReturnBPS: tradeReturn, Forward40MFEBPS: entryMFE, Forward40MAEBPS: entryMAE}
		result.TradesDetail = append(result.TradesDetail, trade)
		result.ExitReasons[reason]++
	}
	for index := start; index < end; index++ {
		f := pathFeatures[rows[index].Features.AsOfMS]
		compressed := f["15m.range_width_20_bps"] <= thresholds.CompressionWidthBPS && f["15m.range_expansion_5_20"] <= thresholds.CompressionExpansion
		switch state {
		case "idle":
			if compressed {
				compressionCount++
			} else {
				compressionCount = 0
			}
			if compressionCount >= 2 {
				state = "compression"
				result.CompressionEntries++
			}
		case "compression":
			breakout := f["15m.breakout_distance_20_bps"]
			if breakout != 0 && f["15m.volume_ratio_5_20"] >= thresholds.BreakoutVolumeRatio {
				direction = 1
				boundary = f["15m.prior_high_20"]
				if breakout < 0 {
					direction = -1
					boundary = f["15m.prior_low_20"]
				}
				state, candidateAge = "breakout_candidate", 0
				result.BreakoutCandidates++
			} else if !compressed {
				state, compressionCount = "idle", 0
			}
		case "breakout_candidate":
			candidateAge++
			directionalReturn := float64(direction) * f["15m.return_5_bps"]
			directionalPosition := f["15m.range_position_20"]
			if direction < 0 {
				directionalPosition = 1 - directionalPosition
			}
			holds := (direction > 0 && f["15m.close"] >= boundary) || (direction < 0 && f["15m.close"] <= boundary)
			if holds && directionalReturn > 0 && directionalPosition >= thresholds.ConfirmRangePosition && float64(direction)*f["15m.direction_balance_20"] > 0 {
				state, entryIndex, entryPrice = "confirmed_trend", index, f["15m.close"]
				entryMFE, entryMAE, _ = directionalPath(forwards[rows[index].Features.AsOfMS][40], direction)
				result.ConfirmedTrends++
				if direction > 0 {
					result.LongTrades++
				} else {
					result.ShortTrades++
				}
			} else if !holds || directionalReturn <= thresholds.FailureReturnBPS || candidateAge >= 4 {
				state, compressionCount = "idle", 0
				result.FailedBreakouts++
			}
		case "confirmed_trend":
			directionalShort := float64(direction) * f["15m.return_5_bps"]
			directionalLong := float64(direction) * f["15m.return_20_bps"]
			position := f["15m.range_position_20"]
			if direction < 0 {
				position = 1 - position
			}
			reason := ""
			if directionalShort < 0 && (f["15m.range_expansion_5_20"] >= thresholds.ExhaustionExpansion || position < .5) {
				reason = "exhaustion"
			}
			if reason == "" && (directionalLong <= 0 || f["15m.path_efficiency_20"] < thresholds.TrendPathEfficiency) {
				reason = "trend_lost"
			}
			if reason == "" && index-entryIndex >= 16 {
				reason = "timeout_4h"
			}
			if reason != "" {
				closeTrade(index, reason)
				state, compressionCount = "idle", 0
			}
		}
	}
	if state == "confirmed_trend" {
		closeTrade(end-1, "end_of_split")
	}
	result.Trades = len(result.TradesDetail)
	gains, losses := 0.0, 0.0
	for _, trade := range result.TradesDetail {
		result.AverageReturnBPS += trade.ReturnBPS
		result.AverageMFEBPS += trade.Forward40MFEBPS
		result.AverageMAEBPS += trade.Forward40MAEBPS
		if trade.ReturnBPS > 0 {
			result.WinningTrades++
			gains += trade.ReturnBPS
		} else {
			losses -= trade.ReturnBPS
		}
		entry := sortSearchRow(rows, trade.EntryMS)
		for episodeIndex, item := range episodes {
			if entry >= max(start, item.startIndex-4) && entry < min(end, item.endIndex) && trade.Direction == item.Direction {
				covered[episodeIndex] = true
			}
		}
	}
	if result.Trades > 0 {
		count := float64(result.Trades)
		result.WinRate = float64(result.WinningTrades) / count
		result.AverageReturnBPS /= count
		result.AverageMFEBPS /= count
		result.AverageMAEBPS /= count
	}
	if losses > 0 {
		result.ProfitFactor = gains / losses
	}
	for episodeIndex, item := range episodes {
		if item.startIndex >= start && item.startIndex < end {
			result.Episodes++
			if covered[episodeIndex] {
				result.CoveredEpisodes++
			}
		}
	}
	result.EpisodeCoverageRate = ratio(result.CoveredEpisodes, result.Episodes)
	if days > 0 {
		result.TradesPerDay = float64(result.Trades) / days
	}
	return result
}

func sortSearchRow(rows []signalresearch.MarketStructureObservation, asOfMS int64) int {
	low, high := 0, len(rows)
	for low < high {
		middle := (low + high) / 2
		if rows[middle].Features.AsOfMS < asOfMS {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}
