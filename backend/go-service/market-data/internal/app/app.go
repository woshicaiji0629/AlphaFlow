package app

import (
	"context"
	"fmt"
	"log/slog"

	"alphaflow/go-service/market-data/internal/binance"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/gate"
	"alphaflow/go-service/market-data/internal/okx"
	"alphaflow/go-service/market-data/internal/store"
	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/httpclient"
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
	defer func() {
		if err := redisManager.Close(); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}()

	collectors, err := buildCollectors(cfg, redisManager)
	if err != nil {
		return err
	}
	if err := runCollectors(ctx, collectors); err != nil {
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

func buildCollectors(
	cfg config.Config,
	redisManager *redisclient.Manager,
) ([]*collector.Collector, error) {
	latestTTL, err := config.LatestTTL(cfg)
	if err != nil {
		return nil, fmt.Errorf("load latest ttl: %w", err)
	}
	pollingTTL, err := config.PollingTTL(cfg)
	if err != nil {
		return nil, fmt.Errorf("load polling ttl: %w", err)
	}
	reconnectDelay, err := config.ReconnectDelay(cfg)
	if err != nil {
		return nil, fmt.Errorf("load reconnect delay: %w", err)
	}

	redisStore := store.NewRedisStore(redisManager.Get(constants.RedisDefaultInstance), store.Retention{
		KlineLimit: cfg.Retention.KlineLimit,
		LatestTTL:  latestTTL,
		PollingTTL: pollingTTL,
	})
	httpClient := httpclient.New()

	collectors := []*collector.Collector{}
	if cfg.Binance.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Binance.Symbols,
				Intervals:            config.BinanceIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     cfg.Retention.LiquidationLimit,
				PollOpenInterest:     true,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
			},
			binance.NewRESTClient(cfg.Binance.RESTBase, httpClient),
			binance.NewWSClient(cfg.Binance.WSBase),
			redisStore,
		))
	}
	if cfg.OKX.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.OKX.Symbols,
				Intervals:            config.OKXIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     cfg.Retention.LiquidationLimit,
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
			},
			okx.NewRESTClient(cfg.OKX.RESTBase, httpClient),
			okx.NewWSClient(cfg.OKX.WSBase),
			redisStore,
		))
	}
	if cfg.Gate.Enabled {
		gateIntervals := config.GateIntervals()
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Gate.Symbols,
				Intervals:            gateIntervals,
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     cfg.Retention.LiquidationLimit,
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
			},
			gate.NewRESTClient(cfg.Gate.RESTBase, cfg.Gate.Settle, httpClient),
			gate.NewWSClient(cfg.Gate.WSBase, cfg.Gate.Settle, gateIntervals[0]),
			redisStore,
		))
	}
	if len(collectors) == 0 {
		return nil, fmt.Errorf("no exchange enabled")
	}
	return collectors, nil
}

func runCollectors(ctx context.Context, collectors []*collector.Collector) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(collectors))
	for _, c := range collectors {
		go func() {
			errCh <- c.Run(ctx)
		}()
	}

	err := <-errCh
	cancel()
	if err != nil {
		return err
	}
	for range len(collectors) - 1 {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}
