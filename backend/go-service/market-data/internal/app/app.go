package app

import (
	"context"
	"errors"
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

	errCh := make(chan error, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- marketStore.RunClickHouseRetry(ctx)
	}()
	for _, c := range collectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCollectorRealtimeLoop(ctx, c, restartDelay)
		}()
	}

	if err := runStartupBackfill(ctx, collectors); err != nil {
		cancel()
		wg.Wait()
		return err
	}
	if err := klineAggregator.RunOnce(ctx); err != nil {
		cancel()
		wg.Wait()
		return fmt.Errorf("startup aggregate klines: %w", err)
	}
	if err := indicatorRunner.RunOnce(ctx); err != nil {
		cancel()
		wg.Wait()
		return fmt.Errorf("startup calculate indicators: %w", err)
	}
	marketStore.AddKlineHandler(indicatorRunner.HandleKline)
	slog.Info("market-data kline warmup and indicator startup completed")

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

func runStartupBackfill(ctx context.Context, collectors []*collector.Collector) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(collectors))
	for _, c := range collectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- c.Backfill(ctx)
		}()
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func runCollectorRealtimeLoop(ctx context.Context, c *collector.Collector, restartDelay time.Duration) {
	for {
		err := c.RunRealtime(ctx)
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
