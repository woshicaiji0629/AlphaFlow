package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/report"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyregistry"
	"alphaflow/go-service/pkg/symbolspec"
)

type marketStore interface {
	reader.KlineStore
	Close() error
}

type resultStore interface {
	AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error
	SaveBacktestTrades(ctx context.Context, trades []strategy.BacktestTrade) error
	SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error
	Close() error
}

var buildMarketStore = buildClickHouseMarketStore
var buildResultStore = buildClickHouseResultStore
var buildStrategy = strategyregistry.BuildSpec

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := setupLogger(cfg); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cfg.ClickHouse.Enabled {
		return fmt.Errorf("clickhouse must be enabled for historical backtest data")
	}
	store, err := buildMarketStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			slog.Error("close clickhouse market store failed", "error", err)
		}
	}()
	klineReader, err := reader.New(store)
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
	strategyItem, err := buildConfiguredStrategy(cfg)
	if err != nil {
		return err
	}
	confirmIntervals, err := backtestConfirmIntervals(cfg, strategyItem)
	if err != nil {
		return err
	}
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange:         cfg.Data.Exchange,
		Market:           cfg.Data.Market,
		Symbols:          cfg.Data.Symbols,
		Interval:         cfg.Data.Interval,
		ConfirmIntervals: confirmIntervals,
		Start:            startTime.UnixMilli(),
		End:              endTime.UnixMilli(),
		WarmupBars:       cfg.Data.WarmupBars,
	})
	if err != nil {
		return err
	}
	slog.Info(
		"backtest historical dataset loaded",
		"run_id", cfg.Runtime.RunID,
		"strategy_set", config.StrategySpec(cfg).Name,
		"exchange", cfg.Data.Exchange,
		"market", cfg.Data.Market,
		"symbols", len(cfg.Data.Symbols),
		"interval", cfg.Data.Interval,
		"confirm_intervals", len(confirmIntervals),
		"series", len(dataset.Series),
		"klines", dataset.TotalKlines(),
		"start", startTime.UnixMilli(),
		"end_exclusive", endTime.UnixMilli(),
		"warmup_bars", cfg.Data.WarmupBars,
	)
	summary, executionErr := runStrategyBacktest(ctx, cfg, dataset)
	if executionErr != nil && summary.RunSummary.RunID == "" {
		return executionErr
	}
	if err := persistBacktestResults(ctx, cfg, summary); err != nil {
		if executionErr != nil {
			return fmt.Errorf("persist failed backtest after %v: %w", executionErr, err)
		}
		return err
	}
	slog.Info(
		"backtest strategy execution finished",
		"run_id", cfg.Runtime.RunID,
		"strategy_set", config.StrategySpec(cfg).Name,
		"contexts", summary.Contexts,
		"decisions", summary.Decisions,
		"results", summary.Results,
		"events", summary.Events,
		"order_fills", summary.OrderFills,
		"open_positions", summary.OpenPositions,
		"total_trades", summary.RunSummary.TotalTrades,
		"win_rate", summary.RunSummary.WinRate,
		"net_pnl", summary.RunSummary.NetPnL,
		"max_drawdown", summary.RunSummary.MaxDrawdown,
		"profit_factor", summary.RunSummary.ProfitFactor,
		"status", summary.RunSummary.Status,
		"failures", len(summary.Failures),
	)
	item, err := report.BuildBacktestReportWithOwnedCurves(summary.RunSummary, report.RunStats{
		Contexts:      summary.Contexts,
		Decisions:     summary.Decisions,
		Results:       summary.Results,
		Events:        summary.Events,
		OrderFills:    summary.OrderFills,
		OpenPositions: summary.OpenPositions,
	}, summary.BacktestTrades, cfg.Sizing.InitialEquity, summary.BarEquityCurve, summary.AccountCurve)
	if err != nil {
		return fmt.Errorf("build backtest report: %w", err)
	}
	slog.Info("backtest report", "report", report.FormatBacktestReport(item))
	if err := writeBacktestReportJSON(cfg.Result.ReportJSONPath, item); err != nil {
		return err
	}
	if executionErr != nil {
		return executionErr
	}
	return nil
}

func writeBacktestReportJSON(path string, item report.BacktestReport) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create backtest report dir: %w", err)
		}
	}
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open backtest report json: %w", err)
	}
	writeErr := report.WriteBacktestReport(output, item)
	closeErr := output.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write backtest report json: %w", err)
	}
	return nil
}

func persistBacktestResults(ctx context.Context, cfg config.Config, summary simulator.ExecutionSummary) error {
	store, err := buildResultStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			slog.Error("close clickhouse result store failed", "error", err)
		}
	}()
	if err := appendEventsInBatches(ctx, store, summary.StrategyEvents, cfg.Result.EventBatchSize); err != nil {
		return err
	}
	if err := saveTradesInBatches(ctx, store, summary.BacktestTrades, cfg.Result.TradeBatchSize); err != nil {
		return err
	}
	if err := store.SaveBacktestRunSummary(ctx, summary.RunSummary); err != nil {
		return err
	}
	return nil
}

func appendEventsInBatches(ctx context.Context, store resultStore, events []strategy.StrategyEvent, batchSize int) error {
	if len(events) == 0 {
		return nil
	}
	if batchSize <= 0 {
		return fmt.Errorf("event batch size must be positive")
	}
	for start := 0; start < len(events); start += batchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + batchSize
		if end > len(events) {
			end = len(events)
		}
		if err := store.AppendEvents(ctx, events[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func saveTradesInBatches(ctx context.Context, store resultStore, trades []strategy.BacktestTrade, batchSize int) error {
	if len(trades) == 0 {
		return nil
	}
	if batchSize <= 0 {
		return fmt.Errorf("trade batch size must be positive")
	}
	for start := 0; start < len(trades); start += batchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + batchSize
		if end > len(trades) {
			end = len(trades)
		}
		if err := store.SaveBacktestTrades(ctx, trades[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func runStrategyBacktest(ctx context.Context, cfg config.Config, dataset reader.Dataset) (simulator.ExecutionSummary, error) {
	spec := config.StrategySpec(cfg)
	item, err := buildConfiguredStrategy(cfg)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	engine := strategy.NewEngine([]strategy.Strategy{item})
	confirmIntervals, err := backtestConfirmIntervals(cfg, item)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	store := position.NewMemoryStore()
	preparationProgress := newPreparationProgressLogger(cfg.Runtime.RunID)
	simulationProgress := newSimulationProgressLogger(cfg.Runtime.RunID)
	executor, err := simulator.NewExecutor(simulator.ExecutorOptions{
		Engine: engine,
		Store:  store,
		ManagerConfig: position.ManagerConfig{
			MaxPositionSize:      cfg.Sizing.MaxPositionSize,
			MarginQuote:          cfg.Sizing.MarginQuote,
			Leverage:             cfg.Sizing.Leverage,
			MinOpenConfidence:    cfg.Sizing.MinOpenConfidence,
			DisableShortExposure: cfg.Sizing.DisableShortExposure,
		},
		FeeConfig: paperhandler.FeeConfig{
			FeeRate:   cfg.Fee.FeeRate,
			RebatePct: cfg.Fee.RebatePct,
		},
		SizingConfig: paperhandler.SizingConfig{
			MarginQuote:  cfg.Sizing.MarginQuote,
			Leverage:     cfg.Sizing.Leverage,
			Capabilities: symbolCapabilities(cfg),
		},
		AccountConfig: simulator.AccountConfig{
			InitialEquity: cfg.Sizing.InitialEquity,
			MarginQuote:   cfg.Sizing.MarginQuote,
			Leverage:      cfg.Sizing.Leverage,
			FeeRate:       cfg.Fee.FeeRate,
			RebatePct:     cfg.Fee.RebatePct,
		},
		SlippageBps:   cfg.Execution.SlippageBps,
		Progress:      simulationProgress,
		ProgressTotal: backtestContextCount(dataset, cfg.Data.Interval),
	})
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	startTime, err := config.StartTime(cfg)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	endTime, err := config.EndTime(cfg)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	summary := simulator.ExecutionSummary{}
	iteratorStates := []contextIteratorState{}
	defer func() {
		for _, state := range iteratorStates {
			state.iterator.Close()
		}
	}()
	indicatorConcurrency := cfg.Data.IndicatorConcurrency
	if indicatorConcurrency <= 0 {
		indicatorConcurrency = runtime.GOMAXPROCS(0)
	}
	indicatorLimiter := make(chan struct{}, indicatorConcurrency)
	for _, symbol := range cfg.Data.Symbols {
		target := strategy.Target{
			Exchange: cfg.Data.Exchange,
			Market:   cfg.Data.Market,
			Symbol:   symbol,
			Interval: cfg.Data.Interval,
			Scope:    strategy.PositionScopeBacktest,
			RunID:    cfg.Runtime.RunID,
		}
		builder, err := simulator.NewSnapshotBuilder(simulator.SnapshotBuilderOptions{
			Dataset:              dataset,
			Target:               target,
			Interval:             cfg.Data.Interval,
			ConfirmIntervals:     confirmIntervals,
			CalculationWindow:    int(cfg.Data.WarmupBars),
			IndicatorBatchSize:   cfg.Data.IndicatorBatchSize,
			IndicatorConcurrency: cfg.Data.IndicatorConcurrency,
			IndicatorLimiter:     indicatorLimiter,
			Progress:             preparationProgress,
		})
		if err != nil {
			return simulator.ExecutionSummary{}, err
		}
		iterator, err := builder.Iterator(ctx)
		if err != nil {
			return simulator.ExecutionSummary{}, err
		}
		item, ok, err := iterator.Next(ctx)
		if err != nil {
			return simulator.ExecutionSummary{}, err
		}
		iteratorStates = append(iteratorStates, contextIteratorState{iterator: iterator, current: item, ok: ok})
	}
	summary, executionErr := executeContextBatches(ctx, executor, iteratorStates, 1)
	events := store.DetachEvents()
	summary.StrategyEvents = events
	summary.Events = len(events)
	summary.OrderFills = 0
	for _, event := range events {
		if event.EventType == strategy.EventTypeOrderFilled {
			summary.OrderFills++
		}
	}
	trades, err := simulator.BuildBacktestTrades(events)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	runSummary := simulator.BuildBacktestRunSummary(events, simulator.SummaryOptions{
		RunID:        cfg.Runtime.RunID,
		StrategySet:  spec.Name,
		Exchange:     cfg.Data.Exchange,
		Market:       cfg.Data.Market,
		Symbols:      cfg.Data.Symbols,
		AccountCurve: summary.AccountCurve,
		StartTime:    startTime.UnixMilli(),
		EndTime:      endTime.UnixMilli(),
	})
	strategySpecJSON, err := json.Marshal(spec)
	if err != nil {
		return simulator.ExecutionSummary{}, fmt.Errorf("marshal strategy spec: %w", err)
	}
	if runSummary.Metadata == nil {
		runSummary.Metadata = map[string]string{}
	}
	runSummary.Metadata["strategy_spec"] = string(strategySpecJSON)
	if executionErr != nil {
		runSummary.Status = strategy.BacktestRunStatusFailed
		runSummary.FailureReason = executionErr.Error()
		runSummary.Metadata["strategy_failure_count"] = fmt.Sprintf("%d", len(summary.Failures))
	}
	if err := store.SaveBacktestRunSummary(ctx, runSummary); err != nil {
		return simulator.ExecutionSummary{}, err
	}
	if err := store.SaveBacktestTrades(ctx, trades); err != nil {
		return simulator.ExecutionSummary{}, err
	}
	summary.BacktestTrades = trades
	summary.RunSummary = runSummary
	if executionErr != nil {
		return summary, fmt.Errorf("execute backtest: %w", executionErr)
	}
	return summary, nil
}

type contextIteratorState struct {
	iterator *simulator.ContextIterator
	current  strategy.Context
	ok       bool
}

func executeContextBatches(
	ctx context.Context,
	executor *simulator.Executor,
	states []contextIteratorState,
	batchSize int,
) (simulator.ExecutionSummary, error) {
	if batchSize <= 0 {
		return simulator.ExecutionSummary{}, fmt.Errorf("context batch size must be positive")
	}
	summary := simulator.ExecutionSummary{}
	for {
		batch, err := nextContextIteratorBatch(ctx, states, batchSize)
		if err != nil {
			return summary, err
		}
		if len(batch) == 0 {
			return summary, nil
		}
		partial, executionErr := executor.ExecuteIncremental(ctx, batch)
		mergeExecutionSummary(&summary, partial)
		if executionErr != nil {
			return summary, executionErr
		}
	}
}

func nextContextIteratorBatch(ctx context.Context, states []contextIteratorState, batchSize int) ([]strategy.Context, error) {
	batch := make([]strategy.Context, 0, batchSize)
	lastOpenTime := int64(0)
	for {
		selected := earliestContextIterator(states)
		if selected < 0 {
			return batch, nil
		}
		nextOpenTime := contextOpenTime(states[selected].current)
		if len(batch) >= batchSize && nextOpenTime != lastOpenTime {
			return batch, nil
		}
		batch = append(batch, states[selected].current)
		lastOpenTime = nextOpenTime
		item, ok, err := states[selected].iterator.Next(ctx)
		if err != nil {
			return nil, err
		}
		states[selected].current = item
		states[selected].ok = ok
	}
}

func earliestContextIterator(states []contextIteratorState) int {
	selected := -1
	for index := range states {
		if !states[index].ok {
			continue
		}
		if selected < 0 || contextOpenTime(states[index].current) < contextOpenTime(states[selected].current) {
			selected = index
		}
	}
	return selected
}

func mergeExecutionSummary(target *simulator.ExecutionSummary, partial simulator.ExecutionSummary) {
	target.Contexts += partial.Contexts
	target.Decisions += partial.Decisions
	target.Results += partial.Results
	target.Events = partial.Events
	target.OpenPositions = partial.OpenPositions
	target.OrderFills = partial.OrderFills
	target.StrategyEvents = partial.StrategyEvents
	target.BarEquityCurve = append(target.BarEquityCurve, partial.BarEquityCurve...)
	target.AccountCurve = append(target.AccountCurve, partial.AccountCurve...)
	target.Failures = append(target.Failures, partial.Failures...)
}

func backtestContextCount(dataset reader.Dataset, interval string) int {
	total := 0
	for _, series := range dataset.Series {
		if series.Key.Interval == interval {
			total += series.Result.TradingCount
		}
	}
	return total
}

func newPreparationProgressLogger(runID string) simulator.PreparationProgress {
	lastReported := map[string]int{}
	startedAt := map[string]time.Time{}
	lastReportedAt := map[string]time.Time{}
	return func(stage string, interval string, processed int, total int) {
		key := stage + ":" + interval
		now := time.Now()
		if startedAt[key].IsZero() {
			startedAt[key] = now
		}
		step := total / 20
		if step < 1 {
			step = 1
		}
		if processed != 1 && processed != total && processed-lastReported[key] < step && now.Sub(lastReportedAt[key]) < 10*time.Second {
			return
		}
		lastReported[key] = processed
		lastReportedAt[key] = now
		logBacktestProgress(runID, stage, interval, processed, total, 0, startedAt[key])
	}
}

func newSimulationProgressLogger(runID string) func(processed int, total int, trades int) {
	startedAt := time.Now()
	lastReported := 0
	lastReportedAt := time.Time{}
	return func(processed int, total int, trades int) {
		now := time.Now()
		step := total / 100
		if step < 1 {
			step = 1
		}
		if processed != 1 && processed != total && processed-lastReported < step && now.Sub(lastReportedAt) < 10*time.Second {
			return
		}
		lastReported = processed
		lastReportedAt = now
		logBacktestProgress(runID, "simulation", "", processed, total, trades, startedAt)
	}
}

func logBacktestProgress(runID string, stage string, interval string, processed int, total int, trades int, startedAt time.Time) {
	elapsed := time.Since(startedAt)
	percent := 0.0
	rate := 0.0
	eta := time.Duration(0)
	if total > 0 {
		percent = float64(processed) * 100 / float64(total)
	}
	if elapsed > 0 {
		rate = float64(processed) / elapsed.Seconds()
		if rate > 0 && total > processed {
			eta = time.Duration(float64(total-processed) / rate * float64(time.Second))
		}
	}
	slog.Info(
		"backtest progress",
		"run_id", runID,
		"stage", stage,
		"interval", interval,
		"processed", processed,
		"total", total,
		"percent", percent,
		"rate_per_second", rate,
		"trades", trades,
		"elapsed", elapsed.String(),
		"eta", eta.String(),
	)
}

func buildConfiguredStrategy(cfg config.Config) (strategy.Strategy, error) {
	return buildStrategy(config.StrategySpec(cfg))
}

func backtestConfirmIntervals(cfg config.Config, item strategy.Strategy) ([]string, error) {
	target := strategy.Target{Interval: cfg.Data.Interval}
	required, err := strategy.NewEngine([]strategy.Strategy{item}).RequiredIntervals(target)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(required)+len(cfg.Data.ConfirmIntervals))
	intervals := make([]string, 0, len(required)+len(cfg.Data.ConfirmIntervals))
	for _, interval := range append(required, cfg.Data.ConfirmIntervals...) {
		interval = strings.TrimSpace(interval)
		if interval == "" || interval == cfg.Data.Interval {
			continue
		}
		if _, ok := seen[interval]; ok {
			continue
		}
		seen[interval] = struct{}{}
		intervals = append(intervals, interval)
	}
	return intervals, nil
}

func symbolCapabilities(cfg config.Config) map[symbolspec.Key]symbolspec.Capability {
	if len(cfg.SymbolSpecs) == 0 {
		return nil
	}
	capabilities := make(map[symbolspec.Key]symbolspec.Capability, len(cfg.SymbolSpecs))
	for _, item := range cfg.SymbolSpecs {
		capability := symbolspec.Normalize(symbolspec.Capability{
			Exchange:     item.Exchange,
			Market:       item.Market,
			Symbol:       item.Symbol,
			PriceTick:    item.PriceTick,
			QuantityStep: item.QuantityStep,
			MinQuantity:  item.MinQuantity,
			MinNotional:  item.MinNotional,
			ContractSize: item.ContractSize,
			QuantityUnit: item.QuantityUnit,
		})
		capabilities[capability.Key()] = capability
	}
	return capabilities
}

func sortBacktestContexts(contexts []strategy.Context) {
	sort.SliceStable(contexts, func(i, j int) bool {
		leftTime := contextOpenTime(contexts[i])
		rightTime := contextOpenTime(contexts[j])
		if leftTime == rightTime {
			return contexts[i].Target.Symbol < contexts[j].Target.Symbol
		}
		return leftTime < rightTime
	})
}

func contextOpenTime(item strategy.Context) int64 {
	snapshot, ok := item.Snapshots[item.Target.Interval]
	if !ok {
		return 0
	}
	return snapshot.Current.OpenTime
}

func buildClickHouseMarketStore(ctx context.Context, cfg config.Config) (marketStore, error) {
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, err
	}
	store, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr:        cfg.ClickHouse.Addr,
		Database:    cfg.ClickHouse.Database,
		Username:    cfg.ClickHouse.Username,
		Password:    cfg.ClickHouse.Password,
		DialTimeout: dialTimeout,
		ReadTimeout: readTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect clickhouse market store: %w", err)
	}
	return store, nil
}

func buildClickHouseResultStore(ctx context.Context, cfg config.Config) (resultStore, error) {
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, err
	}
	store, err := position.NewClickHouseStore(ctx, position.ClickHouseOptions{
		Addr:        cfg.ClickHouse.Addr,
		Database:    cfg.ClickHouse.Database,
		Username:    cfg.ClickHouse.Username,
		Password:    cfg.ClickHouse.Password,
		DialTimeout: dialTimeout,
		ReadTimeout: readTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect clickhouse result store: %w", err)
	}
	return store, nil
}

func setupLogger(cfg config.Config) error {
	if err := logger.Setup(logger.Config{
		Service:    cfg.Logging.Service,
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		Dir:        cfg.Logging.Dir,
		Filename:   cfg.Logging.Filename,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}); err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	return nil
}
