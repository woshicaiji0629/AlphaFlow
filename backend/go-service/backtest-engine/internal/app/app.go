package app

import (
	"context"
	"fmt"
	"log/slog"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/logger"
)

type marketStore interface {
	reader.KlineStore
	Close() error
}

var buildMarketStore = buildClickHouseMarketStore

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
	symbol := cfg.Data.Symbols[0]
	klines, err := klineReader.ReadKlines(ctx, reader.Request{
		Exchange: cfg.Data.Exchange,
		Market:   cfg.Data.Market,
		Symbol:   symbol,
		Interval: cfg.Data.Interval,
		Start:    startTime.UnixMilli(),
		End:      endTime.UnixMilli(),
	})
	if err != nil {
		return err
	}
	slog.Info(
		"backtest historical klines loaded",
		"run_id", cfg.Runtime.RunID,
		"strategy_set", cfg.Runtime.StrategySet,
		"exchange", cfg.Data.Exchange,
		"market", cfg.Data.Market,
		"symbol", symbol,
		"interval", cfg.Data.Interval,
		"klines", len(klines),
	)
	return nil
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
