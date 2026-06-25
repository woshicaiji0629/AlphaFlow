package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/market-data/internal/binance"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/gate"
	"alphaflow/go-service/market-data/internal/okx"
	"alphaflow/go-service/market-data/internal/store"
	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/redisclient"
)

func main() {
	configPath := flag.String("config", "", "path to market-data config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}
	if err := logger.Setup(logger.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		FilePath:   cfg.Logging.FilePath,
		AddSource:  cfg.Logging.AddSource,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}); err != nil {
		slog.Error("setup logger failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisClient, err := redisclient.New(ctx, redisclient.Config{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	})
	if err != nil {
		slog.Error("connect redis failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redisclient.Close(redisClient); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}()

	httpClient := httpclient.New()
	binanceRESTClient := binance.NewRESTClient(cfg.Binance.RESTBase, httpClient)
	binanceWSClient := binance.NewWSClient(cfg.Binance.WSBase)
	latestTTL, err := config.LatestTTL(cfg)
	if err != nil {
		slog.Error("load latest ttl failed", "error", err)
		os.Exit(1)
	}
	pollingTTL, err := config.PollingTTL(cfg)
	if err != nil {
		slog.Error("load polling ttl failed", "error", err)
		os.Exit(1)
	}
	redisStore := store.NewRedisStore(redisClient, store.Retention{
		KlineLimit: cfg.Retention.KlineLimit,
		LatestTTL:  latestTTL,
		PollingTTL: pollingTTL,
	})
	reconnectDelay, err := config.ReconnectDelay(cfg)
	if err != nil {
		slog.Error("load reconnect delay failed", "error", err)
		os.Exit(1)
	}

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
			binanceRESTClient,
			binanceWSClient,
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
		slog.Error("no exchange enabled")
		os.Exit(1)
	}

	if err := runCollectors(ctx, collectors); err != nil {
		if ctx.Err() != nil {
			slog.Info("market-data stopped")
			return
		}
		slog.Error("run collector failed", "error", err)
		os.Exit(1)
	}
	slog.Info("market-data stopped")
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
