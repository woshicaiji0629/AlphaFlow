package main

import (
	"fmt"
	"math"
	"testing"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
)

func TestDatasetSeriesSelectsExactSymbolAndInterval(t *testing.T) {
	dataset := reader.Dataset{Series: []reader.SeriesResult{
		{Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "3m"}, Result: reader.Result{Klines: []marketmodel.Kline{{OpenTime: 1}}}},
		{Key: reader.SeriesKey{Symbol: "ETHUSDT", Interval: "15m"}, Result: reader.Result{Klines: []marketmodel.Kline{{OpenTime: 2}}}},
	}}
	bars, err := datasetSeries(dataset, "ETHUSDT", "3m")
	if err != nil || len(bars) != 1 || bars[0].OpenTime != 1 {
		t.Fatalf("bars=%#v err=%v", bars, err)
	}
	if _, err := datasetSeries(dataset, "BTCUSDT", "3m"); err == nil {
		t.Fatal("expected missing series error")
	}
}

func TestClassifyEpisodeObservation(t *testing.T) {
	ordinaryUp := signalresearch.ForwardMetrics{
		DirectionReturnBps: 110, PathEfficiency: 0.12, DirectionalAdvantage: 0.70, DominantRetention: 0.65,
	}
	if label, direction := classifyEpisodeObservation(ordinaryUp, augustEpisodeThresholds); label != "trend_up" || direction != 1 {
		t.Fatalf("ordinary up label=%q direction=%d", label, direction)
	}
	startDown := signalresearch.ForwardMetrics{
		MidpointReturnBps: 10, LateReturnBps: -70, PhaseExpansion: 1.4, DominantExcursionPosition: 0.8,
		DominantGivebackRatio: 0.3, PathEfficiency: 0.10, DominantExcursionIsUpward: false,
	}
	if label, direction := classifyEpisodeObservation(startDown, augustEpisodeThresholds); label != "trend_start" || direction != -1 {
		t.Fatalf("trend start label=%q direction=%d", label, direction)
	}
	exhaustion := signalresearch.ForwardMetrics{
		DominantExcursionBps: 180, DominantExcursionPosition: 0.5, DominantGivebackRatio: 0.6, DominantRetention: 0.4,
	}
	if label, direction := classifyEpisodeObservation(exhaustion, augustEpisodeThresholds); label != "trend_exhaustion" || direction != 0 {
		t.Fatalf("exhaustion label=%q direction=%d", label, direction)
	}
}

func TestBuildEpisodeEvaluationBridgesOneNeutralAndRejectsShortRun(t *testing.T) {
	startUp := signalresearch.ForwardMetrics{
		HorizonBars: 80, DirectionReturnBps: 80, MidpointReturnBps: 10, LateReturnBps: 70,
		PhaseExpansion: 1.4, DominantExcursionPosition: 0.8, DominantGivebackRatio: 0.3,
		PathEfficiency: 0.10, DominantExcursionIsUpward: true,
	}
	ordinaryUp := signalresearch.ForwardMetrics{
		HorizonBars: 80, DirectionReturnBps: 110, PathEfficiency: 0.12,
		DirectionalAdvantage: 0.70, DominantRetention: 0.65,
	}
	ordinaryDown := ordinaryUp
	ordinaryDown.DirectionReturnBps = -110
	observations := []signalresearch.MarketStructureObservation{
		observationAt(0, startUp),
		observationAt(15*60*1000, signalresearch.ForwardMetrics{HorizonBars: 80}),
		observationAt(30*60*1000, ordinaryUp),
		observationAt(45*60*1000, signalresearch.ForwardMetrics{HorizonBars: 80}),
		observationAt(60*60*1000, ordinaryDown),
		observationAt(75*60*1000, signalresearch.ForwardMetrics{HorizonBars: 40}),
	}
	evaluation := buildEpisodeEvaluation(observations)
	if len(evaluation.Episodes) != 1 || evaluation.CandidateRuns != 2 || evaluation.RejectedShortRuns != 1 {
		t.Fatalf("evaluation=%+v", evaluation)
	}
	item := evaluation.Episodes[0]
	if item.Direction != "up" || item.GridObservations != 3 || item.DirectionalObservations != 2 || item.TrendStartLeadMinutes != 30 {
		t.Fatalf("episode=%+v", item)
	}
	if math.Abs(evaluation.CoverageRate-2.0/3.0) > 1e-12 || evaluation.MissedRunRate != 0.5 || evaluation.DirectionAgreementRate != 1 {
		t.Fatalf("rates coverage=%v missed=%v agreement=%v", evaluation.CoverageRate, evaluation.MissedRunRate, evaluation.DirectionAgreementRate)
	}
	if evaluation.EpisodesWithLead != 1 || evaluation.AverageTrendStartLeadMinutes != 30 {
		t.Fatalf("lead count=%d average=%v", evaluation.EpisodesWithLead, evaluation.AverageTrendStartLeadMinutes)
	}
}

func TestBuildFeatureEvaluationSeparatesCohorts(t *testing.T) {
	observations := make([]signalresearch.MarketStructureObservation, 16)
	for index := range observations {
		observations[index] = observationAt(int64(index)*15*60*1000, signalresearch.ForwardMetrics{HorizonBars: 80})
		observations[index].Features.Numeric = map[string]float64{"15m.test": float64(index)}
		observations[index].Features.Signals = map[string]string{"15m.state": "neutral"}
	}
	observations[5].Features.Numeric["15m.test"] = 100
	observations[5].Features.Signals["15m.state"] = "bull"
	observations[12].Forward = signalresearch.ForwardMetrics{
		HorizonBars: 80, DirectionReturnBps: 110, PathEfficiency: 0.12,
		DirectionalAdvantage: 0.70, DominantRetention: 0.65,
	}
	episodes := []episode{{startIndex: 5, endIndex: 8}}
	evaluation := buildFeatureEvaluation(observations, episodes)
	if evaluation.CohortSamples["episode_start"] != 1 || evaluation.CohortSamples["pre_episode_60m"] != 4 {
		t.Fatalf("cohort samples=%v", evaluation.CohortSamples)
	}
	if evaluation.CohortSamples["consolidation"] != 4 || evaluation.CohortSamples["rejected_direction_start"] != 1 {
		t.Fatalf("cohort samples=%v", evaluation.CohortSamples)
	}
	numeric := evaluation.Numeric["15m.test"]["episode_start"]
	if numeric.Count != 1 || numeric.Mean != 100 || numeric.DeltaFromConsolidation <= 0 {
		t.Fatalf("numeric=%+v", numeric)
	}
	signal := evaluation.Signals["15m.state"]["episode_start"]["bull"]
	if signal.Count != 1 || signal.Rate != 1 || signal.ConsolidationRate != 0 || signal.RateDelta != 1 {
		t.Fatalf("signal=%+v", signal)
	}
}

func TestExtractRawPathFeaturesUsesOnlyAvailableBars(t *testing.T) {
	bars := make([]marketmodel.Kline, 41)
	for index := range bars {
		closePrice := 100 + float64(index)
		bars[index] = marketmodel.Kline{
			OpenTime: int64(index * 3), CloseTime: int64(index*3 + 2), IsClosed: true,
			Open: fmt.Sprintf("%f", closePrice-0.5), High: fmt.Sprintf("%f", closePrice+1),
			Low: fmt.Sprintf("%f", closePrice-1), Close: fmt.Sprintf("%f", closePrice), Volume: "10",
		}
	}
	features, err := extractRawPathFeatures(bars[40].CloseTime, bars, 40, bars, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 32 || features["3m.return_5_bps"] <= 0 || features["15m.direction_balance_20"] != 1 {
		t.Fatalf("features=%v", features)
	}
	if _, err := extractRawPathFeatures(bars[40].CloseTime-1, bars, 40, bars, 40); err == nil {
		t.Fatal("expected future bar rejection")
	}
}

func TestReplayStateMachineConfirmsAndExitsExhaustion(t *testing.T) {
	rows := make([]signalresearch.MarketStructureObservation, 6)
	paths := map[int64]map[string]float64{}
	for index := range rows {
		asOf := int64(index * 15 * 60 * 1000)
		rows[index] = observationAt(asOf, signalresearch.ForwardMetrics{HorizonBars: 80})
		paths[asOf] = map[string]float64{
			"15m.close": 100, "15m.range_width_20_bps": 200, "15m.range_expansion_5_20": 1,
			"15m.volume_ratio_5_20": 1, "15m.range_position_20": .5,
			"15m.return_5_bps": 0, "15m.return_20_bps": 0,
			"15m.direction_balance_20": 0, "15m.path_efficiency_20": .7,
		}
	}
	paths[0]["15m.range_width_20_bps"], paths[0]["15m.range_expansion_5_20"] = 50, .5
	paths[15*60*1000]["15m.range_width_20_bps"], paths[15*60*1000]["15m.range_expansion_5_20"] = 50, .5
	breakout := paths[30*60*1000]
	breakout["15m.breakout_distance_20_bps"], breakout["15m.volume_ratio_5_20"] = 10, 2
	breakout["15m.prior_high_20"], breakout["15m.close"] = 100, 101
	confirm := paths[45*60*1000]
	confirm["15m.close"], confirm["15m.return_5_bps"] = 102, 10
	confirm["15m.range_position_20"], confirm["15m.direction_balance_20"] = .8, .5
	progress := paths[60*60*1000]
	progress["15m.close"], progress["15m.return_5_bps"], progress["15m.return_20_bps"] = 103, 5, 10
	progress["15m.range_position_20"] = .8
	exhaustion := paths[75*60*1000]
	exhaustion["15m.close"], exhaustion["15m.return_5_bps"], exhaustion["15m.return_20_bps"] = 101, -5, 10
	exhaustion["15m.range_expansion_5_20"], exhaustion["15m.range_position_20"] = 2.5, .4
	forwards := map[int64]map[int]signalresearch.ForwardMetrics{
		45 * 60 * 1000: {40: {MaxUpsideBps: 100, MaxDownsideBps: 40}},
	}
	thresholds := stateMachineThresholds{
		CompressionWidthBPS: 100, CompressionExpansion: .8, BreakoutVolumeRatio: 1.5,
		ConfirmRangePosition: .6, FailureReturnBPS: -10, TrendPathEfficiency: .5, ExhaustionExpansion: 2,
	}
	result := replayStateMachine(rows, forwards, []episode{{Direction: "up", startIndex: 3, endIndex: 6}}, paths, 0, 6, thresholds, 1)
	if result.CompressionEntries != 1 || result.BreakoutCandidates != 1 || result.ConfirmedTrends != 1 || result.Trades != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.ExitReasons["exhaustion"] != 1 || result.CoveredEpisodes != 1 || result.TradesDetail[0].ReturnBPS >= 0 {
		t.Fatalf("result=%+v", result)
	}
	funnel := buildStateMachineFunnel(rows, []episode{{Direction: "up", startIndex: 3, endIndex: 6, GridObservations: 3}}, paths, 0, 6, thresholds)
	if funnel.All.Episodes != 1 || funnel.All.StateMachineEntry != 1 || funnel.Up.StateMachineEntry != 1 {
		t.Fatalf("funnel=%+v", funnel)
	}
	if funnel.FirstRejectionReasons["confirmed"].Count != 1 {
		t.Fatalf("funnel=%+v", funnel)
	}
}

func observationAt(asOfMS int64, metrics signalresearch.ForwardMetrics) signalresearch.MarketStructureObservation {
	return signalresearch.MarketStructureObservation{
		Features: signalresearch.MarketStructureFeatureSnapshot{AsOfMS: asOfMS},
		Forward:  metrics,
	}
}

func TestMatchesShapeMirrorsCompressionBreakoutDirection(t *testing.T) {
	rows := make([]signalresearch.MarketStructureObservation, 9)
	features := map[int64]map[string]float64{}
	for index := range rows {
		asOf := int64(index)
		rows[index] = observationAt(asOf, signalresearch.ForwardMetrics{HorizonBars: 80})
		features[asOf] = map[string]float64{"15m.range_width_20_bps": 200, "15m.range_expansion_5_20": 1, "15m.range_position_20": .5}
	}
	features[0]["15m.range_width_20_bps"] = 80
	features[8]["15m.range_expansion_5_20"] = 1.3
	features[8]["15m.breakout_distance_20_bps"] = -20
	features[8]["15m.range_position_20"] = .2
	thresholds := shapeThresholds{NarrowWidthBPS: 100, ExpansionP60: 1.2}
	if !matchesShape(features, rows, 8, -1, 0, thresholds) {
		t.Fatal("expected mirrored down compression breakout")
	}
	if matchesShape(features, rows, 8, 1, 0, thresholds) {
		t.Fatal("down breakout must not match up direction")
	}
}

func TestEvaluateShapeSplitSeparatesEpisodeAndControlAlerts(t *testing.T) {
	rows := make([]signalresearch.MarketStructureObservation, 12)
	forwards := map[int64]map[int]signalresearch.ForwardMetrics{}
	for index := range rows {
		rows[index] = observationAt(int64(index), signalresearch.ForwardMetrics{HorizonBars: 80})
		forwards[int64(index)] = map[int]signalresearch.ForwardMetrics{20: {DirectionReturnBps: 30, MaxUpsideBps: 50, MaxDownsideBps: 10}}
	}
	episodes := []episode{{Direction: "up", startIndex: 8, endIndex: 11}}
	result := evaluateShapeSplit(rows, forwards, episodes, []shapeAlert{{index: 6, direction: 1}, {index: 11, direction: 1}}, 0, len(rows))
	if result.TrueAlerts != 1 || result.FalseAlerts != 1 || result.CoveredEpisodes != 1 || result.AverageLeadMinutes != 30 {
		t.Fatalf("result=%+v", result)
	}
	if result.Outcomes[20].AverageNetOpportunityBPS != 34 || result.DirectionAccuracy != 1 {
		t.Fatalf("outcomes=%+v accuracy=%v", result.Outcomes, result.DirectionAccuracy)
	}
}

func TestMarketStructureCommandHelpers(t *testing.T) {
	if !contains([]string{"15m", "30m"}, "30m") || contains([]string{"15m"}, "30m") {
		t.Fatal("contains returned unexpected result")
	}
	if value, err := parsePositive("100.5"); err != nil || value != 100.5 {
		t.Fatalf("value=%v err=%v", value, err)
	}
	if _, err := parsePositive("0"); err == nil {
		t.Fatal("expected invalid price error")
	}
	bars := []marketmodel.Kline{{OpenTime: 0}, {OpenTime: 3}, {OpenTime: 6}}
	if !continuousFuture(bars, 0, 2, 3) {
		t.Fatal("expected continuous future")
	}
	bars[1].OpenTime = 4
	if continuousFuture(bars, 0, 2, 3) {
		t.Fatal("expected discontinuous future")
	}
}
