package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/exchange/binance"
	"alphaflow/go-service/market-data/internal/exchange/bitget"
	"alphaflow/go-service/market-data/internal/exchange/bybit"
	"alphaflow/go-service/market-data/internal/exchange/gate"
	"alphaflow/go-service/market-data/internal/health"
	"alphaflow/go-service/market-data/internal/indicator"
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
	collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, restartDelay, err := buildRuntime(ctx, cfg, redisManager)
	if err != nil {
		if closeErr := redisManager.Close(); closeErr != nil {
			slog.Error("close redis failed", "error", closeErr)
		}
		return err
	}
	defer func() {
		if err := marketStore.Close(); err != nil {
			slog.Error("close market store failed", "error", err)
		}
		if err := redisManager.Close(); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}()

	if err := runMarketData(ctx, collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, restartDelay); err != nil {
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

func buildRuntime(
	ctx context.Context,
	cfg config.Config,
	redisManager *redisclient.Manager,
) ([]*collector.Collector, *aggregator.Aggregator, *indicator.Runner, *health.Runner, *store.MarketStore, time.Duration, error) {
	reconnectDelay := collector.DefaultReconnectDelay()

	redisStore := store.NewRedisStore(redisManager.Get(constants.RedisDefaultInstance), store.Retention{
		KlineLimit:     config.KlineLimit(),
		KlineTTL:       config.KlineTTL(),
		LiquidationTTL: config.LiquidationTTL(),
		LatestTTL:      config.LatestTTL(),
		PollingTTL:     config.PollingTTL(),
	})
	marketStore, err := buildStore(ctx, cfg, redisStore)
	if err != nil {
		return nil, nil, nil, nil, nil, 0, err
	}
	httpClient := httpclient.New()

	collectors := []*collector.Collector{}
	if cfg.Binance.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Binance.Symbols,
				Intervals:            config.BinanceIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     true,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Binance.WebSocketConnections,
			},
			binance.NewRESTClient(config.BinanceRESTBase(), httpClient),
			binance.NewWSClient(config.BinanceWSBase()),
			marketStore,
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
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Gate.WebSocketConnections,
			},
			gate.NewRESTClient(config.GateRESTBase(), config.GateSettle(), httpClient),
			gate.NewWSClient(config.GateWSBase(), config.GateSettle(), gateIntervals[0]),
			marketStore,
		))
	}
	if cfg.Bitget.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Bitget.Symbols,
				Intervals:            config.BitgetIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Bitget.WebSocketConnections,
			},
			bitget.NewRESTClient(config.BitgetRESTBase(), config.BitgetProductType(), httpClient),
			bitget.NewWSClient(config.BitgetWSBase(), config.BitgetProductType()),
			marketStore,
		))
	}
	if cfg.Bybit.Enabled {
		collectors = append(collectors, collector.New(
			collector.Options{
				Symbols:              cfg.Bybit.Symbols,
				Intervals:            config.BybitIntervals(),
				RESTLimit:            config.RESTLimit(),
				ReconnectDelay:       reconnectDelay,
				LiquidationLimit:     config.LiquidationLimit(),
				PollOpenInterest:     false,
				OpenInterestInterval: config.OpenInterestInterval(),
				MarkPriceInterval:    config.MarkPriceInterval(),
				WebSocketConnections: cfg.Bybit.WebSocketConnections,
			},
			bybit.NewRESTClient(config.BybitRESTBase(), config.BybitCategory(), httpClient),
			bybit.NewWSClient(config.BybitWSBase(), config.BybitCategory()),
			marketStore,
		))
	}
	if len(collectors) == 0 {
		_ = marketStore.Close()
		return nil, nil, nil, nil, nil, 0, fmt.Errorf("no exchange enabled")
	}
	klineAggregator := aggregator.New(marketStore, aggregator.Options{
		Rules:           aggregationRules(cfg),
		ScanInterval:    config.AggregationScanInterval(),
		LookbackPeriods: config.KlineLimit(),
	})
	indicatorRunner := indicator.NewRunner(marketStore, indicator.RunnerOptions{
		Rules:           indicatorRules(cfg),
		ScanInterval:    config.IndicatorScanInterval(),
		LookbackPeriods: config.IndicatorLookbackPeriods(),
	})
	healthRunner := health.NewRunner(marketStore, health.Options{
		Rules:        healthRules(cfg),
		ScanInterval: config.HealthScanInterval(),
		GapLookback:  config.HealthGapLookback(),
	})
	marketStore.AddKlineHandler(indicatorRunner.HandleKline)
	return collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, reconnectDelay, nil
}

func buildStore(ctx context.Context, cfg config.Config, redisStore *store.RedisStore) (*store.MarketStore, error) {
	if !cfg.ClickHouse.Enabled {
		return store.NewMarketStore(redisStore, nil, store.MarketStoreOptions{}), nil
	}

	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, err
	}
	retryInterval, err := config.ClickHouseRetryInterval(cfg)
	if err != nil {
		return nil, err
	}
	clickHouseStore, err := store.NewClickHouseStore(ctx, store.ClickHouseOptions{
		Addr:        cfg.ClickHouse.Addr,
		Database:    cfg.ClickHouse.Database,
		Username:    cfg.ClickHouse.Username,
		Password:    cfg.ClickHouse.Password,
		DialTimeout: dialTimeout,
		ReadTimeout: readTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect clickhouse: %w", err)
	}

	return store.NewMarketStore(redisStore, clickHouseStore, store.MarketStoreOptions{
		RetryInterval: retryInterval,
		RetryBatch:    cfg.ClickHouse.RetryBatch,
		MaxPending:    cfg.ClickHouse.MaxPending,
	}), nil
}

func runMarketData(
	ctx context.Context,
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

func aggregationRules(cfg config.Config) []aggregator.Rule {
	rules := []aggregator.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, aggregator.Rule{
			Exchange:       "binance",
			Market:         "um",
			Symbols:        cfg.Binance.Symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, missingIntervalRules("gate", config.GateSettle(), cfg.Gate.Symbols)...)
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, missingIntervalRules("bitget", strings.ToLower(config.BitgetProductType()), cfg.Bitget.Symbols)...)
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, aggregator.Rule{
			Exchange:       "bybit",
			Market:         config.BybitCategory(),
			Symbols:        cfg.Bybit.Symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		})
	}
	return rules
}

func indicatorRules(cfg config.Config) []indicator.Rule {
	rules := []indicator.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   cfg.Binance.Symbols,
			Intervals: withExtraIntervals(config.BinanceIntervals(), "10m"),
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "gate",
			Market:    config.GateSettle(),
			Symbols:   cfg.Gate.Symbols,
			Intervals: withExtraIntervals(config.GateIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "bitget",
			Market:    strings.ToLower(config.BitgetProductType()),
			Symbols:   cfg.Bitget.Symbols,
			Intervals: withExtraIntervals(config.BitgetIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, indicator.Rule{
			Exchange:  "bybit",
			Market:    config.BybitCategory(),
			Symbols:   cfg.Bybit.Symbols,
			Intervals: withExtraIntervals(config.BybitIntervals(), "10m"),
		})
	}
	return rules
}

func healthRules(cfg config.Config) []health.Rule {
	rules := []health.Rule{}
	if cfg.Binance.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "binance",
			Market:    "um",
			Symbols:   cfg.Binance.Symbols,
			Intervals: withExtraIntervals(config.BinanceIntervals(), "10m"),
		})
	}
	if cfg.Gate.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "gate",
			Market:    config.GateSettle(),
			Symbols:   cfg.Gate.Symbols,
			Intervals: withExtraIntervals(config.GateIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bitget.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "bitget",
			Market:    strings.ToLower(config.BitgetProductType()),
			Symbols:   cfg.Bitget.Symbols,
			Intervals: withExtraIntervals(config.BitgetIntervals(), "3m", "10m", "2h"),
		})
	}
	if cfg.Bybit.Enabled {
		rules = append(rules, health.Rule{
			Exchange:  "bybit",
			Market:    config.BybitCategory(),
			Symbols:   cfg.Bybit.Symbols,
			Intervals: withExtraIntervals(config.BybitIntervals(), "10m"),
		})
	}
	return rules
}

func withExtraIntervals(intervals []string, extra ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(intervals)+len(extra))
	for _, interval := range append(intervals, extra...) {
		if _, ok := seen[interval]; ok {
			continue
		}
		seen[interval] = struct{}{}
		result = append(result, interval)
	}
	return result
}

func missingIntervalRules(exchange string, market string, symbols []string) []aggregator.Rule {
	return []aggregator.Rule{
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "1m",
			TargetInterval: "3m",
		},
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "5m",
			TargetInterval: "10m",
		},
		{
			Exchange:       exchange,
			Market:         market,
			Symbols:        symbols,
			SourceInterval: "1h",
			TargetInterval: "2h",
		},
	}
}
