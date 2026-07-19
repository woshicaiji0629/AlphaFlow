package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
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
	"alphaflow/go-service/pkg/marketmodel"
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
	regimeVersion := flag.String("regime-version", "v1", "market regime analyzer version: v1, v2, v3, v4, v5, or v6")
	regimeV3NoiseMultiplier := flag.Float64("regime-v3-noise-multiplier", 1.5, "v3 ATR noise threshold multiplier")
	regimeV3CatchUpSpeed := flag.Float64("regime-v3-catch-up-speed", 0.35, "v3 volatility filter catch-up speed")
	regimeV3ConfirmBars := flag.Int("regime-v3-confirm-bars", 2, "v3 state confirmation bars")
	regimeAppendVersionRunID := flag.Bool("regime-append-version-run-id", false, "append the regime version to run_id for side-by-side research")
	regimeRunIDTag := flag.String("regime-run-id-tag", "", "optional research-only suffix appended to run_id")
	researchSkipPersist := flag.Bool("research-skip-persist", false, "run research in memory without persisting detailed rows")
	singlePositionScan := flag.Bool("single-position-scan", false, "compare staged single-position protection parameters in one replay")
	supertrendVersionCompare := flag.Bool("supertrend-version-compare", false, "compare standard, adaptive, and AI Supertrend flips with identical single-position rules")
	supertrendTradeDiagnostics := flag.Bool("supertrend-trade-diagnostics", false, "log standard Supertrend v4 flip decisions and completed trades")
	swingReviewPath := flag.String("swing-review-json", "", "write ETH swing opportunity and AI Supertrend coverage review JSON")
	swingMinimumPoints := flag.Float64("swing-minimum-points", 30, "minimum absolute ETH price move included in the swing review")
	swingReversalPoints := flag.Float64("swing-reversal-points", 10, "absolute ETH price reversal used to confirm a swing pivot")
	stopReviewPath := flag.String("stop-review-json", "", "write initial-stop entry diagnostics and post-stop path review JSON")
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
	if err := run(ctx, *configPath, *fixedStops, *atrStops, *takeProfits, *horizon, platformConfig, impulseConfig, pullbackConfig, *eventCooldownBars, counterTrendConfig, *counterTrendEnabled, *validationObservationBars, chopConfig, regimeAnalyzer, *regimeAppendVersionRunID, *regimeRunIDTag, *researchSkipPersist, *singlePositionScan, *supertrendVersionCompare, *supertrendTradeDiagnostics, *swingReviewPath, *swingMinimumPoints, *swingReversalPoints, *stopReviewPath); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string, fixedText string, atrText string, takeProfitText string, horizon time.Duration, platformConfig signalresearch.PlatformConfig, impulseConfig signalresearch.ImpulseConfig, pullbackConfig signalresearch.PullbackConfig, eventCooldownBars int, counterTrendConfig signalresearch.CounterTrendConfig, counterTrendEnabled bool, validationBarsText string, chopConfig signalresearch.ChopConfig, regimeAnalyzer marketregime.Analyzer, appendVersionRunID bool, runIDTag string, skipPersist bool, scanSinglePosition bool, compareSupertrendVersions bool, logTradeDiagnostics bool, swingReviewPath string, swingMinimumPoints float64, swingReversalPoints float64, stopReviewPath string) error {
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
	supertrendComparisonReplays, err := buildSupertrendComparisonReplays(singlePositionConfig, pullbackConfig, compareSupertrendVersions)
	if err != nil {
		return err
	}
	breakoutComparison, err := buildBreakoutComparison(singlePositionConfig, compareSupertrendVersions)
	if err != nil {
		return err
	}
	platformDetector, err := signalresearch.NewPlatformDetector(platformConfig)
	if err != nil {
		return err
	}
	compressionBreakoutDetector, err := signalresearch.NewCompressionBreakoutDetector(signalresearch.DefaultCompressionBreakoutConfig())
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
	flipDiagnostics := make(map[string][]supertrendFlipDiagnostic, 3)
	swingBars := make([]marketmodel.Kline, 0, 15000)
	swingEvidence := make([]signalresearch.SwingEvidence, 0, 20000)
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
		if (swingReviewPath != "" || stopReviewPath != "") && snapshot.Current.OpenTime >= startTime.UnixMilli() && snapshot.Current.OpenTime < endTime.UnixMilli() {
			swingBars = append(swingBars, snapshot.Current)
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
			if err := item.advance(snapshot.Current); err != nil {
				return err
			}
		}
		if breakoutComparison != nil {
			if err := breakoutComparison.advance(snapshot.Current); err != nil {
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
		compressionBreakoutEvents, err := compressionBreakoutDetector.Update(snapshot, currentRegime)
		if err != nil {
			return err
		}
		if logTradeDiagnostics {
			for _, event := range compressionBreakoutEvents {
				log.Printf("compression breakout diagnostic time_ms=%d side=%s metadata=%s", snapshot.Current.CloseTime, event.Side, event.MetadataJSON)
			}
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
		if swingReviewPath != "" && snapshot.Current.OpenTime < endTime.UnixMilli() {
			if side, ok := supertrendFlipSide(snapshot.Window, "ai_supertrend_direction"); ok {
				swingEvidence = append(swingEvidence, signalresearch.SwingEvidence{TimeMS: snapshot.Current.CloseTime, Side: side, Source: "ai_trend"})
			}
			for _, event := range researchEvents {
				swingEvidence = append(swingEvidence, signalresearch.SwingEvidence{TimeMS: snapshot.Current.CloseTime, Side: event.Side, Source: event.Source})
			}
			for _, event := range compressionBreakoutEvents {
				swingEvidence = append(swingEvidence, signalresearch.SwingEvidence{TimeMS: snapshot.Current.CloseTime, Side: event.Side, Source: event.Source})
			}
		}
		for _, comparison := range supertrendComparisonReplays {
			if err := comparison.followthroughReplay.Advance(snapshot); err != nil {
				return err
			}
			events, err := comparison.pullbackDetector.Update(snapshot)
			if err != nil {
				return err
			}
			comparison.currentPullbackSide = pullbackEventSide(events)
		}
		eventGate.Advance()
		if snapshot.Current.OpenTime >= endTime.UnixMilli() {
			continue
		}
		if breakoutComparison != nil {
			platformSide := pullbackEventSide(platformEvents)
			compressionSide := pullbackEventSide(compressionBreakoutEvents)
			if platformSide != strategy.SignalSideHold {
				breakoutComparison.platformSignals++
				if _, err := breakoutComparison.platformReplay.TryEnter(snapshot, platformSide, researchEntryRegime(currentRegime, platformSide)); err != nil {
					return err
				}
			}
			if compressionSide != strategy.SignalSideHold {
				breakoutComparison.compressionSignals++
				if _, err := breakoutComparison.compressionReplay.TryEnter(snapshot, compressionSide, researchEntryRegime(currentRegime, compressionSide)); err != nil {
					return err
				}
			}
			flipSide, hasFlip := supertrendFlipSide(snapshot.Window, "supertrend_flip")
			combinedSide, conflict := combinedEntrySide(flipSide, hasFlip, compressionSide)
			if conflict {
				breakoutComparison.combinedReplay.SkipConflict()
			} else if combinedSide != strategy.SignalSideHold {
				regime := currentRegime
				if compressionSide != strategy.SignalSideHold {
					regime = researchEntryRegime(currentRegime, combinedSide)
				}
				breakoutComparison.combinedSignals++
				if _, err := breakoutComparison.combinedReplay.TryEnter(snapshot, combinedSide, regime); err != nil {
					return err
				}
			}
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
			flipSide, hasFlip := supertrendFlipSide(snapshot.Window, item.flipKey)
			if hasFlip {
				item.exhaustPending = nil
				item.flipSignals++
				if logTradeDiagnostics || swingReviewPath != "" {
					flipDiagnostics[item.name] = append(flipDiagnostics[item.name], buildFlipDiagnostic(snapshot, flipSide, currentRegime))
				}
				entered, err := item.flipReplay.TryEnter(snapshot, flipSide, currentRegime)
				if err != nil {
					return err
				}
				if entered {
					item.entryDiagnostics = append(item.entryDiagnostics, buildScenarioEntryDiagnostic("flip", snapshot, flipSide, currentRegime))
					entryKey := fmt.Sprintf("%s:%d", item.name, snapshot.Current.CloseTime)
					if err := item.followthroughReplay.AddSignal(entryKey, snapshot, flipSide, []string{item.flipKey}); err != nil {
						return err
					}
				}
				if volumeAllowsFlip(snapshot, flipSide, 1.0, true) {
					item.volumeLooseSignals++
					if _, err := item.volumeLooseReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				}
				if volumeAllowsFlip(snapshot, flipSide, 1.2, false) {
					item.volumeStrongSignals++
					if _, err := item.volumeStrongReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				}
				if !exhaustionBlocked(snapshot, flipSide, 30, 8) {
					item.exhaustLooseSignals++
					if _, err := item.exhaustLooseReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				}
				if !exhaustionBlocked(snapshot, flipSide, 35, 8) {
					item.exhaustStrictSignals++
					if _, err := item.exhaustStrictReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				}
				if regimeAllowsSide(currentRegime, flipSide) {
					if exhaustionBlocked(snapshot, flipSide, 35, 8) {
						pending, err := newConfirmationPending(snapshot, flipSide)
						if err != nil {
							return err
						}
						item.exhaustPending = &pending
					} else {
						item.exhaustDeferredSignals++
						if _, err := item.exhaustDeferredReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
							return err
						}
					}
				}
				if !macroMomentumBlocked(snapshot, flipSide) {
					item.macroVetoSignals++
					if _, err := item.macroVetoReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				}
				if regimeAllowsSide(currentRegime, flipSide) {
					pending, err := newConfirmationPending(snapshot, flipSide)
					if err != nil {
						return err
					}
					item.waitOnePending = &pending
					retestPending := pending
					item.retestPending = &retestPending
				} else {
					item.waitOnePending = nil
					item.retestPending = nil
				}
				if regimeAllowsSide(currentRegime, flipSide) {
					item.pendingFlip = nil
					item.deferredSignals++
					if _, err := item.deferredReplay.TryEnter(snapshot, flipSide, currentRegime); err != nil {
						return err
					}
				} else if compressionBlocked(currentRegime) {
					pending, err := newPendingFlip(snapshot, flipSide)
					if err != nil {
						return err
					}
					item.pendingFlip = &pending
				} else {
					item.pendingFlip = nil
				}
			}
			if !hasFlip {
				if item.exhaustPending != nil {
					allowed, expired, err := item.exhaustPending.exhaustReaccelerationAllows(snapshot)
					if err != nil {
						return err
					}
					if allowed && regimeAllowsSide(currentRegime, item.exhaustPending.side) {
						item.exhaustDeferredSignals++
						if _, err := item.exhaustDeferredReplay.TryEnter(snapshot, item.exhaustPending.side, currentRegime); err != nil {
							return err
						}
						item.exhaustPending = nil
					} else if expired {
						item.exhaustPending = nil
					}
				}
				if item.waitOnePending != nil {
					allowed, expired, err := item.waitOnePending.waitOneAllows(snapshot)
					if err != nil {
						return err
					}
					if allowed {
						item.waitOneSignals++
						if _, err := item.waitOneReplay.TryEnter(snapshot, item.waitOnePending.side, currentRegime); err != nil {
							return err
						}
					}
					if allowed || expired {
						item.waitOnePending = nil
					}
				}
				if item.retestPending != nil {
					allowed, expired, err := item.retestPending.retestAllows(snapshot)
					if err != nil {
						return err
					}
					if allowed {
						item.retestSignals++
						if _, err := item.retestReplay.TryEnter(snapshot, item.retestPending.side, currentRegime); err != nil {
							return err
						}
					}
					if allowed || expired {
						item.retestPending = nil
					}
				}
			}
			if item.pendingFlip != nil && !hasFlip {
				activated, err := item.tryDeferredEntry(snapshot, currentRegime)
				if err != nil {
					return err
				}
				if activated {
					item.deferredSignals++
				}
			}
			pullbackSide := item.currentPullbackSide
			if pullbackSide != strategy.SignalSideHold {
				item.pullbackSignals++
				entered, err := item.pullbackReplay.TryEnter(snapshot, pullbackSide, currentRegime)
				if err != nil {
					return err
				}
				if entered {
					item.entryDiagnostics = append(item.entryDiagnostics, buildScenarioEntryDiagnostic("pullback", snapshot, pullbackSide, currentRegime))
				}
			}
			combinedSide, conflict := combinedEntrySide(flipSide, hasFlip, pullbackSide)
			if conflict {
				item.combinedReplay.SkipConflict()
			} else if combinedSide != strategy.SignalSideHold {
				item.combinedSignals++
				if _, err := item.combinedReplay.TryEnter(snapshot, combinedSide, currentRegime); err != nil {
					return err
				}
			}
		}
	}
	replay.Finish()
	for _, item := range singlePositionReplays {
		item.replay.Finish()
	}
	for _, item := range supertrendComparisonReplays {
		item.finish()
	}
	if swingReviewPath != "" {
		if !compareSupertrendVersions {
			return fmt.Errorf("swing review requires -supertrend-version-compare")
		}
		var ai *supertrendComparisonReplay
		for _, item := range supertrendComparisonReplays {
			if item.name == "ai" {
				ai = item
				break
			}
		}
		if ai == nil {
			return fmt.Errorf("AI Supertrend comparison replay missing")
		}
		swingSignals := make([]signalresearch.SwingSignal, 0, len(flipDiagnostics["ai"]))
		for _, item := range flipDiagnostics["ai"] {
			swingSignals = append(swingSignals, signalresearch.SwingSignal{TimeMS: item.SignalTimeMS, Side: item.Side, Allowed: item.Allowed, Reason: item.Reason})
		}
		report, err := signalresearch.ReviewSwings(swingBars, swingSignals, swingEvidence, ai.flipReplay.Trades(), signalresearch.SwingReviewConfig{MinimumMovePoints: swingMinimumPoints, ReversalPoints: swingReversalPoints, LeadWindowMS: (45 * time.Minute).Milliseconds()})
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(swingReviewPath, encoded, 0o644); err != nil {
			return fmt.Errorf("write swing review: %w", err)
		}
		log.Printf("swing review written path=%s opportunities=%d early=%d middle=%d late=%d missed=%d traded=%d net_pnl=%.2f", swingReviewPath, len(report.Opportunities), report.EarlyHits, report.MiddleHits, report.LateHits, report.Missed, report.Traded, report.NetPnL)
	}
	if breakoutComparison != nil {
		breakoutComparison.finish()
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
		for _, mode := range item.summaries() {
			summary := mode.replay.Summary()
			summaryJSON, err := json.Marshal(summary)
			if err != nil {
				log.Printf("supertrend continuation comparison run_id=%s regime_version=%s supertrend_version=%s entry_mode=%s raw_signals=%d trades=%d net_pnl=%.2f profit_factor=%v", cfg.Runtime.RunID, regimeAnalyzer.Version(), item.name, mode.name, mode.rawSignals, summary.Trades, summary.NetPnL, summary.ProfitFactor)
				continue
			}
			log.Printf("supertrend continuation comparison run_id=%s regime_version=%s supertrend_version=%s entry_mode=%s raw_signals=%d summary=%s", cfg.Runtime.RunID, regimeAnalyzer.Version(), item.name, mode.name, mode.rawSignals, summaryJSON)
		}
		if logTradeDiagnostics {
			for _, diagnostic := range flipDiagnostics[item.name] {
				encoded, err := json.Marshal(diagnostic)
				if err != nil {
					return err
				}
				log.Printf("supertrend flip diagnostic version=%s detail=%s", item.name, encoded)
			}
			for _, mode := range []struct {
				name   string
				replay *signalresearch.SinglePositionReplay
			}{
				{name: "flip", replay: item.flipReplay},
				{name: "flip_volume_loose", replay: item.volumeLooseReplay},
				{name: "flip_volume_strong", replay: item.volumeStrongReplay},
				{name: "exhaust_adx30_di8", replay: item.exhaustLooseReplay},
				{name: "exhaust_adx35_di8", replay: item.exhaustStrictReplay},
				{name: "10m_15m_veto", replay: item.macroVetoReplay},
				{name: "wait_1_bar", replay: item.waitOneReplay},
				{name: "retest_3_bars", replay: item.retestReplay},
				{name: "exhaust_deferred_reacceleration", replay: item.exhaustDeferredReplay},
			} {
				for _, trade := range mode.replay.Trades() {
					encoded, err := json.Marshal(trade)
					if err != nil {
						return err
					}
					log.Printf("supertrend trade diagnostic version=%s entry_mode=%s detail=%s", item.name, mode.name, encoded)
				}
			}
			for _, trade := range item.deferredReplay.Trades() {
				encoded, err := json.Marshal(trade)
				if err != nil {
					return err
				}
				log.Printf("supertrend deferred trade diagnostic %s", encoded)
			}
			for _, observation := range item.followthroughReplay.Results() {
				encoded, err := json.Marshal(observation)
				if err != nil {
					return err
				}
				log.Printf("supertrend followthrough diagnostic version=%s detail=%s", item.name, encoded)
			}
		}
	}
	if stopReviewPath != "" {
		var sources []stopReviewSource
		for _, item := range supertrendComparisonReplays {
			if item.name == "ai" {
				sources = append(sources,
					stopReviewSource{mode: "flip", trades: item.flipReplay.Trades(), entries: item.entryDiagnostics},
					stopReviewSource{mode: "pullback", trades: item.pullbackReplay.Trades(), entries: item.entryDiagnostics})
			}
		}
		report, err := buildStopReview(swingBars, sources)
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(stopReviewPath, encoded, 0o644); err != nil {
			return fmt.Errorf("write stop review: %w", err)
		}
		log.Printf("stop review written path=%s initial_stops=%d reasons=%v", stopReviewPath, len(report.Trades), report.ReasonCounts)
	}
	if breakoutComparison != nil {
		for _, mode := range breakoutComparison.summaries() {
			encoded, err := json.Marshal(mode.replay.Summary())
			if err != nil {
				return err
			}
			log.Printf("compression breakout comparison run_id=%s regime_version=%s entry_mode=%s raw_signals=%d summary=%s", cfg.Runtime.RunID, regimeAnalyzer.Version(), mode.name, mode.rawSignals, encoded)
		}
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

type breakoutComparisonReplay struct {
	platformSignals    int
	compressionSignals int
	combinedSignals    int
	platformReplay     *signalresearch.SinglePositionReplay
	compressionReplay  *signalresearch.SinglePositionReplay
	combinedReplay     *signalresearch.SinglePositionReplay
}

func buildBreakoutComparison(config signalresearch.SinglePositionConfig, enabled bool) (*breakoutComparisonReplay, error) {
	if !enabled {
		return nil, nil
	}
	result := &breakoutComparisonReplay{}
	var err error
	result.platformReplay, err = buildComparisonReplay(config, "standard", "platform_breakout")
	if err != nil {
		return nil, err
	}
	result.compressionReplay, err = buildComparisonReplay(config, "standard", "compression_breakout")
	if err != nil {
		return nil, err
	}
	result.combinedReplay, err = buildComparisonReplay(config, "standard", "flip_compression_combined")
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *breakoutComparisonReplay) advance(bar marketmodel.Kline) error {
	for _, replay := range []*signalresearch.SinglePositionReplay{r.platformReplay, r.compressionReplay, r.combinedReplay} {
		if err := replay.Advance(bar); err != nil {
			return err
		}
	}
	return nil
}

func (r *breakoutComparisonReplay) finish() {
	r.platformReplay.Finish()
	r.compressionReplay.Finish()
	r.combinedReplay.Finish()
}

func (r *breakoutComparisonReplay) summaries() []comparisonSummary {
	return []comparisonSummary{
		{name: "platform", rawSignals: r.platformSignals, replay: r.platformReplay},
		{name: "compression", rawSignals: r.compressionSignals, replay: r.compressionReplay},
		{name: "flip_compression", rawSignals: r.combinedSignals, replay: r.combinedReplay},
	}
}

func researchEntryRegime(current *marketregime.Result, side strategy.SignalSide) *marketregime.Result {
	result := marketregime.Result{State: marketregime.StateTrendArmed, AllowNewPosition: true}
	if current != nil {
		result = *current
		result.State = marketregime.StateTrendArmed
		result.AllowNewPosition = true
	}
	result.Direction = marketregime.DirectionLong
	result.AllowLong, result.AllowShort = true, false
	if side == strategy.SignalSideSell {
		result.Direction = marketregime.DirectionShort
		result.AllowLong, result.AllowShort = false, true
	}
	return &result
}

type supertrendComparisonReplay struct {
	name                   string
	flipKey                string
	valueKey               string
	directionKey           string
	flipSignals            int
	volumeLooseSignals     int
	volumeStrongSignals    int
	pullbackSignals        int
	combinedSignals        int
	deferredSignals        int
	exhaustLooseSignals    int
	exhaustStrictSignals   int
	macroVetoSignals       int
	waitOneSignals         int
	retestSignals          int
	exhaustDeferredSignals int
	currentPullbackSide    strategy.SignalSide
	entryDiagnostics       []scenarioEntryDiagnostic
	pendingFlip            *pendingFlip
	pullbackDetector       *signalresearch.PullbackDetector
	flipReplay             *signalresearch.SinglePositionReplay
	volumeLooseReplay      *signalresearch.SinglePositionReplay
	volumeStrongReplay     *signalresearch.SinglePositionReplay
	pullbackReplay         *signalresearch.SinglePositionReplay
	combinedReplay         *signalresearch.SinglePositionReplay
	deferredReplay         *signalresearch.SinglePositionReplay
	exhaustLooseReplay     *signalresearch.SinglePositionReplay
	exhaustStrictReplay    *signalresearch.SinglePositionReplay
	macroVetoReplay        *signalresearch.SinglePositionReplay
	waitOneReplay          *signalresearch.SinglePositionReplay
	retestReplay           *signalresearch.SinglePositionReplay
	exhaustDeferredReplay  *signalresearch.SinglePositionReplay
	followthroughReplay    *signalresearch.ValidationReplay
	waitOnePending         *confirmationPending
	retestPending          *confirmationPending
	exhaustPending         *confirmationPending
}

func buildSupertrendComparisonReplays(base signalresearch.SinglePositionConfig, pullbackBase signalresearch.PullbackConfig, enabled bool) ([]*supertrendComparisonReplay, error) {
	if !enabled {
		return nil, nil
	}
	versions := []struct {
		name         string
		flipKey      string
		valueKey     string
		directionKey string
	}{
		{name: "standard", flipKey: "supertrend_flip", valueKey: "supertrend", directionKey: "supertrend_direction"},
		{name: "adaptive", flipKey: "adaptive_supertrend_flip", valueKey: "adaptive_supertrend", directionKey: "adaptive_supertrend_direction"},
		{name: "ai", flipKey: "ai_supertrend_flip", valueKey: "ai_supertrend", directionKey: "ai_supertrend_direction"},
	}
	result := make([]*supertrendComparisonReplay, 0, len(versions))
	for _, version := range versions {
		pullbackConfig := pullbackBase
		pullbackConfig.TrendValueKey = version.valueKey
		pullbackConfig.TrendDirectionKey = version.directionKey
		detector, err := signalresearch.NewPullbackDetector(pullbackConfig)
		if err != nil {
			return nil, fmt.Errorf("build %s Supertrend pullback detector: %w", version.name, err)
		}
		item := &supertrendComparisonReplay{
			name: version.name, flipKey: version.flipKey, valueKey: version.valueKey, directionKey: version.directionKey,
			pullbackDetector: detector,
		}
		item.flipReplay, err = buildComparisonReplay(base, version.name, "flip")
		if err != nil {
			return nil, err
		}
		item.volumeLooseReplay, err = buildComparisonReplay(base, version.name, "flip_volume_loose")
		if err != nil {
			return nil, err
		}
		item.volumeStrongReplay, err = buildComparisonReplay(base, version.name, "flip_volume_strong")
		if err != nil {
			return nil, err
		}
		item.pullbackReplay, err = buildComparisonReplay(base, version.name, "pullback")
		if err != nil {
			return nil, err
		}
		item.combinedReplay, err = buildComparisonReplay(base, version.name, "combined")
		if err != nil {
			return nil, err
		}
		item.deferredReplay, err = buildComparisonReplay(base, version.name, "deferred")
		if err != nil {
			return nil, err
		}
		item.exhaustLooseReplay, err = buildComparisonReplay(base, version.name, "exhaust_adx30_di8")
		if err != nil {
			return nil, err
		}
		item.exhaustStrictReplay, err = buildComparisonReplay(base, version.name, "exhaust_adx35_di8")
		if err != nil {
			return nil, err
		}
		item.macroVetoReplay, err = buildComparisonReplay(base, version.name, "10m_15m_veto")
		if err != nil {
			return nil, err
		}
		item.waitOneReplay, err = buildComparisonReplay(base, version.name, "wait_1_bar")
		if err != nil {
			return nil, err
		}
		item.retestReplay, err = buildComparisonReplay(base, version.name, "retest_3_bars")
		if err != nil {
			return nil, err
		}
		item.exhaustDeferredReplay, err = buildComparisonReplay(base, version.name, "exhaust_deferred_reacceleration")
		if err != nil {
			return nil, err
		}
		item.followthroughReplay, err = signalresearch.NewValidationReplay(signalresearch.ValidationConfig{ObservationBars: []int{1, 3, 5, 10}})
		if err != nil {
			return nil, fmt.Errorf("build %s Supertrend follow-through replay: %w", version.name, err)
		}
		result = append(result, item)
	}
	return result, nil
}

func buildComparisonReplay(config signalresearch.SinglePositionConfig, version string, mode string) (*signalresearch.SinglePositionReplay, error) {
	replay, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return nil, fmt.Errorf("build %s Supertrend %s comparison replay: %w", version, mode, err)
	}
	return replay, nil
}

func (r *supertrendComparisonReplay) advance(bar marketmodel.Kline) error {
	for _, replay := range []*signalresearch.SinglePositionReplay{r.flipReplay, r.volumeLooseReplay, r.volumeStrongReplay, r.pullbackReplay, r.combinedReplay, r.deferredReplay, r.exhaustLooseReplay, r.exhaustStrictReplay, r.macroVetoReplay, r.waitOneReplay, r.retestReplay, r.exhaustDeferredReplay} {
		if err := replay.Advance(bar); err != nil {
			return err
		}
	}
	if r.pendingFlip != nil {
		r.pendingFlip.age++
		if r.pendingFlip.age > 10 {
			r.pendingFlip = nil
		}
	}
	for _, pending := range []*confirmationPending{r.waitOnePending, r.retestPending, r.exhaustPending} {
		if pending != nil {
			pending.age++
		}
	}
	return nil
}

func (r *supertrendComparisonReplay) finish() {
	r.flipReplay.Finish()
	r.volumeLooseReplay.Finish()
	r.volumeStrongReplay.Finish()
	r.pullbackReplay.Finish()
	r.combinedReplay.Finish()
	r.deferredReplay.Finish()
	r.exhaustLooseReplay.Finish()
	r.exhaustStrictReplay.Finish()
	r.macroVetoReplay.Finish()
	r.waitOneReplay.Finish()
	r.retestReplay.Finish()
	r.exhaustDeferredReplay.Finish()
}

type comparisonSummary struct {
	name       string
	rawSignals int
	replay     *signalresearch.SinglePositionReplay
}

type pendingFlip struct {
	side        strategy.SignalSide
	signalPrice float64
	atr         float64
	age         int
}

type confirmationPending struct {
	side     strategy.SignalSide
	close    float64
	high     float64
	low      float64
	midpoint float64
	age      int
	retested bool
}

func newConfirmationPending(snapshot strategy.Snapshot, side strategy.SignalSide) (confirmationPending, error) {
	openPrice, err := strconv.ParseFloat(snapshot.Current.Open, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation open %q", snapshot.Current.Open)
	}
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation low %q", snapshot.Current.Low)
	}
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation close %q", snapshot.Current.Close)
	}
	return confirmationPending{side: side, close: closePrice, high: high, low: low, midpoint: (openPrice + closePrice) / 2}, nil
}

func (p *confirmationPending) waitOneAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse wait confirmation close %q", snapshot.Current.Close)
	}
	directionalPrice := p.side == strategy.SignalSideBuy && closePrice > p.midpoint || p.side == strategy.SignalSideSell && closePrice < p.midpoint
	macdHistogram, histogramOK := snapshot.Indicator.Float("macd_hist")
	macdDelta, deltaOK := snapshot.Indicator.Float("macd_hist_delta")
	squeeze, squeezeOK := snapshot.Indicator.Float("squeeze_momentum")
	squeezeDelta, squeezeDeltaOK := snapshot.Indicator.Float("squeeze_momentum_delta")
	direction := 1.0
	if p.side == strategy.SignalSideSell {
		direction = -1
	}
	macdOK := (histogramOK && direction*macdHistogram >= 0) || (deltaOK && direction*macdDelta >= 0)
	squeezeOK = (squeezeOK && direction*squeeze > 0) || (squeezeDeltaOK && direction*squeezeDelta > 0)
	return p.age == 1 && directionalPrice && macdOK && squeezeOK, p.age >= 1, nil
}

func (p *confirmationPending) retestAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest low %q", snapshot.Current.Low)
	}
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest close %q", snapshot.Current.Close)
	}
	invalid := p.side == strategy.SignalSideBuy && low < p.low || p.side == strategy.SignalSideSell && high > p.high
	if invalid {
		return false, true, nil
	}
	if p.side == strategy.SignalSideBuy && low <= p.midpoint || p.side == strategy.SignalSideSell && high >= p.midpoint {
		p.retested = true
	}
	recovered := p.retested && (p.side == strategy.SignalSideBuy && closePrice > p.close || p.side == strategy.SignalSideSell && closePrice < p.close)
	return recovered, p.age >= 3, nil
}

func (p *confirmationPending) exhaustReaccelerationAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse exhaustion confirmation close %q", snapshot.Current.Close)
	}
	structureEvent := strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["structure_event"]))
	if structureEvent == "" {
		if series, ok := snapshot.Window.Signal("structure_event"); ok {
			structureEvent = strings.ToLower(strings.TrimSpace(series.Latest))
		}
	}
	invalid := p.side == strategy.SignalSideBuy && (closePrice < p.low || structureEvent == "bos_down" || structureEvent == "choch_down") ||
		p.side == strategy.SignalSideSell && (closePrice > p.high || structureEvent == "bos_up" || structureEvent == "choch_up")
	if invalid {
		return false, true, nil
	}
	direction := 1.0
	if p.side == strategy.SignalSideSell {
		direction = -1
	}
	macdReaccelerated := false
	if timeframe, ok := snapshot.Timeframes["15m"]; ok {
		if delta, deltaOK := timeframe.Indicator.Float("macd_hist_delta"); deltaOK {
			macdReaccelerated = direction*delta > 0
		}
	}
	breakout := p.side == strategy.SignalSideBuy && closePrice > p.high || p.side == strategy.SignalSideSell && closePrice < p.low
	squeeze, squeezeOK := snapshot.Indicator.Float("squeeze_momentum")
	squeezeDelta, squeezeDeltaOK := snapshot.Indicator.Float("squeeze_momentum_delta")
	squeezeAligned := squeezeOK && squeezeDeltaOK && direction*squeeze > 0 && direction*squeezeDelta > 0
	return macdReaccelerated || breakout && squeezeAligned, p.age >= 10, nil
}

func exhaustionBlocked(snapshot strategy.Snapshot, side strategy.SignalSide, minADX float64, minDIDifference float64) bool {
	timeframe, ok := snapshot.Timeframes["15m"]
	if !ok {
		return false
	}
	adx, adxOK := timeframe.Indicator.Float("adx14")
	diPlus, plusOK := timeframe.Indicator.Float("di_plus14")
	diMinus, minusOK := timeframe.Indicator.Float("di_minus14")
	delta, deltaOK := timeframe.Indicator.Float("macd_hist_delta")
	if !adxOK || !plusOK || !minusOK || !deltaOK {
		return false
	}
	direction := 1.0
	if side == strategy.SignalSideSell {
		direction = -1
	}
	return adx >= minADX && direction*(diPlus-diMinus) >= minDIDifference && direction*delta <= 0
}

func macroMomentumBlocked(snapshot strategy.Snapshot, side strategy.SignalSide) bool {
	_, tenOK := snapshot.Timeframes["10m"]
	fifteenMinute, fifteenOK := snapshot.Timeframes["15m"]
	if !tenOK || !fifteenOK {
		return false
	}
	direction := timeframeSignal(snapshot, "10m", "supertrend_direction")
	opposite := side == strategy.SignalSideBuy && direction == "down" || side == strategy.SignalSideSell && direction == "up"
	delta, ok := fifteenMinute.Indicator.Float("macd_hist_delta")
	if !ok {
		return false
	}
	if side == strategy.SignalSideSell {
		delta = -delta
	}
	return opposite && delta <= 0
}

func newPendingFlip(snapshot strategy.Snapshot, side strategy.SignalSide) (pendingFlip, error) {
	price, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || price <= 0 {
		return pendingFlip{}, fmt.Errorf("parse pending flip price %q", snapshot.Current.Close)
	}
	atr, ok := snapshot.Indicator.Float("atr14")
	if !ok || atr <= 0 {
		return pendingFlip{}, fmt.Errorf("pending flip requires positive atr14")
	}
	return pendingFlip{side: side, signalPrice: price, atr: atr}, nil
}

func (r *supertrendComparisonReplay) tryDeferredEntry(snapshot strategy.Snapshot, regime *marketregime.Result) (bool, error) {
	pending := r.pendingFlip
	if pending == nil || !regimeAllowsSide(regime, pending.side) {
		return false, nil
	}
	r.pendingFlip = nil
	price, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || price <= 0 {
		return false, fmt.Errorf("parse deferred entry price %q", snapshot.Current.Close)
	}
	continued := pending.side == strategy.SignalSideBuy && price >= pending.signalPrice || pending.side == strategy.SignalSideSell && price <= pending.signalPrice
	if !continued || math.Abs(price-pending.signalPrice) > pending.atr {
		return false, nil
	}
	entered, err := r.deferredReplay.TryEnter(snapshot, pending.side, regime)
	return entered, err
}

func regimeAllowsSide(regime *marketregime.Result, side strategy.SignalSide) bool {
	return regime != nil && (side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort)
}

func compressionBlocked(regime *marketregime.Result) bool {
	if regime == nil {
		return false
	}
	if regime.State == marketregime.StateChopLock {
		return true
	}
	for _, reason := range regime.Reasons {
		if strings.Contains(reason, "compression") || strings.Contains(reason, "breakout_width") {
			return true
		}
	}
	return false
}

type supertrendFlipDiagnostic struct {
	SignalTimeMS          int64                                `json:"signal_time_ms"`
	Side                  strategy.SignalSide                  `json:"side"`
	Close                 string                               `json:"close"`
	RegimeState           marketregime.State                   `json:"regime_state,omitempty"`
	Direction             marketregime.Direction               `json:"direction,omitempty"`
	Allowed               bool                                 `json:"allowed"`
	Reason                string                               `json:"reason"`
	VolumeRatio20         float64                              `json:"volume_ratio20"`
	VolumeRatioReady      bool                                 `json:"volume_ratio20_ready"`
	PriceVolume           string                               `json:"price_volume_confirmation,omitempty"`
	VolumeLooseAllowed    bool                                 `json:"volume_loose_allowed"`
	VolumeStrongAllowed   bool                                 `json:"volume_strong_allowed"`
	ATR14                 float64                              `json:"atr14"`
	BodyATR               float64                              `json:"body_atr"`
	UpperWickATR          float64                              `json:"upper_wick_atr"`
	LowerWickATR          float64                              `json:"lower_wick_atr"`
	EMA25DistancePct      float64                              `json:"ema25_distance_pct"`
	EMA99DistancePct      float64                              `json:"ema99_distance_pct"`
	SupertrendDistancePct float64                              `json:"supertrend_distance_pct"`
	MACDHistogram         float64                              `json:"macd_hist"`
	MACDHistogramDelta    float64                              `json:"macd_hist_delta"`
	MACDMomentum          string                               `json:"macd_momentum,omitempty"`
	MACDDivergence        string                               `json:"macd_divergence,omitempty"`
	StructureEvent        string                               `json:"structure_event,omitempty"`
	StructureBias         string                               `json:"structure_bias,omitempty"`
	Direction5M           string                               `json:"direction_5m,omitempty"`
	Direction10M          string                               `json:"direction_10m,omitempty"`
	Direction15M          string                               `json:"direction_15m,omitempty"`
	HigherTimeframes      map[string]higherTimeframeDiagnostic `json:"higher_timeframes,omitempty"`
}

type scenarioEntryDiagnostic struct {
	Mode                  string              `json:"mode"`
	TimeMS                int64               `json:"time_ms"`
	Side                  strategy.SignalSide `json:"side"`
	Price                 float64             `json:"price"`
	RegimeState           marketregime.State  `json:"regime_state,omitempty"`
	RegimeReason          string              `json:"regime_reason,omitempty"`
	ATR14                 float64             `json:"atr14"`
	EMA25DistancePct      float64             `json:"ema25_distance_pct"`
	SupertrendDistancePct float64             `json:"supertrend_distance_pct"`
	VolumeRatio20         float64             `json:"volume_ratio20"`
	MACDHistogram         float64             `json:"macd_hist"`
	MACDHistogramDelta    float64             `json:"macd_hist_delta"`
	MACDMomentum          string              `json:"macd_momentum,omitempty"`
	StructureEvent        string              `json:"structure_event,omitempty"`
	StructureBias         string              `json:"structure_bias,omitempty"`
	Direction5M           string              `json:"direction_5m,omitempty"`
	Direction10M          string              `json:"direction_10m,omitempty"`
	Direction15M          string              `json:"direction_15m,omitempty"`
	Direction30M          string              `json:"direction_30m,omitempty"`
}

type stopReviewSource struct {
	mode    string
	trades  []signalresearch.SinglePositionTrade
	entries []scenarioEntryDiagnostic
}

type stopReviewTrade struct {
	Mode                 string                             `json:"mode"`
	Trade                signalresearch.SinglePositionTrade `json:"trade"`
	Entry                *scenarioEntryDiagnostic           `json:"entry,omitempty"`
	PostStopFavorablePts float64                            `json:"post_stop_favorable_points"`
	PostStopAdversePts   float64                            `json:"post_stop_adverse_points"`
	PostStopBars         int                                `json:"post_stop_bars"`
	Reason               string                             `json:"reason"`
}

type stopReviewReport struct {
	ForwardBars   int               `json:"forward_bars"`
	Trades        []stopReviewTrade `json:"trades"`
	WinningTrades []stopReviewTrade `json:"winning_trades"`
	ModeCounts    map[string]int    `json:"mode_counts"`
	ReasonCounts  map[string]int    `json:"reason_counts"`
}

func buildScenarioEntryDiagnostic(mode string, snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) scenarioEntryDiagnostic {
	price, _ := strconv.ParseFloat(snapshot.Current.Close, 64)
	result := scenarioEntryDiagnostic{Mode: mode, TimeMS: snapshot.Current.CloseTime, Side: side, Price: price}
	result.ATR14, _ = snapshot.Indicator.Float("atr14")
	result.EMA25DistancePct, _ = snapshot.Indicator.Float("price_ema25_distance_pct")
	result.SupertrendDistancePct, _ = snapshot.Indicator.Float("supertrend_distance_pct")
	result.VolumeRatio20, _ = snapshot.Indicator.Float("volume_ratio20")
	result.MACDHistogram, _ = snapshot.Indicator.Float("macd_hist")
	result.MACDHistogramDelta, _ = snapshot.Indicator.Float("macd_hist_delta")
	result.MACDMomentum = snapshot.Indicator.Signals["macd_momentum"]
	result.StructureEvent = snapshot.Indicator.Signals["structure_event"]
	result.StructureBias = snapshot.Indicator.Signals["structure_bias"]
	result.Direction5M = timeframeSignal(snapshot, "5m", "ai_supertrend_direction")
	result.Direction10M = timeframeSignal(snapshot, "10m", "ai_supertrend_direction")
	result.Direction15M = timeframeSignal(snapshot, "15m", "ai_supertrend_direction")
	result.Direction30M = timeframeSignal(snapshot, "30m", "ai_supertrend_direction")
	if regime != nil {
		result.RegimeState = regime.State
		result.RegimeReason = regimeDecisionReason(side, *regime)
	}
	return result
}

func buildStopReview(bars []marketmodel.Kline, sources []stopReviewSource) (stopReviewReport, error) {
	const forwardBars = 20
	report := stopReviewReport{ForwardBars: forwardBars, ModeCounts: map[string]int{}, ReasonCounts: map[string]int{}}
	for _, source := range sources {
		for _, trade := range source.trades {
			var entry *scenarioEntryDiagnostic
			for index := range source.entries {
				candidate := source.entries[index]
				if candidate.Mode == source.mode && candidate.TimeMS == trade.EntryTimeMS {
					copy := candidate
					entry = &copy
					break
				}
			}
			if trade.NetPnL > 0 {
				report.WinningTrades = append(report.WinningTrades, stopReviewTrade{Mode: source.mode, Trade: trade, Entry: entry, Reason: "profitable"})
			}
			if trade.ExitReason != "initial_stop" {
				continue
			}
			item := stopReviewTrade{Mode: source.mode, Trade: trade, Entry: entry, PostStopBars: forwardBars}
			seen := 0
			for _, bar := range bars {
				if bar.CloseTime <= trade.ExitTimeMS || seen >= forwardBars {
					continue
				}
				high, err := strconv.ParseFloat(bar.High, 64)
				if err != nil {
					return stopReviewReport{}, err
				}
				low, err := strconv.ParseFloat(bar.Low, 64)
				if err != nil {
					return stopReviewReport{}, err
				}
				if trade.Side == strategy.SignalSideBuy {
					item.PostStopFavorablePts = math.Max(item.PostStopFavorablePts, high-trade.EntryPrice)
					item.PostStopAdversePts = math.Max(item.PostStopAdversePts, trade.EntryPrice-low)
				} else {
					item.PostStopFavorablePts = math.Max(item.PostStopFavorablePts, trade.EntryPrice-low)
					item.PostStopAdversePts = math.Max(item.PostStopAdversePts, high-trade.EntryPrice)
				}
				seen++
			}
			item.PostStopBars = seen
			switch {
			case item.PostStopFavorablePts >= 30:
				item.Reason = "stop_too_tight_or_entry_timing"
			case item.PostStopAdversePts >= 30:
				item.Reason = "wrong_direction"
			case source.mode == "pullback":
				item.Reason = "failed_pullback_resume"
			case source.mode == "breakout":
				item.Reason = "false_breakout"
			case source.mode == "impulse":
				item.Reason = "failed_impulse_followthrough"
			default:
				item.Reason = "unclassified_no_followthrough"
			}
			report.Trades = append(report.Trades, item)
			report.ModeCounts[item.Mode]++
			report.ReasonCounts[item.Reason]++
		}
	}
	return report, nil
}

type higherTimeframeDiagnostic struct {
	Available             bool     `json:"available"`
	Supertrend            *float64 `json:"supertrend,omitempty"`
	SupertrendDistancePct *float64 `json:"supertrend_distance_pct,omitempty"`
	SupertrendDirection   string   `json:"supertrend_direction,omitempty"`
	SupertrendFlip        string   `json:"supertrend_flip,omitempty"`
	EMA7                  *float64 `json:"ema7,omitempty"`
	EMA25                 *float64 `json:"ema25,omitempty"`
	EMA99                 *float64 `json:"ema99,omitempty"`
	EMA25DistancePct      *float64 `json:"ema25_distance_pct,omitempty"`
	EMA99DistancePct      *float64 `json:"ema99_distance_pct,omitempty"`
	EMA25SlopePct         *float64 `json:"ema25_slope_pct,omitempty"`
	MAGroupSpreadPct      *float64 `json:"ma_group_spread_pct,omitempty"`
	MAArrangement         string   `json:"ma_arrangement,omitempty"`
	MAState               string   `json:"ma_state,omitempty"`
	MACD                  *float64 `json:"macd,omitempty"`
	MACDSignal            *float64 `json:"macd_signal,omitempty"`
	MACDHistogram         *float64 `json:"macd_hist,omitempty"`
	MACDHistogramDelta    *float64 `json:"macd_hist_delta,omitempty"`
	MACDMomentum          string   `json:"macd_momentum,omitempty"`
	MACDHistogramPhase    string   `json:"macd_hist_phase,omitempty"`
	MACDDivergence        string   `json:"macd_divergence,omitempty"`
	ADX                   *float64 `json:"adx14,omitempty"`
	DIPlus                *float64 `json:"di_plus14,omitempty"`
	DIMinus               *float64 `json:"di_minus14,omitempty"`
	KAMA                  *float64 `json:"kama10,omitempty"`
	KAMASlopeATR          *float64 `json:"kama_slope_atr,omitempty"`
	MoneyFlow             string   `json:"money_flow,omitempty"`
	RSI                   *float64 `json:"rsi14,omitempty"`
	BollingerWidthPct     *float64 `json:"bb_width_pct,omitempty"`
	BollingerWidthDelta   *float64 `json:"bb_width_delta,omitempty"`
	BollingerWidthState   string   `json:"bb_width_state,omitempty"`
	BollingerPosition     string   `json:"bb_position,omitempty"`
	SqueezeState          string   `json:"squeeze_state,omitempty"`
	SqueezeMomentum       *float64 `json:"squeeze_momentum,omitempty"`
	SqueezeMomentumDelta  *float64 `json:"squeeze_momentum_delta,omitempty"`
	MomentumState         string   `json:"momentum_state,omitempty"`
	VolumeRatio20         *float64 `json:"volume_ratio20,omitempty"`
	PriceVolume           string   `json:"price_volume_confirmation,omitempty"`
	StructureEvent        string   `json:"structure_event,omitempty"`
	StructureBias         string   `json:"structure_bias,omitempty"`
}

func buildFlipDiagnostic(snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) supertrendFlipDiagnostic {
	diagnostic := supertrendFlipDiagnostic{
		SignalTimeMS: snapshot.Current.CloseTime, Side: side, Close: snapshot.Current.Close,
		Reason: "no_regime", PriceVolume: strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["price_volume_confirmation"])),
	}
	diagnostic.VolumeRatio20, diagnostic.VolumeRatioReady = snapshot.Indicator.Float("volume_ratio20")
	diagnostic.VolumeLooseAllowed = volumeAllowsFlip(snapshot, side, 1.0, true)
	diagnostic.VolumeStrongAllowed = volumeAllowsFlip(snapshot, side, 1.2, false)
	diagnostic.ATR14, _ = snapshot.Indicator.Float("atr14")
	diagnostic.EMA25DistancePct, _ = snapshot.Indicator.Float("price_ema25_distance_pct")
	diagnostic.EMA99DistancePct, _ = snapshot.Indicator.Float("price_ema99_distance_pct")
	diagnostic.SupertrendDistancePct, _ = snapshot.Indicator.Float("supertrend_distance_pct")
	diagnostic.MACDHistogram, _ = snapshot.Indicator.Float("macd_hist")
	diagnostic.MACDHistogramDelta, _ = snapshot.Indicator.Float("macd_hist_delta")
	diagnostic.MACDMomentum = snapshot.Indicator.Signals["macd_momentum"]
	diagnostic.MACDDivergence = snapshot.Indicator.Signals["macd_divergence"]
	diagnostic.StructureEvent = snapshot.Indicator.Signals["structure_event"]
	diagnostic.StructureBias = snapshot.Indicator.Signals["structure_bias"]
	diagnostic.Direction5M = timeframeSignal(snapshot, "5m", "supertrend_direction")
	diagnostic.Direction10M = timeframeSignal(snapshot, "10m", "supertrend_direction")
	diagnostic.Direction15M = timeframeSignal(snapshot, "15m", "supertrend_direction")
	diagnostic.HigherTimeframes = make(map[string]higherTimeframeDiagnostic, 4)
	for _, interval := range []string{"5m", "10m", "15m", "30m"} {
		diagnostic.HigherTimeframes[interval] = buildHigherTimeframeDiagnostic(snapshot, interval)
	}
	if diagnostic.ATR14 > 0 {
		openPrice, openErr := strconv.ParseFloat(snapshot.Current.Open, 64)
		highPrice, highErr := strconv.ParseFloat(snapshot.Current.High, 64)
		lowPrice, lowErr := strconv.ParseFloat(snapshot.Current.Low, 64)
		closePrice, closeErr := strconv.ParseFloat(snapshot.Current.Close, 64)
		if openErr == nil && highErr == nil && lowErr == nil && closeErr == nil {
			diagnostic.BodyATR = math.Abs(closePrice-openPrice) / diagnostic.ATR14
			diagnostic.UpperWickATR = (highPrice - math.Max(openPrice, closePrice)) / diagnostic.ATR14
			diagnostic.LowerWickATR = (math.Min(openPrice, closePrice) - lowPrice) / diagnostic.ATR14
		}
	}
	if regime == nil {
		return diagnostic
	}
	diagnostic.RegimeState = regime.State
	diagnostic.Direction = regime.Direction
	diagnostic.Allowed = side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort
	diagnostic.Reason = regimeDecisionReason(side, *regime)
	return diagnostic
}

func buildHigherTimeframeDiagnostic(snapshot strategy.Snapshot, interval string) higherTimeframeDiagnostic {
	timeframe, ok := snapshot.Timeframes[interval]
	if !ok {
		return higherTimeframeDiagnostic{}
	}
	view := timeframe.Indicator
	result := higherTimeframeDiagnostic{
		Available:  true,
		Supertrend: numericDiagnostic(view, "supertrend"), SupertrendDistancePct: numericDiagnostic(view, "supertrend_distance_pct"),
		SupertrendDirection: timeframeSignal(snapshot, interval, "supertrend_direction"), SupertrendFlip: indicatorSignalDiagnostic(timeframe, "supertrend_flip"),
		EMA7: numericDiagnostic(view, "ema7"), EMA25: numericDiagnostic(view, "ema25"), EMA99: numericDiagnostic(view, "ema99"),
		EMA25DistancePct: numericDiagnostic(view, "price_ema25_distance_pct"), EMA99DistancePct: numericDiagnostic(view, "price_ema99_distance_pct"),
		EMA25SlopePct: numericDiagnostic(view, "ema25_slope5_pct"), MAGroupSpreadPct: numericDiagnostic(view, "ma_group_spread_pct"),
		MAArrangement: indicatorSignalDiagnostic(timeframe, "ma_arrangement"), MAState: indicatorSignalDiagnostic(timeframe, "ma_state"),
		MACD: numericDiagnostic(view, "macd"), MACDSignal: numericDiagnostic(view, "macd_signal"),
		MACDHistogram: numericDiagnostic(view, "macd_hist"), MACDHistogramDelta: numericDiagnostic(view, "macd_hist_delta"),
		MACDMomentum: indicatorSignalDiagnostic(timeframe, "macd_momentum"), MACDHistogramPhase: indicatorSignalDiagnostic(timeframe, "macd_hist_phase"),
		MACDDivergence: indicatorSignalDiagnostic(timeframe, "macd_divergence"), ADX: numericDiagnostic(view, "adx14"),
		DIPlus: numericDiagnostic(view, "di_plus14"), DIMinus: numericDiagnostic(view, "di_minus14"), KAMA: numericDiagnostic(view, "kama10"),
		MoneyFlow: indicatorSignalDiagnostic(timeframe, "money_flow_window_bias"), RSI: numericDiagnostic(view, "rsi14"),
		BollingerWidthPct: numericDiagnostic(view, "bb_width_pct"), BollingerWidthDelta: numericDiagnostic(view, "bb_width_delta"),
		BollingerWidthState: indicatorSignalDiagnostic(timeframe, "bb_width_state"), BollingerPosition: indicatorSignalDiagnostic(timeframe, "bb_position"),
		SqueezeState: indicatorSignalDiagnostic(timeframe, "squeeze_state"), SqueezeMomentum: numericDiagnostic(view, "squeeze_momentum"),
		SqueezeMomentumDelta: numericDiagnostic(view, "squeeze_momentum_delta"), MomentumState: indicatorSignalDiagnostic(timeframe, "momentum_state"),
		VolumeRatio20: numericDiagnostic(view, "volume_ratio20"), PriceVolume: indicatorSignalDiagnostic(timeframe, "price_volume_confirmation"),
		StructureEvent: indicatorSignalDiagnostic(timeframe, "structure_event"), StructureBias: indicatorSignalDiagnostic(timeframe, "structure_bias"),
	}
	atr, atrOK := view.Float("atr14")
	if kamaSeries, seriesOK := timeframe.Window.Numeric("kama10"); atrOK && atr > 0 && seriesOK {
		value := (kamaSeries.Latest - kamaSeries.Previous) / atr
		result.KAMASlopeATR = &value
	}
	return result
}

func numericDiagnostic(view strategy.IndicatorView, key string) *float64 {
	value, ok := view.Float(key)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func indicatorSignalDiagnostic(timeframe strategy.TimeframeSnapshot, key string) string {
	if value := strings.TrimSpace(timeframe.Indicator.Signals[key]); value != "" {
		return value
	}
	if series, ok := timeframe.Window.Signal(key); ok {
		return strings.TrimSpace(series.Latest)
	}
	return ""
}

func timeframeSignal(snapshot strategy.Snapshot, interval string, key string) string {
	timeframe, ok := snapshot.Timeframes[interval]
	if !ok {
		return ""
	}
	if value := strings.TrimSpace(timeframe.Indicator.Signals[key]); value != "" {
		return value
	}
	if series, ok := timeframe.Window.Signal(key); ok {
		return strings.TrimSpace(series.Latest)
	}
	return ""
}

func regimeDecisionReason(side strategy.SignalSide, regime marketregime.Result) string {
	if side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort {
		return "permitted"
	}
	if side == strategy.SignalSideBuy && regime.AllowShort || side == strategy.SignalSideSell && regime.AllowLong {
		return "v4_countertrend_signal"
	}
	for index := len(regime.Reasons) - 1; index >= 0; index-- {
		if (strings.HasPrefix(regime.Reasons[index], "v4_") || strings.HasPrefix(regime.Reasons[index], "v5_") || strings.HasPrefix(regime.Reasons[index], "v6_")) &&
			regime.Reasons[index] != "v4_permitted" && regime.Reasons[index] != "v4_release_confirmed" &&
			regime.Reasons[index] != "v5_permitted" && regime.Reasons[index] != "v5_fast_release_confirmed" &&
			regime.Reasons[index] != "v6_permitted" && regime.Reasons[index] != "v6_fast_release_confirmed" {
			return regime.Reasons[index]
		}
	}
	return "regime_blocked"
}

func (r *supertrendComparisonReplay) summaries() []comparisonSummary {
	result := []comparisonSummary{
		{name: "flip", rawSignals: r.flipSignals, replay: r.flipReplay},
		{name: "flip_volume_loose", rawSignals: r.volumeLooseSignals, replay: r.volumeLooseReplay},
		{name: "flip_volume_strong", rawSignals: r.volumeStrongSignals, replay: r.volumeStrongReplay},
		{name: "pullback", rawSignals: r.pullbackSignals, replay: r.pullbackReplay},
		{name: "combined", rawSignals: r.combinedSignals, replay: r.combinedReplay},
		{name: "deferred", rawSignals: r.deferredSignals, replay: r.deferredReplay},
		{name: "exhaust_adx30_di8", rawSignals: r.exhaustLooseSignals, replay: r.exhaustLooseReplay},
		{name: "exhaust_adx35_di8", rawSignals: r.exhaustStrictSignals, replay: r.exhaustStrictReplay},
		{name: "10m_15m_veto", rawSignals: r.macroVetoSignals, replay: r.macroVetoReplay},
		{name: "wait_1_bar", rawSignals: r.waitOneSignals, replay: r.waitOneReplay},
		{name: "retest_3_bars", rawSignals: r.retestSignals, replay: r.retestReplay},
		{name: "exhaust_deferred_reacceleration", rawSignals: r.exhaustDeferredSignals, replay: r.exhaustDeferredReplay},
	}
	return result
}

func volumeAllowsFlip(snapshot strategy.Snapshot, side strategy.SignalSide, minRatio float64, allowDirectionalConfirmation bool) bool {
	confirmation := strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["price_volume_confirmation"]))
	if confirmation == "" {
		if series, ok := snapshot.Window.Signal("price_volume_confirmation"); ok {
			confirmation = strings.ToLower(strings.TrimSpace(series.Latest))
		}
	}
	if side == strategy.SignalSideBuy && confirmation == "divergence_bear" ||
		side == strategy.SignalSideSell && confirmation == "divergence_bull" {
		return false
	}
	if ratio, ok := snapshot.Indicator.Float("volume_ratio20"); ok && ratio >= minRatio {
		return true
	}
	if !allowDirectionalConfirmation {
		return false
	}
	expected := "confirm_up"
	if side == strategy.SignalSideSell {
		expected = "confirm_down"
	}
	return confirmation == expected
}

func pullbackEventSide(events []signalresearch.PlatformEvent) strategy.SignalSide {
	if len(events) != 1 {
		return strategy.SignalSideHold
	}
	return events[0].Side
}

func combinedEntrySide(flipSide strategy.SignalSide, hasFlip bool, pullbackSide strategy.SignalSide) (strategy.SignalSide, bool) {
	if pullbackSide == "" {
		pullbackSide = strategy.SignalSideHold
	}
	if !hasFlip {
		return pullbackSide, false
	}
	if pullbackSide == strategy.SignalSideHold || pullbackSide == flipSide {
		return flipSide, false
	}
	return strategy.SignalSideHold, true
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
	case marketregime.VersionV4:
		return marketregime.NewV4Analyzer(marketregime.DefaultV4Config())
	case marketregime.VersionV5:
		return marketregime.NewV5Analyzer(marketregime.DefaultV5Config())
	case marketregime.VersionV6:
		return marketregime.NewV6Analyzer(marketregime.DefaultV6Config())
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
