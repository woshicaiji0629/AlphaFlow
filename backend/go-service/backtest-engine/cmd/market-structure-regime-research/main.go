package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

type report struct {
	Version                      string                                      `json:"version"`
	RunID                        string                                      `json:"run_id"`
	SampleStartMS                int64                                       `json:"sample_start_ms"`
	SampleEndMS                  int64                                       `json:"sample_end_ms"`
	ObservationInterval          string                                      `json:"observation_interval"`
	ForwardInterval              string                                      `json:"forward_interval"`
	Observations                 []signalresearch.MarketStructureObservation `json:"observations"`
	EpisodeEvaluation            episodeEvaluation                           `json:"episode_evaluation"`
	FeatureEvaluation            featureEvaluation                           `json:"feature_evaluation"`
	MarketStateMachineEvaluation marketStateMachineEvaluation                `json:"market_state_machine_evaluation"`
}

type episodeThresholds struct {
	DirectionReturnBPS    float64 `json:"direction_return_bps"`
	PathEfficiency        float64 `json:"path_efficiency"`
	DirectionalAdvantage  float64 `json:"directional_advantage"`
	Retention             float64 `json:"retention"`
	DominantExcursionBPS  float64 `json:"dominant_excursion_bps"`
	EarlyPosition         float64 `json:"early_position"`
	HighGiveback          float64 `json:"high_giveback"`
	LowRetention          float64 `json:"low_retention"`
	SmallMidpointBPS      float64 `json:"small_midpoint_bps"`
	LargeLateReturnBPS    float64 `json:"large_late_return_bps"`
	LowGiveback           float64 `json:"low_giveback"`
	PhaseExpansion        float64 `json:"phase_expansion"`
	StartPathEfficiency   float64 `json:"start_path_efficiency"`
	HighVolatilityBPS     float64 `json:"high_volatility_bps"`
	HighTotalExcursionBPS float64 `json:"high_total_excursion_bps"`
}

var augustEpisodeThresholds = episodeThresholds{
	DirectionReturnBPS: 100.7072135785007, PathEfficiency: 0.11322573368060682,
	DirectionalAdvantage: 0.6904520060944521, Retention: 0.6389409559512711,
	DominantExcursionBPS: 171.72514197441728, EarlyPosition: 0.7,
	HighGiveback: 0.575436115040071, LowRetention: 0.424563884959929,
	SmallMidpointBPS: 41.28486342515023, LargeLateReturnBPS: 66.61440230567524,
	LowGiveback: 0.3610590440487289, PhaseExpansion: 1.3871212121212615,
	StartPathEfficiency: 0.08912263535551354,
	HighVolatilityBPS:   17.979443952006225, HighTotalExcursionBPS: 265.476826423037,
}

type episode struct {
	Direction               string  `json:"direction"`
	StartMS                 int64   `json:"start_ms"`
	EndMS                   int64   `json:"end_ms"`
	GridObservations        int     `json:"grid_observations"`
	DirectionalObservations int     `json:"directional_observations"`
	DirectionAgreementRate  float64 `json:"direction_agreement_rate"`
	TrendStartLeadMinutes   int     `json:"trend_start_lead_minutes,omitempty"`
	startIndex              int
	endIndex                int
}

type episodeEvaluation struct {
	Version                        string            `json:"version"`
	HorizonBars                    int               `json:"horizon_bars"`
	ObservationMinutes             int               `json:"observation_minutes"`
	BridgeNeutralObservations      int               `json:"bridge_neutral_observations"`
	MinimumGridObservations        int               `json:"minimum_grid_observations"`
	Thresholds                     episodeThresholds `json:"thresholds"`
	LabelCounts                    map[string]int    `json:"label_counts"`
	DirectionalObservations        int               `json:"directional_observations"`
	CoveredDirectionalObservations int               `json:"covered_directional_observations"`
	CoverageRate                   float64           `json:"coverage_rate"`
	CandidateRuns                  int               `json:"candidate_runs"`
	RejectedShortRuns              int               `json:"rejected_short_runs"`
	MissedRunRate                  float64           `json:"missed_run_rate"`
	DirectionAgreementRate         float64           `json:"direction_agreement_rate"`
	EpisodesWithLead               int               `json:"episodes_with_lead"`
	AverageTrendStartLeadMinutes   float64           `json:"average_trend_start_lead_minutes"`
	Episodes                       []episode         `json:"episodes"`
}

func main() {
	configPath := flag.String("config", "configs/market-structure-regime-research.ethusdt-training.toml", "research config path")
	outputPath := flag.String("output", "", "JSON output path; defaults to result.report_json_path")
	flag.Parse()
	if err := run(*configPath, *outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(configPath string, outputPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load research config: %w", err)
	}
	if len(cfg.Data.Symbols) != 1 || cfg.Data.Interval != "3m" {
		return fmt.Errorf("market structure research requires one symbol and 3m forward interval")
	}
	if !contains(cfg.Data.ConfirmIntervals, "15m") || !contains(cfg.Data.ConfirmIntervals, "30m") {
		return fmt.Errorf("market structure research requires 15m and 30m confirm intervals")
	}
	start, err := config.StartTime(cfg)
	if err != nil {
		return err
	}
	end, err := config.EndTime(cfg)
	if err != nil {
		return err
	}
	intervalMS, err := marketmodel.IntervalMillis(cfg.Data.Interval)
	if err != nil {
		return err
	}
	maxHorizon := signalresearch.DefaultForwardHorizons[len(signalresearch.DefaultForwardHorizons)-1]

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return err
	}
	store, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout, SkipSchemaInit: true,
	})
	if err != nil {
		return err
	}
	defer store.Close()
	klineReader, err := reader.New(store)
	if err != nil {
		return err
	}
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbols: cfg.Data.Symbols,
		Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		Start: start.UnixMilli(), End: end.UnixMilli() + int64(maxHorizon)*intervalMS, WarmupBars: cfg.Data.WarmupBars,
	})
	if err != nil {
		return err
	}
	entryBars, err := datasetSeries(dataset, cfg.Data.Symbols[0], cfg.Data.Interval)
	if err != nil {
		return err
	}
	indexByOpenTime := make(map[int64]int, len(entryBars))
	for index, bar := range entryBars {
		indexByOpenTime[bar.OpenTime] = index
	}
	fifteenBars, err := datasetSeries(dataset, cfg.Data.Symbols[0], "15m")
	if err != nil {
		return err
	}
	fifteenIndexByCloseTime := make(map[int64]int, len(fifteenBars))
	for index, bar := range fifteenBars {
		fifteenIndexByCloseTime[bar.CloseTime] = index
	}
	rawPathFeatures := map[int64]map[string]float64{}
	target := strategy.Target{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbol: cfg.Data.Symbols[0],
		Interval: cfg.Data.Interval, Scope: strategy.PositionScopeBacktest, RunID: cfg.Runtime.RunID,
	}
	builder, err := simulator.NewSnapshotBuilder(simulator.SnapshotBuilderOptions{
		Dataset: dataset, Target: target, Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		IndicatorOptions: indicatorcalc.DefaultOptions(), CalculationWindow: int(cfg.Data.WarmupBars),
		IndicatorBatchSize: cfg.Data.IndicatorBatchSize, IndicatorConcurrency: cfg.Data.IndicatorConcurrency,
	})
	if err != nil {
		return err
	}
	iterator, err := builder.Iterator(ctx)
	if err != nil {
		return err
	}
	defer iterator.Close()

	result := report{
		Version: signalresearch.MarketStructureRegimeFeatureVersion, RunID: cfg.Runtime.RunID,
		SampleStartMS: start.UnixMilli(), SampleEndMS: end.UnixMilli(),
		ObservationInterval: "15m", ForwardInterval: cfg.Data.Interval,
		Observations: make([]signalresearch.MarketStructureObservation, 0, 20000),
	}
	lastObservationClose := int64(0)
	for {
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		snapshot, ok := item.Snapshots[cfg.Data.Interval]
		if !ok || snapshot.Current.OpenTime < start.UnixMilli() || snapshot.Current.OpenTime >= end.UnixMilli() {
			continue
		}
		fifteen, ok := snapshot.Timeframes["15m"]
		if !ok || fifteen.Indicator.CloseTime != snapshot.Current.CloseTime || fifteen.Indicator.CloseTime == lastObservationClose {
			continue
		}
		lastObservationClose = fifteen.Indicator.CloseTime
		features, err := signalresearch.ExtractMarketStructureFeatures(snapshot)
		if err != nil {
			return err
		}
		originIndex, ok := indexByOpenTime[snapshot.Current.OpenTime]
		if !ok {
			return fmt.Errorf("entry bar open_time=%d missing from dataset", snapshot.Current.OpenTime)
		}
		fifteenIndex, ok := fifteenIndexByCloseTime[fifteen.Indicator.CloseTime]
		if !ok {
			return fmt.Errorf("15m bar close_time=%d missing from dataset", fifteen.Indicator.CloseTime)
		}
		pathFeatures, err := extractRawPathFeatures(snapshot.AsOf, entryBars, originIndex, fifteenBars, fifteenIndex)
		if err != nil {
			return fmt.Errorf("extract raw path features at as_of=%d: %w", snapshot.AsOf, err)
		}
		rawPathFeatures[snapshot.AsOf] = pathFeatures
		entryPrice, err := parsePositive(snapshot.Current.Close)
		if err != nil {
			return err
		}
		for _, horizon := range signalresearch.DefaultForwardHorizons {
			if !continuousFuture(entryBars, originIndex, horizon, intervalMS) {
				return fmt.Errorf("forward horizon %d unavailable at open_time=%d", horizon, snapshot.Current.OpenTime)
			}
			metrics, err := signalresearch.CalculateForwardMetrics(entryPrice, entryBars[originIndex+1:], horizon)
			if err != nil {
				return fmt.Errorf("calculate forward metrics at open_time=%d horizon=%d: %w", snapshot.Current.OpenTime, horizon, err)
			}
			result.Observations = append(result.Observations, signalresearch.MarketStructureObservation{Features: features, Forward: metrics})
		}
	}
	result.EpisodeEvaluation = buildEpisodeEvaluation(result.Observations)
	result.FeatureEvaluation = buildFeatureEvaluation(result.Observations, result.EpisodeEvaluation.Episodes)
	result.MarketStateMachineEvaluation = buildMarketStateMachineEvaluation(result.Observations, result.EpisodeEvaluation.Episodes, rawPathFeatures)
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	path := strings.TrimSpace(outputPath)
	if path == "" {
		path = strings.TrimSpace(cfg.Result.ReportJSONPath)
	}
	if path == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write market structure report: %w", err)
	}
	return nil
}

type labeledObservation struct {
	asOfMS    int64
	label     string
	direction int
	metrics   signalresearch.ForwardMetrics
}

func buildEpisodeEvaluation(observations []signalresearch.MarketStructureObservation) episodeEvaluation {
	const horizon = 80
	const minimumGridObservations = 3
	evaluation := episodeEvaluation{
		Version: "market-structure-episode.v1", HorizonBars: horizon, ObservationMinutes: 15,
		BridgeNeutralObservations: 1, MinimumGridObservations: minimumGridObservations,
		Thresholds: augustEpisodeThresholds, LabelCounts: map[string]int{}, Episodes: []episode{},
	}
	labeled := make([]labeledObservation, 0, len(observations)/3)
	for _, observation := range observations {
		if observation.Forward.HorizonBars != horizon {
			continue
		}
		label, direction := classifyEpisodeObservation(observation.Forward, augustEpisodeThresholds)
		evaluation.LabelCounts[label]++
		if direction != 0 {
			evaluation.DirectionalObservations++
		}
		labeled = append(labeled, labeledObservation{asOfMS: observation.Features.AsOfMS, label: label, direction: direction, metrics: observation.Forward})
	}
	bridged := make([]int, len(labeled))
	for index := range labeled {
		bridged[index] = labeled[index].direction
	}
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
		direction := bridged[start]
		end := start + 1
		for end < len(bridged) && bridged[end] == direction {
			end++
		}
		evaluation.CandidateRuns++
		directionalCount := 0
		agreements := 0
		firstOrdinary := int64(0)
		for index := start; index < end; index++ {
			if labeled[index].direction == 0 {
				continue
			}
			directionalCount++
			if (labeled[index].metrics.DirectionReturnBps > 0 && direction > 0) || (labeled[index].metrics.DirectionReturnBps < 0 && direction < 0) {
				agreements++
			}
			if firstOrdinary == 0 && ((direction > 0 && labeled[index].label == "trend_up") || (direction < 0 && labeled[index].label == "trend_down")) {
				firstOrdinary = labeled[index].asOfMS
			}
		}
		if end-start < minimumGridObservations || directionalCount < 2 {
			evaluation.RejectedShortRuns++
			start = end
			continue
		}
		item := episode{
			Direction: map[int]string{-1: "down", 1: "up"}[direction], StartMS: labeled[start].asOfMS,
			EndMS: labeled[end-1].asOfMS, GridObservations: end - start, DirectionalObservations: directionalCount,
			DirectionAgreementRate: float64(agreements) / float64(directionalCount),
			startIndex:             start, endIndex: end,
		}
		if labeled[start].label == "trend_start" && firstOrdinary > labeled[start].asOfMS {
			item.TrendStartLeadMinutes = int((firstOrdinary - labeled[start].asOfMS) / 60000)
			evaluation.EpisodesWithLead++
			evaluation.AverageTrendStartLeadMinutes += float64(item.TrendStartLeadMinutes)
		}
		evaluation.CoveredDirectionalObservations += directionalCount
		evaluation.DirectionAgreementRate += float64(agreements)
		evaluation.Episodes = append(evaluation.Episodes, item)
		start = end
	}
	if evaluation.DirectionalObservations > 0 {
		evaluation.CoverageRate = float64(evaluation.CoveredDirectionalObservations) / float64(evaluation.DirectionalObservations)
	}
	if evaluation.CoveredDirectionalObservations > 0 {
		evaluation.DirectionAgreementRate /= float64(evaluation.CoveredDirectionalObservations)
	}
	if evaluation.CandidateRuns > 0 {
		evaluation.MissedRunRate = float64(evaluation.RejectedShortRuns) / float64(evaluation.CandidateRuns)
	}
	if evaluation.EpisodesWithLead > 0 {
		evaluation.AverageTrendStartLeadMinutes /= float64(evaluation.EpisodesWithLead)
	}
	return evaluation
}

func classifyEpisodeObservation(metrics signalresearch.ForwardMetrics, thresholds episodeThresholds) (string, int) {
	absReturn := abs(metrics.DirectionReturnBps)
	absMidpoint := abs(metrics.MidpointReturnBps)
	absLate := abs(metrics.LateReturnBps)
	absAdvantage := abs(metrics.DirectionalAdvantage)
	ordinary := absReturn >= thresholds.DirectionReturnBPS && metrics.PathEfficiency >= thresholds.PathEfficiency &&
		absAdvantage >= thresholds.DirectionalAdvantage && metrics.DominantRetention >= thresholds.Retention
	exhaustion := metrics.DominantExcursionBps >= thresholds.DominantExcursionBPS &&
		metrics.DominantExcursionPosition <= thresholds.EarlyPosition && metrics.DominantGivebackRatio >= thresholds.HighGiveback &&
		metrics.DominantRetention <= thresholds.LowRetention && !ordinary
	start := absMidpoint <= thresholds.SmallMidpointBPS && absLate >= thresholds.LargeLateReturnBPS &&
		metrics.PhaseExpansion >= thresholds.PhaseExpansion && metrics.DominantExcursionPosition > thresholds.EarlyPosition &&
		metrics.DominantGivebackRatio <= thresholds.LowGiveback && metrics.PathEfficiency >= thresholds.StartPathEfficiency
	if exhaustion {
		return "trend_exhaustion", 0
	}
	if start {
		if metrics.DominantExcursionIsUpward {
			return "trend_start", 1
		}
		return "trend_start", -1
	}
	if ordinary {
		if metrics.DirectionReturnBps > 0 {
			return "trend_up", 1
		}
		return "trend_down", -1
	}
	if metrics.RealizedVolatilityBps >= thresholds.HighVolatilityBPS || metrics.MaxUpsideBps+metrics.MaxDownsideBps >= thresholds.HighTotalExcursionBPS {
		return "high_volatility_range", 0
	}
	return "low_volatility_consolidation", 0
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func continuousFuture(bars []marketmodel.Kline, originIndex int, horizon int, intervalMS int64) bool {
	if originIndex < 0 || horizon <= 0 || intervalMS <= 0 || originIndex+horizon >= len(bars) {
		return false
	}
	originOpenTime := bars[originIndex].OpenTime
	for offset := 1; offset <= horizon; offset++ {
		if bars[originIndex+offset].OpenTime != originOpenTime+int64(offset)*intervalMS {
			return false
		}
	}
	return true
}

func datasetSeries(dataset reader.Dataset, symbol string, interval string) ([]marketmodel.Kline, error) {
	for _, series := range dataset.Series {
		if series.Key.Symbol == symbol && series.Key.Interval == interval {
			return series.Result.Klines, nil
		}
	}
	return nil, fmt.Errorf("dataset series %s %s missing", symbol, interval)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func parsePositive(raw string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse positive price %q", raw)
	}
	return value, nil
}
