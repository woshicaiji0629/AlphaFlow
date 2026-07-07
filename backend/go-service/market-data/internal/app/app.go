package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/admin"
	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/health"
	"alphaflow/go-service/market-data/internal/indicator"
	"alphaflow/go-service/market-data/internal/store"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/redisclient"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := setupLogger(cfg); err != nil {
		return err
	}

	redisManager, err := redisclient.NewManager(ctx, config.RedisConfigs())
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, closePublisher, restartDelay, err := buildRuntime(ctx, cfg, redisManager)
	if err != nil {
		if closeErr := redisManager.Close(); closeErr != nil {
			slog.Error("close redis failed", "error", closeErr)
		}
		return err
	}
	defer func() {
		closePublisher()
		if err := marketStore.Close(); err != nil {
			slog.Error("close market store failed", "error", err)
		}
		if err := redisManager.Close(); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}()

	if err := runMarketData(ctx, configPath, cfg, collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, restartDelay); err != nil {
		if ctx.Err() != nil {
			slog.Info("market-data stopped")
			return nil
		}
		return fmt.Errorf("run collector: %w", err)
	}
	slog.Info("market-data stopped")
	return nil
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

func runMarketData(
	ctx context.Context,
	configPath string,
	cfg config.Config,
	collectors []*collector.Collector,
	klineAggregator *aggregator.Aggregator,
	indicatorRunner *indicator.Runner,
	healthRunner *health.Runner,
	marketStore *store.MarketStore,
	restartDelay time.Duration,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 4)
	var wg sync.WaitGroup
	for _, c := range collectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCollectorLoop(ctx, c, restartDelay)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- klineAggregator.Run(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- indicatorRunner.Run(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- healthRunner.Run(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- marketStore.RunClickHouseRetry(ctx)
	}()
	if cfg.Backfill.WorkerEnabled {
		backfillMaxWait, err := config.BackfillWorkerMaxWait(cfg)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- admin.RunBackfillWorker(ctx, configPath, admin.BackfillWorkerOptions{
				Batch:   cfg.Backfill.WorkerBatch,
				MaxWait: backfillMaxWait,
			})
		}()
	}

	err := <-errCh
	cancel()
	wg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func runCollectorLoop(ctx context.Context, c *collector.Collector, restartDelay time.Duration) {
	for {
		err := c.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Error("collector stopped", "error", err, "restart_delay", restartDelay)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(restartDelay):
		}
	}
}
