package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
)

func main() {
	configPath := flag.String("config", "configs/supertrend-signal-research.ethusdt-20250801-20251201.toml", "research config path")
	fixedStops := flag.String("fixed-stops", "50,70,100,150", "fixed stop margin percentages")
	atrStops := flag.String("atr-stops", "1,1.5,2,2.5", "ATR stop multipliers")
	takeProfits := flag.String("take-profits", "30,50,75,100,150,200,300,500", "take profit margin percentages")
	horizon := flag.Duration("horizon", 12*time.Hour, "maximum observation horizon")
	platformWindowBars := flag.Int("platform-window-bars", 12, "closed entry bars in a trend platform")
	platformMaxRangePct := flag.Float64("platform-max-range-pct", 0.6, "maximum platform high-low range percentage")
	platformMinVolumeRatio := flag.Float64("platform-min-volume-ratio", 1.5, "minimum breakout volume versus platform average")
	platformCooldownBars := flag.Int("platform-cooldown-bars", 20, "same-side breakout cooldown in entry bars")
	impulseLookbackBars := flag.Int("impulse-lookback-bars", 3, "entry bars used to measure an impulse")
	impulseBreakoutBars := flag.Int("impulse-breakout-bars", 10, "prior entry bars used as impulse structure")
	impulseMinMoveATR := flag.Float64("impulse-min-move-atr", 1.5, "minimum short-window move in ATR")
	impulseMinVolumeRatio := flag.Float64("impulse-min-volume-ratio", 1.5, "minimum impulse volume_ratio20")
	impulseCooldownBars := flag.Int("impulse-cooldown-bars", 20, "same-side impulse cooldown in entry bars")
	pullbackTouchDistancePct := flag.Float64("pullback-touch-distance-pct", 0.15, "maximum EMA25 touch distance percentage")
	pullbackResumeBars := flag.Int("pullback-resume-bars", 3, "prior bars crossed to confirm pullback resume")
	pullbackMaxArmedBars := flag.Int("pullback-max-armed-bars", 10, "maximum bars after a pullback touch")
	pullbackMinVolumeRatio := flag.Float64("pullback-min-volume-ratio", 1.0, "minimum resume volume_ratio5")
	pullbackCooldownBars := flag.Int("pullback-cooldown-bars", 20, "same-side pullback resume cooldown in entry bars")
	eventCooldownBars := flag.Int("event-cooldown-bars", 20, "cross-source same-side research event cooldown in entry bars")
	counterTrendWaitBars := flag.Int("counter-trend-wait-bars", 3, "entry bars required after a counter-trend flip")
	counterTrendStructureBars := flag.Int("counter-trend-structure-bars", 3, "prior entry bars crossed by a counter-trend signal")
	counterTrendMinVolumeRatio := flag.Float64("counter-trend-min-volume-ratio", 1.5, "minimum counter-trend volume_ratio20")
	counterTrendSizeFactor := flag.Float64("counter-trend-size-factor", 0.25, "research sizing marker for counter-trend signals")
	counterTrendEnabled := flag.Bool("counter-trend-enabled", false, "enable the experimental counter-trend entry gate")
	validationObservationBars := flag.String("validation-observation-bars", "3,4,5", "entry bars used for early follow-through observations")
	chopWindowBars := flag.Int("chop-window-bars", 60, "entry bars used for chop observations")
	chopMaxEfficiency := flag.Float64("chop-max-efficiency", 0.25, "maximum price efficiency ratio for chop evidence")
	chopMaxADX := flag.Float64("chop-max-adx", 20, "maximum 10m ADX for chop evidence")
	chopMinFlips := flag.Int("chop-min-flips", 3, "minimum 10m Supertrend flips for chop evidence")
	chopMaxSlope := flag.Float64("chop-max-slope", 0.08, "maximum absolute ATR-normalized 10m Supertrend slope")
	chopMaxRangeATR := flag.Float64("chop-max-range-atr", 4, "maximum platform range in 10m ATR")
	chopMinVotes := flag.Int("chop-min-votes", 3, "minimum chop evidence votes")
	chopConfirmBars := flag.Int("chop-confirm-bars", 10, "entry bars required to confirm chop")
	chopExitBars := flag.Int("chop-exit-bars", 5, "entry bars with weak chop evidence required to exit")
	chopBreakoutVolumeRatio := flag.Float64("chop-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for a breakout attempt")
	regimeWindowBars := flag.Int("regime-window-bars", 60, "entry bars used for market regime observations")
	regimeMinVotes := flag.Int("regime-min-dormant-votes", 3, "minimum dormant votes per higher interval")
	regimeMaxMASpreadATR := flag.Float64("regime-max-ma-spread-atr", 0.8, "maximum EMA25/EMA99 spread in ATR")
	regimeMaxMACDAxisATR := flag.Float64("regime-max-macd-axis-atr", 0.2, "maximum MACD axis distance in ATR")
	regimeMaxMACDHistATR := flag.Float64("regime-max-macd-hist-atr", 0.08, "maximum MACD histogram magnitude in ATR")
	regimeMaxMASlopePct := flag.Float64("regime-max-ma-slope-pct", 0.08, "maximum absolute EMA25 slope percentage")
	regimeMaxEfficiency := flag.Float64("regime-max-efficiency", 0.25, "maximum price efficiency for chop lock")
	regimeMaxRangeATR := flag.Float64("regime-max-range-atr", 4, "maximum platform range in entry ATR")
	regimeStallMaxEfficiency := flag.Float64("regime-stall-max-efficiency", 0.10, "maximum local price efficiency for a trend stall")
	regimeStallMaxRangeATR := flag.Float64("regime-stall-max-range-atr", 3.5, "maximum local stall range in higher-timeframe ATR")
	regimeLockBars := flag.Int("regime-lock-bars", 10, "dormant entry bars required to lock new positions")
	regimeUnlockBars := flag.Int("regime-unlock-bars", 5, "non-dormant bars required to leave chop lock")
	regimeBreakoutBars := flag.Int("regime-breakout-bars", 2, "platform-external closes required to arm trend")
	regimeBreakoutVolume := flag.Float64("regime-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for breakout pending")
	regimeMinBreakoutMACDAxis := flag.Float64("regime-min-breakout-macd-axis-atr", 0.3, "minimum MACD axis distance in ATR to arm a breakout")
	regimeVersion := flag.String("regime-version", "v1", "market regime analyzer version: v1, v2, or v3")
	regimeV3NoiseMultiplier := flag.Float64("regime-v3-noise-multiplier", 1.5, "v3 ATR noise threshold multiplier")
	regimeV3CatchUpSpeed := flag.Float64("regime-v3-catch-up-speed", 0.35, "v3 volatility filter catch-up speed")
	regimeV3ConfirmBars := flag.Int("regime-v3-confirm-bars", 2, "v3 state confirmation bars")
	regimeAppendVersionRunID := flag.Bool("regime-append-version-run-id", false, "append the regime version to run_id for side-by-side research")
	regimeRunIDTag := flag.String("regime-run-id-tag", "", "optional research-only suffix appended to run_id")
	researchSkipPersist := flag.Bool("research-skip-persist", false, "run research in memory without persisting detailed rows")
	singlePositionScan := flag.Bool("single-position-scan", false, "compare staged single-position protection parameters in one replay")
	supertrendVersionCompare := flag.Bool("supertrend-version-compare", false, "compare standard, adaptive, and AI Supertrend flips with identical single-position rules")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	platformConfig := signalresearch.PlatformConfig{
		WindowBars: *platformWindowBars, MaxRangePct: *platformMaxRangePct,
		MinVolumeRatio: *platformMinVolumeRatio, CooldownBars: *platformCooldownBars,
	}
	impulseConfig := signalresearch.ImpulseConfig{
		LookbackBars: *impulseLookbackBars, BreakoutBars: *impulseBreakoutBars,
		MinMoveATR: *impulseMinMoveATR, MinVolumeRatio: *impulseMinVolumeRatio, CooldownBars: *impulseCooldownBars,
	}
	pullbackConfig := signalresearch.PullbackConfig{
		TouchDistancePct: *pullbackTouchDistancePct, ResumeBars: *pullbackResumeBars,
		MaxArmedBars: *pullbackMaxArmedBars, MinVolumeRatio: *pullbackMinVolumeRatio, CooldownBars: *pullbackCooldownBars,
	}
	counterTrendConfig := signalresearch.CounterTrendConfig{
		WaitBars: *counterTrendWaitBars, StructureBars: *counterTrendStructureBars,
		MinVolumeRatio: *counterTrendMinVolumeRatio, SizeFactor: *counterTrendSizeFactor,
	}
	chopConfig := signalresearch.ChopConfig{
		WindowBars: *chopWindowBars, MaxEfficiencyRatio: *chopMaxEfficiency, MaxADX: *chopMaxADX,
		MinFlips: *chopMinFlips, MaxNormalizedSlope: *chopMaxSlope, MaxRangeATR: *chopMaxRangeATR,
		MinVotes: *chopMinVotes, ConfirmBars: *chopConfirmBars, ExitBars: *chopExitBars,
		BreakoutVolumeRatio: *chopBreakoutVolumeRatio,
	}
	regimeConfig := marketregime.DefaultConfig()
	regimeConfig.WindowBars = *regimeWindowBars
	regimeConfig.MinDormantVotes = *regimeMinVotes
	regimeConfig.MaxMASpreadATR = *regimeMaxMASpreadATR
	regimeConfig.MaxMACDAxisATR = *regimeMaxMACDAxisATR
	regimeConfig.MaxMACDHistogramATR = *regimeMaxMACDHistATR
	regimeConfig.MaxMASlopePct = *regimeMaxMASlopePct
	regimeConfig.MaxEfficiencyRatio = *regimeMaxEfficiency
	regimeConfig.MaxPlatformRangeATR = *regimeMaxRangeATR
	regimeConfig.StallMaxEfficiency = *regimeStallMaxEfficiency
	regimeConfig.StallMaxRangeATR = *regimeStallMaxRangeATR
	regimeConfig.LockConfirmBars = *regimeLockBars
	regimeConfig.UnlockEvidenceBars = *regimeUnlockBars
	regimeConfig.BreakoutConfirmBars = *regimeBreakoutBars
	regimeConfig.BreakoutVolumeRatio = *regimeBreakoutVolume
	regimeConfig.MinBreakoutMACDAxisATR = *regimeMinBreakoutMACDAxis
	regimeAnalyzer, err := buildRegimeAnalyzer(*regimeVersion, regimeConfig, *regimeV3NoiseMultiplier, *regimeV3CatchUpSpeed, *regimeV3ConfirmBars)
	if err != nil {
		log.Fatal(err)
	}
	if err := run(ctx, *configPath, *fixedStops, *atrStops, *takeProfits, *horizon, platformConfig, impulseConfig, pullbackConfig, *eventCooldownBars, counterTrendConfig, *counterTrendEnabled, *validationObservationBars, chopConfig, regimeAnalyzer, *regimeAppendVersionRunID, *regimeRunIDTag, *researchSkipPersist, *singlePositionScan, *supertrendVersionCompare); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string, fixedText string, atrText string, takeProfitText string, horizon time.Duration, platformConfig signalresearch.PlatformConfig, impulseConfig signalresearch.ImpulseConfig, pullbackConfig signalresearch.PullbackConfig, eventCooldownBars int, counterTrendConfig signalresearch.CounterTrendConfig, counterTrendEnabled bool, validationBarsText string, chopConfig signalresearch.ChopConfig, regimeAnalyzer marketregime.Analyzer, appendVersionRunID bool, runIDTag string, skipPersist bool, scanSinglePosition bool, compareSupertrendVersions bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load research config: %w", err)
	}
	if appendVersionRunID {
		cfg.Runtime.RunID += "-regime-" + string(regimeAnalyzer.Version())
	}
	if tag := strings.TrimSpace(runIDTag); tag != "" {
		cfg.Runtime.RunID += "-" + tag
	}
	if !cfg.ClickHouse.Enabled {
		return fmt.Errorf("clickhouse must be enabled")
	}
	if len(cfg.Data.Symbols) != 1 {
		return fmt.Errorf("signal research requires exactly one symbol")
	}
	fixed, err := parsePositiveList("fixed-stops", fixedText)
	if err != nil {
		return err
	}
	atr, err := parsePositiveList("atr-stops", atrText)
	if err != nil {
		return err
	}
	takeProfits, err := parsePositiveList("take-profits", takeProfitText)
	if err != nil {
		return err
	}
	validationBars, err := parsePositiveIntList("validation-observation-bars", validationBarsText)
	if err != nil {
		return err
	}
	startTime, err := config.StartTime(cfg)
	if err != nil {
		return err
	}
	endTime, err := config.EndTime(cfg)
	if err != nil {
		return err
	}
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return err
	}
	marketStore, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return err
	}
	defer marketStore.Close()
	klineReader, err := reader.New(marketStore)
	if err != nil {
		return err
	}
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbols: cfg.Data.Symbols,
		Interval: cfg.Data.Interval, ConfirmIntervals: cfg.Data.ConfirmIntervals,
		Start: startTime.UnixMilli(), End: endTime.Add(horizon).UnixMilli(), WarmupBars: cfg.Data.WarmupBars,
	})
	if err != nil {
		return err
	}
	replay, err := signalresearch.New(signalresearch.Config{
		RunID: cfg.Runtime.RunID, Leverage: cfg.Sizing.Leverage, Horizon: horizon,
		FixedStopMargin: fixed, ATRStopMultipliers: atr, TakeProfitMargin: takeProfits,
	})
	if err != nil {
		return err
	}
	singlePositionConfig := signalresearch.SinglePositionConfig{
		InitialEquity: cfg.Sizing.InitialEquity, MarginQuote: cfg.Sizing.MarginQuote, Leverage: cfg.Sizing.Leverage,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: horizon, CooldownBars: 2, FeeRate: cfg.Fee.FeeRate, SlippageBps: cfg.Execution.SlippageBps,
	}
	singlePositionReplays, err := buildSinglePositionReplays(singlePositionConfig, scanSinglePosition)
	if err != nil {
		return err
	}
	supertrendComparisonReplays, err := buildSupertrendComparisonReplays(singlePositionConfig, compareSupertrendVersions)
	if err != nil {
		return err
	}
	platformDetector, err := signalresearch.NewPlatformDetector(platformConfig)
	if err != nil {
		return err
	}
	impulseDetector, err := signalresearch.NewImpulseDetector(impulseConfig)
	if err != nil {
		return err
	}
	pullbackDetector, err := signalresearch.NewPullbackDetector(pullbackConfig)
	if err != nil {
		return err
	}
	eventGate, err := signalresearch.NewEventGate(eventCooldownBars)
	if err != nil {
		return err
	}
	validationReplay, err := signalresearch.NewValidationReplay(signalresearch.ValidationConfig{ObservationBars: validationBars})
	if err != nil {
		return err
	}
	chopDetector, err := signalresearch.NewChopDetector(chopConfig)
	if err != nil {
		return err
	}
	chopObservations := make([]signalresearch.ChopObservation, 0, 1024)
	regimeObservations := make([]marketregime.Result, 0, 1024)
	var counterTrendGate *signalresearch.CounterTrendGate
	if counterTrendEnabled {
		counterTrendGate, err = signalresearch.NewCounterTrendGate(counterTrendConfig)
		if err != nil {
			return err
		}
	}
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
	for {
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		snapshot, ok := item.Snapshots[cfg.Data.Interval]
		if !ok {
			return fmt.Errorf("entry snapshot %s missing", cfg.Data.Interval)
		}
		if err := replay.Advance(snapshot.Current); err != nil {
			return err
		}
		for _, item := range singlePositionReplays {
			if err := item.replay.Advance(snapshot.Current); err != nil {
				return err
			}
		}
		for _, item := range supertrendComparisonReplays {
			if err := item.replay.Advance(snapshot.Current); err != nil {
				return err
			}
		}
		if err := validationReplay.Advance(snapshot); err != nil {
			return err
		}
		if observation, ok, err := chopDetector.Update(cfg.Runtime.RunID, snapshot); err != nil {
			return err
		} else if ok && snapshot.Current.OpenTime < endTime.UnixMilli() {
			chopObservations = append(chopObservations, observation)
		}
		var currentRegime *marketregime.Result
		if observation, ok, err := regimeAnalyzer.Analyze(snapshot); err != nil {
			return err
		} else if ok && snapshot.Current.OpenTime < endTime.UnixMilli() {
			regimeObservations = append(regimeObservations, observation)
			currentRegime = &observation
		}
		platformEvents, err := platformDetector.Update(snapshot)
		if err != nil {
			return err
		}
		impulseEvents, err := impulseDetector.Update(snapshot)
		if err != nil {
			return err
		}
		pullbackEvents, err := pullbackDetector.Update(snapshot)
		if err != nil {
			return err
		}
		researchEvents := append(platformEvents, impulseEvents...)
		researchEvents = append(researchEvents, pullbackEvents...)
		eventGate.Advance()
		if snapshot.Current.OpenTime >= endTime.UnixMilli() {
			continue
		}
		singlePositionSides := make([]strategy.SignalSide, 0, 2)
		for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
			sources := supertrend.ResearchTriggerSources(snapshot.Window, side)
			metadataJSON := ""
			metadataParts := make([]string, 0, 3)
			for _, event := range researchEvents {
				if event.Side != side {
					continue
				}
				sources = append(sources, event.Source)
				metadataParts = append(metadataParts, event.MetadataJSON)
			}
			if len(metadataParts) > 0 {
				metadataJSON = `{"events":[` + strings.Join(metadataParts, ",") + `]}`
			}
			if len(sources) == 0 {
				continue
			}
			if counterTrendGate != nil {
				decision, err := counterTrendGate.Evaluate(snapshot, side, sources)
				if err != nil {
					return err
				}
				if !decision.Allow {
					continue
				}
				if decision.MetadataJSON != "" {
					metadataParts = append(metadataParts, decision.MetadataJSON)
					metadataJSON = `{"events":[` + strings.Join(metadataParts, ",") + `]}`
				}
			}
			if !eventGate.Allow(side, sources) {
				continue
			}
			if err := replay.AddSignalWithMetadata(snapshot, side, sources, metadataJSON); err != nil {
				return err
			}
			if err := validationReplay.AddSignal(cfg.Runtime.RunID, snapshot, side, sources); err != nil {
				return err
			}
			singlePositionSides = append(singlePositionSides, side)
		}
		if len(singlePositionSides) == 1 {
			for _, item := range singlePositionReplays {
				if _, err := item.replay.TryEnter(snapshot, singlePositionSides[0], currentRegime); err != nil {
					return err
				}
			}
		} else if len(singlePositionSides) > 1 {
			for _, item := range singlePositionReplays {
				item.replay.SkipConflict()
			}
		}
		for _, item := range supertrendComparisonReplays {
			side, ok := supertrendFlipSide(snapshot.Window, item.flipKey)
			if !ok {
				continue
			}
			item.flipSignals++
			if _, err := item.replay.TryEnter(snapshot, side, currentRegime); err != nil {
				return err
			}
		}
	}
	replay.Finish()
	for _, item := range singlePositionReplays {
		item.replay.Finish()
	}
	for _, item := range supertrendComparisonReplays {
		item.replay.Finish()
	}
	signals, outcomes := replay.Results()
	observations := validationReplay.Results()
	for _, item := range singlePositionReplays {
		singlePositionJSON, err := json.Marshal(item.replay.Summary())
		if err != nil {
			return err
		}
		log.Printf("single position research run_id=%s regime_version=%s variant=%s summary=%s", cfg.Runtime.RunID, regimeAnalyzer.Version(), item.name, singlePositionJSON)
	}
	for _, item := range supertrendComparisonReplays {
		summaryJSON, err := json.Marshal(item.replay.Summary())
		if err != nil {
			return err
		}
		log.Printf("supertrend version comparison run_id=%s regime_version=%s supertrend_version=%s flip_signals=%d summary=%s", cfg.Runtime.RunID, regimeAnalyzer.Version(), item.name, item.flipSignals, summaryJSON)
	}
	if skipPersist {
		log.Printf("signal research completed without persistence run_id=%s regime_version=%s signals=%d outcomes=%d validation_observations=%d chop_observations=%d regime_observations=%d", cfg.Runtime.RunID, regimeAnalyzer.Version(), len(signals), len(outcomes), len(observations), len(chopObservations), len(regimeObservations))
		return nil
	}
	researchStore, err := signalresearch.NewStore(ctx, signalresearch.StoreOptions{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password, DialTimeout: dialTimeout, ReadTimeout: readTimeout,
	})
	if err != nil {
		return err
	}
	defer researchStore.Close()
	if err := researchStore.SaveSignals(ctx, signals, 250); err != nil {
		return err
	}
	if err := researchStore.SaveOutcomes(ctx, outcomes, 1000); err != nil {
		return err
	}
	if err := researchStore.SaveValidationObservations(ctx, observations, 1000); err != nil {
		return err
	}
	if err := researchStore.SaveChopObservations(ctx, chopObservations, 1000); err != nil {
		return err
	}
	if err := researchStore.SaveMarketRegimeObservations(ctx, cfg.Runtime.RunID, regimeObservations, 1000); err != nil {
		return err
	}
	log.Printf("signal research completed run_id=%s regime_version=%s signals=%d outcomes=%d validation_observations=%d chop_observations=%d regime_observations=%d", cfg.Runtime.RunID, regimeAnalyzer.Version(), len(signals), len(outcomes), len(observations), len(chopObservations), len(regimeObservations))
	return nil
}

type namedSinglePositionReplay struct {
	name   string
	replay *signalresearch.SinglePositionReplay
}

type supertrendComparisonReplay struct {
	name        string
	flipKey     string
	flipSignals int
	replay      *signalresearch.SinglePositionReplay
}

func buildSupertrendComparisonReplays(base signalresearch.SinglePositionConfig, enabled bool) ([]*supertrendComparisonReplay, error) {
	if !enabled {
		return nil, nil
	}
	versions := []struct {
		name    string
		flipKey string
	}{
		{name: "standard", flipKey: "supertrend_flip"},
		{name: "adaptive", flipKey: "adaptive_supertrend_flip"},
		{name: "ai", flipKey: "ai_supertrend_flip"},
	}
	result := make([]*supertrendComparisonReplay, 0, len(versions))
	for _, version := range versions {
		replay, err := signalresearch.NewSinglePositionReplay(base)
		if err != nil {
			return nil, fmt.Errorf("build %s Supertrend comparison replay: %w", version.name, err)
		}
		result = append(result, &supertrendComparisonReplay{name: version.name, flipKey: version.flipKey, replay: replay})
	}
	return result, nil
}

func supertrendFlipSide(window strategy.IndicatorWindowView, flipKey string) (strategy.SignalSide, bool) {
	series, ok := window.Signal(flipKey)
	if !ok {
		return strategy.SignalSideHold, false
	}
	switch strings.ToLower(strings.TrimSpace(series.Latest)) {
	case "up", "bull", "buy", "long":
		return strategy.SignalSideBuy, true
	case "down", "bear", "sell", "short":
		return strategy.SignalSideSell, true
	default:
		return strategy.SignalSideHold, false
	}
}

func buildSinglePositionReplays(base signalresearch.SinglePositionConfig, scan bool) ([]namedSinglePositionReplay, error) {
	configs := []struct {
		name   string
		config signalresearch.SinglePositionConfig
	}{{name: "baseline", config: base}}
	if scan {
		add := func(name string, stop float64, breakEven float64, trailing float64, drawdown float64) {
			config := base
			config.InitialStopBps = stop
			config.BreakEvenTriggerBps = breakEven
			config.TrailingTriggerBps = trailing
			config.TrailingDrawdownBps = drawdown
			configs = append(configs, struct {
				name   string
				config signalresearch.SinglePositionConfig
			}{name: name, config: config})
		}
		add("s50-be30-t75-d30", 50, 30, 75, 30)
		add("s50-be40-t75-d30", 50, 40, 75, 30)
		add("s50-be40-t100-d30", 50, 40, 100, 30)
		add("s50-be40-t100-d40", 50, 40, 100, 40)
		add("s70-be40-t100-d40", 70, 40, 100, 40)
		add("s70-be50-t100-d40", 70, 50, 100, 40)
	}
	result := make([]namedSinglePositionReplay, 0, len(configs))
	for _, item := range configs {
		replay, err := signalresearch.NewSinglePositionReplay(item.config)
		if err != nil {
			return nil, fmt.Errorf("build single position variant %s: %w", item.name, err)
		}
		result = append(result, namedSinglePositionReplay{name: item.name, replay: replay})
	}
	return result, nil
}

func buildRegimeAnalyzer(versionText string, v1Config marketregime.Config, noiseMultiplier float64, catchUpSpeed float64, confirmBars int) (marketregime.Analyzer, error) {
	version := marketregime.Version(strings.ToLower(strings.TrimSpace(versionText)))
	switch version {
	case marketregime.VersionV1:
		return marketregime.NewDetector(v1Config)
	case marketregime.VersionV2:
		return marketregime.NewV2Analyzer(marketregime.DefaultV2Config())
	case marketregime.VersionV3:
		config := marketregime.DefaultV3Config()
		config.NoiseMultiplier = noiseMultiplier
		config.CatchUpSpeed = catchUpSpeed
		config.ConfirmBars = confirmBars
		return marketregime.NewV3Analyzer(config)
	default:
		return nil, fmt.Errorf("unsupported regime version %q", versionText)
	}
}

func parsePositiveList(name string, text string) ([]float64, error) {
	parts := strings.Split(text, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s contains invalid positive number %q", name, part)
		}
		values = append(values, value)
	}
	return values, nil
}

func parsePositiveIntList(name string, text string) ([]int, error) {
	parts := strings.Split(text, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s contains invalid positive integer %q", name, part)
		}
		values = append(values, value)
	}
	return values, nil
}
