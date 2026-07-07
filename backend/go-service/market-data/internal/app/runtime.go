package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/aggregator"
	"alphaflow/go-service/market-data/internal/collector"
	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/health"
	"alphaflow/go-service/market-data/internal/indicator"
	"alphaflow/go-service/market-data/internal/store"
	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/redisclient"
)

func buildRuntime(
	ctx context.Context,
	cfg config.Config,
	redisManager *redisclient.Manager,
) ([]*collector.Collector, *aggregator.Aggregator, *indicator.Runner, *health.Runner, *store.MarketStore, func(), time.Duration, error) {
	reconnectDelay := collector.DefaultReconnectDelay()
	closePublisher := func() {}

	redisStore := store.NewRedisStore(redisManager.Get(constants.RedisDefaultInstance), store.Retention{
		KlineLimit:     config.IndicatorKlineCacheLimit(),
		KlineTTL:       config.KlineTTL(),
		IndicatorLimit: int64(config.IndicatorSnapshotCacheLimit()),
		LiquidationTTL: config.LiquidationTTL(),
		LatestTTL:      config.LatestTTL(),
		PollingTTL:     config.PollingTTL(),
	})
	marketStore, err := buildStore(ctx, cfg, redisStore)
	if err != nil {
		return nil, nil, nil, nil, nil, closePublisher, 0, err
	}
	publisher, closePublisher, err := buildMarketSnapshotPublisher(cfg)
	if err != nil {
		_ = marketStore.Close()
		return nil, nil, nil, nil, nil, closePublisher, 0, err
	}

	collectors := buildCollectors(cfg, marketStore, reconnectDelay)
	if len(collectors) == 0 {
		_ = marketStore.Close()
		closePublisher()
		return nil, nil, nil, nil, nil, closePublisher, 0, fmt.Errorf("no exchange enabled")
	}
	klineAggregator := aggregator.New(marketStore, aggregator.Options{
		Rules:           aggregationRules(cfg),
		ScanInterval:    config.AggregationScanInterval(),
		LookbackPeriods: config.KlineLimit(),
	})
	indicatorRunnerOptions := indicator.RunnerOptions{
		Rules:              indicatorRules(cfg),
		ScanInterval:       config.IndicatorScanInterval(),
		LookbackPeriods:    config.IndicatorLookbackPeriods(),
		WindowLookback:     int(config.IndicatorWindowLookback()),
		SnapshotCacheLimit: config.IndicatorSnapshotCacheLimit(),
		Publisher:          publisher,
	}
	if publisher != nil {
		publishTTL, err := config.MarketBusDefaultTTL(cfg)
		if err != nil {
			_ = marketStore.Close()
			closePublisher()
			return nil, nil, nil, nil, nil, closePublisher, 0, err
		}
		indicatorRunnerOptions.PublishTTL = publishTTL
	}
	if cfg.Indicator.Enabled {
		indicatorAckWait, err := config.IndicatorQueueAckWait(cfg)
		if err != nil {
			_ = marketStore.Close()
			closePublisher()
			return nil, nil, nil, nil, nil, closePublisher, 0, err
		}
		indicatorWorkerMaxWait, err := config.IndicatorQueueWorkerMaxWait(cfg)
		if err != nil {
			_ = marketStore.Close()
			closePublisher()
			return nil, nil, nil, nil, nil, closePublisher, 0, err
		}
		indicatorQueue, err := indicator.NewNATSTaskQueue(indicator.NATSTaskQueueOptions{
			URL:           cfg.NATS.URL,
			AckWait:       indicatorAckWait,
			MaxDeliveries: cfg.Indicator.MaxDeliveries,
			MaxPending:    cfg.Indicator.MaxPending,
		})
		if err != nil {
			_ = marketStore.Close()
			closePublisher()
			return nil, nil, nil, nil, nil, closePublisher, 0, fmt.Errorf("connect nats indicator task queue: %w", err)
		}
		indicatorRunnerOptions.TaskQueue = indicatorQueue
		indicatorRunnerOptions.TaskBatch = cfg.Indicator.WorkerBatch
		indicatorRunnerOptions.TaskMaxWait = indicatorWorkerMaxWait
		indicatorRunnerOptions.TaskMaxDeliveries = cfg.Indicator.MaxDeliveries
		indicatorRunnerOptions.TaskWorkers = cfg.Indicator.WorkerCount
	}
	indicatorRunner := indicator.NewRunner(marketStore, indicator.RunnerOptions{
		Rules:             indicatorRunnerOptions.Rules,
		ScanInterval:      indicatorRunnerOptions.ScanInterval,
		LookbackPeriods:   indicatorRunnerOptions.LookbackPeriods,
		CalculateOptions:  indicatorRunnerOptions.CalculateOptions,
		Publisher:         indicatorRunnerOptions.Publisher,
		PublishTTL:        indicatorRunnerOptions.PublishTTL,
		TaskQueue:         indicatorRunnerOptions.TaskQueue,
		TaskBatch:         indicatorRunnerOptions.TaskBatch,
		TaskMaxWait:       indicatorRunnerOptions.TaskMaxWait,
		TaskMaxDeliveries: indicatorRunnerOptions.TaskMaxDeliveries,
		TaskWorkers:       indicatorRunnerOptions.TaskWorkers,
	})
	healthRunner := health.NewRunner(marketStore, health.Options{
		Rules:        healthRules(cfg),
		ScanInterval: config.HealthScanInterval(),
		GapLookback:  config.HealthGapLookback(),
	})
	marketStore.AddKlineHandler(indicatorRunner.HandleKline)
	return collectors, klineAggregator, indicatorRunner, healthRunner, marketStore, closePublisher, reconnectDelay, nil
}

func buildMarketSnapshotPublisher(cfg config.Config) (indicator.SnapshotPublisher, func(), error) {
	if !cfg.MarketBus.Enabled {
		return nil, func() {}, nil
	}
	publisher, err := marketbus.NewNATSPublisher(marketbus.NATSPublisherOptions{
		URL:             cfg.NATS.URL,
		Stream:          cfg.MarketBus.Stream,
		ClosedSubject:   cfg.MarketBus.ClosedSubject,
		RealtimeSubject: cfg.MarketBus.RealtimeSubject,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect nats market snapshot publisher: %w", err)
	}
	return publisher, func() {
		if err := publisher.Close(); err != nil {
			slog.Error("close market snapshot publisher failed", "error", err)
		}
	}, nil
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
	pendingAckWait, err := config.ClickHousePendingAckWait(cfg)
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
	pendingQueue, err := store.NewNATSPendingQueue(store.NATSPendingQueueOptions{
		URL:           cfg.NATS.URL,
		AckWait:       pendingAckWait,
		MaxDeliveries: cfg.ClickHouse.PendingMaxDeliveries,
		MaxPending:    cfg.ClickHouse.MaxPending,
	})
	if err != nil {
		if closeErr := clickHouseStore.Close(); closeErr != nil {
			slog.Error("close clickhouse after nats pending queue failure failed", "error", closeErr)
		}
		return nil, fmt.Errorf("connect nats clickhouse pending queue: %w", err)
	}

	return store.NewMarketStore(redisStore, clickHouseStore, store.MarketStoreOptions{
		RetryInterval: retryInterval,
		RetryBatch:    cfg.ClickHouse.RetryBatch,
		MaxDeliveries: cfg.ClickHouse.PendingMaxDeliveries,
		PendingQueue:  pendingQueue,
	}), nil
}
