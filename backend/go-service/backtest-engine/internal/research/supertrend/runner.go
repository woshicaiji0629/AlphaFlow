package supertrend

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/research/supertrend/experiments"
	supertrendexperiment "alphaflow/go-service/backtest-engine/internal/research/supertrend/experiments/supertrend"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
)

func run(ctx context.Context, options commandOptions) error {
	configPath, fixedText, atrText, takeProfitText := options.configPath, options.fixedStops, options.atrStops, options.takeProfits
	horizon, platformConfig, impulseConfig, pullbackConfig := options.horizon, options.platform, options.impulse, options.pullback
	eventCooldownBars, counterTrendConfig, counterTrendEnabled := options.eventCooldownBars, options.counterTrend, options.counterTrendEnabled
	validationBarsText, chopConfig, regimeAnalyzer := options.validationBars, options.chop, options.regimeAnalyzer
	appendVersionRunID, runIDTag, skipPersist := options.appendVersionRunID, options.runIDTag, options.skipPersist
	scanSinglePosition, compareSupertrendVersions := options.scanSinglePosition, options.compareSupertrendVersions
	logTradeDiagnostics, swingReviewPath := options.logTradeDiagnostics, options.swingReviewPath
	swingMinimumPoints, swingReversalPoints, stopReviewPath := options.swingMinimumPoints, options.swingReversalPoints, options.stopReviewPath
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load research config: %w", err)
	}
	if err := setupResearchLogger(cfg); err != nil {
		return err
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
	singlePositionExperiment, err := experiments.NewSinglePositionExperiment(singlePositionConfig, scanSinglePosition)
	if err != nil {
		return err
	}
	experimentItems := []experiments.Experiment{singlePositionExperiment}
	if compareSupertrendVersions {
		breakoutExperiment, err := experiments.NewBreakoutExperiment(singlePositionConfig)
		if err != nil {
			return err
		}
		experimentItems = append(experimentItems, breakoutExperiment)
		var swingReview *signalresearch.SwingReviewConfig
		if swingReviewPath != "" {
			swingReview = &signalresearch.SwingReviewConfig{
				MinimumMovePoints: swingMinimumPoints, ReversalPoints: swingReversalPoints,
				LeadWindowMS: (45 * time.Minute).Milliseconds(),
			}
		}
		supertrendExperiment, err := supertrendexperiment.New(supertrendexperiment.Config{
			Replay: singlePositionConfig, Pullback: pullbackConfig, Diagnostics: logTradeDiagnostics,
			SwingReview: swingReview, StopReview: stopReviewPath != "",
		})
		if err != nil {
			return err
		}
		experimentItems = append(experimentItems, supertrendExperiment)
	} else if swingReviewPath != "" || stopReviewPath != "" {
		return fmt.Errorf("Supertrend review requires -supertrend-version-compare")
	}
	experimentRegistry, err := experiments.NewRegistry(experimentItems...)
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
				slog.Info("compression breakout diagnostic", "time_ms", snapshot.Current.CloseTime, "side", event.Side, "metadata", event.MetadataJSON)
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
		eventGate.Advance()
		inWindow := snapshot.Current.OpenTime < endTime.UnixMilli()
		entryCandidates := make([]experiments.EntryCandidate, 0, 2)
		if inWindow {
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
				entryCandidates = append(entryCandidates, experiments.EntryCandidate{
					Side: side, Sources: append([]string(nil), sources...), MetadataJSON: metadataJSON,
				})
			}
		}
		experimentFrame := experiments.Frame{
			Snapshot: snapshot,
			Events: experiments.EventSet{
				Platform: platformEvents, CompressionBreakout: compressionBreakoutEvents,
				Impulse: impulseEvents, Pullback: pullbackEvents,
			},
			Entries: entryCandidates, RunID: cfg.Runtime.RunID, InWindow: inWindow,
			InAnalysisWindow: snapshot.Current.OpenTime >= startTime.UnixMilli() && inWindow,
		}
		if currentRegime != nil {
			experimentFrame.Regime = *currentRegime
			experimentFrame.HasRegime = true
		}
		if err := experimentRegistry.OnFrame(ctx, experimentFrame); err != nil {
			return err
		}
		if !inWindow {
			continue
		}
	}
	replay.Finish()
	experimentResults, err := experimentRegistry.Finish(ctx)
	if err != nil {
		return err
	}
	signals, outcomes := replay.Results()
	observations := validationReplay.Results()
	for _, result := range experimentResults {
		if result.Descriptor.Name == "supertrend_version_comparison" {
			if err := reportSupertrendExperiment(result, cfg.Runtime.RunID, string(regimeAnalyzer.Version()), swingReviewPath, stopReviewPath); err != nil {
				return err
			}
			continue
		}
		encoded, err := json.Marshal(result.Summary)
		if err != nil {
			return fmt.Errorf("marshal experiment %s@%s summary: %w", result.Descriptor.Name, result.Descriptor.Version, err)
		}
		slog.Info("signal research experiment", "run_id", cfg.Runtime.RunID, "regime_version", regimeAnalyzer.Version(), "experiment", result.Descriptor.Name, "version", result.Descriptor.Version, "summary", string(encoded))
	}
	if skipPersist {
		slog.Info("signal research completed without persistence", "run_id", cfg.Runtime.RunID, "regime_version", regimeAnalyzer.Version(), "signals", len(signals), "outcomes", len(outcomes), "validation_observations", len(observations), "chop_observations", len(chopObservations), "regime_observations", len(regimeObservations))
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
	slog.Info("signal research completed", "run_id", cfg.Runtime.RunID, "regime_version", regimeAnalyzer.Version(), "signals", len(signals), "outcomes", len(outcomes), "validation_observations", len(observations), "chop_observations", len(chopObservations), "regime_observations", len(regimeObservations))
	return nil
}

func setupResearchLogger(cfg config.Config) error {
	if err := logger.Setup(logger.Config{
		Service: cfg.Logging.Service, Level: cfg.Logging.Level, Format: cfg.Logging.Format,
		Output: cfg.Logging.Output, Dir: cfg.Logging.Dir, Filename: cfg.Logging.Filename,
		MaxSizeMB: cfg.Logging.MaxSizeMB, MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays, Compress: cfg.Logging.Compress,
	}); err != nil {
		return fmt.Errorf("setup research logger: %w", err)
	}
	return nil
}
