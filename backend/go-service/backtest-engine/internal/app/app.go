package app

import (
	"context"
	"fmt"
	"log/slog"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/backtest-engine/internal/simulator"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyregistry"
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
	dataset, err := klineReader.ReadDataset(ctx, reader.DatasetRequest{
		Exchange:         cfg.Data.Exchange,
		Market:           cfg.Data.Market,
		Symbols:          cfg.Data.Symbols,
		Interval:         cfg.Data.Interval,
		ConfirmIntervals: cfg.Data.ConfirmIntervals,
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
		"strategy_set", cfg.Runtime.StrategySet,
		"exchange", cfg.Data.Exchange,
		"market", cfg.Data.Market,
		"symbols", len(cfg.Data.Symbols),
		"interval", cfg.Data.Interval,
		"confirm_intervals", len(cfg.Data.ConfirmIntervals),
		"series", len(dataset.Series),
		"klines", dataset.TotalKlines(),
		"start", startTime.UnixMilli(),
		"end_exclusive", endTime.UnixMilli(),
		"warmup_bars", cfg.Data.WarmupBars,
	)
	summary, err := runStrategyBacktest(ctx, cfg, dataset)
	if err != nil {
		return err
	}
	if err := persistBacktestResults(ctx, cfg, summary); err != nil {
		return err
	}
	slog.Info(
		"backtest strategy execution completed",
		"run_id", cfg.Runtime.RunID,
		"strategy_set", cfg.Runtime.StrategySet,
		"contexts", summary.Contexts,
		"decisions", summary.Decisions,
		"results", summary.Results,
		"events", summary.Events,
		"order_fills", summary.OrderFills,
		"open_positions", summary.OpenPositions,
		"total_trades", summary.RunSummary.TotalTrades,
		"win_rate", summary.RunSummary.WinRate,
		"net_pnl", summary.RunSummary.NetPnL,
		"profit_factor", summary.RunSummary.ProfitFactor,
	)
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
	item, err := strategyregistry.Build(cfg.Runtime.StrategySet)
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	engine := strategy.NewEngine([]strategy.Strategy{item})
	store := position.NewMemoryStore()
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
			MarginQuote: cfg.Sizing.MarginQuote,
			Leverage:    cfg.Sizing.Leverage,
		},
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
			Dataset:          dataset,
			Target:           target,
			Interval:         cfg.Data.Interval,
			ConfirmIntervals: cfg.Data.ConfirmIntervals,
		})
		if err != nil {
			return simulator.ExecutionSummary{}, err
		}
		contexts, err := builder.Build(ctx)
		if err != nil {
			return simulator.ExecutionSummary{}, err
		}
		symbolSummary, err := executor.Execute(ctx, contexts)
		if err != nil {
			return simulator.ExecutionSummary{}, fmt.Errorf("execute backtest symbol=%s: %w", symbol, err)
		}
		summary.Contexts += symbolSummary.Contexts
		summary.Decisions += symbolSummary.Decisions
		summary.Results += symbolSummary.Results
		summary.Events = symbolSummary.Events
		summary.OrderFills = symbolSummary.OrderFills
		summary.OpenPositions = symbolSummary.OpenPositions
		summary.StrategyEvents = symbolSummary.StrategyEvents
	}
	trades, err := simulator.BuildBacktestTrades(store.Events())
	if err != nil {
		return simulator.ExecutionSummary{}, err
	}
	runSummary := simulator.BuildBacktestRunSummary(store.Events(), simulator.SummaryOptions{
		RunID:       cfg.Runtime.RunID,
		StrategySet: cfg.Runtime.StrategySet,
		Exchange:    cfg.Data.Exchange,
		Market:      cfg.Data.Market,
		Symbols:     cfg.Data.Symbols,
		StartTime:   startTime.UnixMilli(),
		EndTime:     endTime.UnixMilli(),
	})
	if err := store.SaveBacktestRunSummary(ctx, runSummary); err != nil {
		return simulator.ExecutionSummary{}, err
	}
	if err := store.SaveBacktestTrades(ctx, trades); err != nil {
		return simulator.ExecutionSummary{}, err
	}
	summary.BacktestTrades = trades
	summary.RunSummary = runSummary
	return summary, nil
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
