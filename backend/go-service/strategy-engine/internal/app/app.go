package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/redisclient"
	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategybus"
	"alphaflow/go-service/strategy-engine/internal/config"
	"alphaflow/go-service/strategy-engine/internal/reader"
	"alphaflow/go-service/strategy-engine/internal/runner"
	"github.com/redis/go-redis/v9"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := setupLogger(cfg); err != nil {
		return err
	}
	redisClient, err := redisclient.New(ctx, config.RedisClientConfig(cfg))
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if err := redisclient.Close(redisClient); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}()

	positionStore := position.NewRedisStore(redisClient, position.RedisStoreOptions{})
	eventStore, closeEventStore, err := buildEventStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeEventStore()

	publisher, err := buildDecisionPublisher(cfg, redisClient)
	if err != nil {
		return err
	}
	runtime, err := buildRuntime(cfg, reader.NewRedisHashReader(redisClient), positionStore, eventStore, publisher)
	if err != nil {
		return err
	}
	if err := runLoop(ctx, cfg, runtime); err != nil {
		if ctx.Err() != nil {
			slog.Info("strategy-engine stopped")
			return nil
		}
		return err
	}
	slog.Info("strategy-engine stopped")
	return nil
}

type runtimeState struct {
	reader *reader.Reader
	runner *runner.Runner
}

func buildRuntime(
	cfg config.Config,
	hashes reader.HashReader,
	positionStore position.Store,
	eventStore position.EventStore,
	publisher runner.DecisionPublisher,
) (runtimeState, error) {
	snapshotReader, err := reader.New(reader.Options{Hashes: hashes})
	if err != nil {
		return runtimeState{}, err
	}
	positionManager := position.NewManager(position.ManagerConfig{
		MaxPositionSize:      cfg.Sizing.MaxPositionSize,
		MarginQuote:          cfg.Sizing.MarginQuote,
		Leverage:             cfg.Sizing.Leverage,
		MinOpenConfidence:    cfg.Sizing.MinOpenConfidence,
		DisableShortExposure: cfg.Sizing.DisableShortExposure,
	})
	strategyRunner, err := runner.New(runner.Options{
		Engine:          strategy.NewEngine([]strategy.Strategy{supertrend.New(supertrend.Config{})}),
		Publisher:       publisher,
		PositionManager: positionManager,
		PositionStore:   positionStore,
		EventStore:      eventStore,
		Broker:          brokerForScope(config.PositionScope(cfg)),
		FeeConfig: runner.FeeConfig{
			FeeRate:   cfg.Fee.FeeRate,
			RebatePct: cfg.Fee.RebatePct,
		},
		SizingConfig: runner.SizingConfig{
			MarginQuote: cfg.Sizing.MarginQuote,
			Leverage:    cfg.Sizing.Leverage,
		},
		Now: func() int64 { return time.Now().UnixMilli() },
	})
	if err != nil {
		return runtimeState{}, err
	}
	return runtimeState{
		reader: snapshotReader,
		runner: strategyRunner,
	}, nil
}

type decisionPublisher interface {
	PublishDecision(ctx context.Context, envelope strategybus.DecisionEnvelope) (string, error)
}

type busDecisionPublisher struct {
	publisher decisionPublisher
	ttl       time.Duration
	now       func() int64
}

func (p busDecisionPublisher) PublishDecision(ctx context.Context, decision strategy.Decision) error {
	messageID, err := p.publisher.PublishDecision(ctx, strategybus.NewDecisionEnvelope(decision, p.now(), p.ttl))
	if err != nil {
		return err
	}
	slog.Info("strategy decision published", "message_id", messageID, "target", decision.Target, "results", len(decision.Results))
	return nil
}

func buildDecisionPublisher(cfg config.Config, redisClient *redis.Client) (runner.DecisionPublisher, error) {
	if cfg.Output.Mode == "local" {
		return nil, nil
	}
	ttl, err := config.OutputDefaultTTL(cfg)
	if err != nil {
		return nil, err
	}
	publisher, err := strategybus.NewRedisPublisher(redisClient, strategybus.RedisPublisherOptions{
		Stream: cfg.Output.Stream,
	})
	if err != nil {
		return nil, err
	}
	return busDecisionPublisher{
		publisher: publisher,
		ttl:       ttl,
		now:       func() int64 { return time.Now().UnixMilli() },
	}, nil
}

func buildEventStore(ctx context.Context, cfg config.Config) (position.EventStore, func(), error) {
	if !cfg.ClickHouse.Enabled {
		return nil, func() {}, nil
	}
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("connect clickhouse event store: %w", err)
	}
	return store, func() {
		if err := store.Close(); err != nil {
			slog.Error("close clickhouse event store failed", "error", err)
		}
	}, nil
}

func brokerForScope(scope strategy.PositionScope) execution.Broker {
	if scope != strategy.PositionScopePaper {
		return nil
	}
	return execution.NewPaperBroker("", func() int64 { return time.Now().UnixMilli() })
}

func runLoop(ctx context.Context, cfg config.Config, runtime runtimeState) error {
	scanInterval, err := config.ScanInterval(cfg)
	if err != nil {
		return err
	}
	targets := config.Targets(cfg)
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		if err := runOnce(ctx, cfg, runtime, targets); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runOnce(ctx context.Context, cfg config.Config, runtime runtimeState, targets []strategy.Target) error {
	for index, target := range targets {
		intervals := config.ConfirmIntervals(cfg.Targets[index])
		input, err := runtime.reader.Read(ctx, target, intervals)
		if err != nil {
			slog.Warn("read strategy snapshot skipped", "target", target, "error", err)
			continue
		}
		decision, err := runtime.runner.Handle(ctx, input)
		if err != nil {
			return fmt.Errorf("handle strategy target %s/%s/%s/%s: %w", target.Exchange, target.Market, target.Symbol, target.Interval, err)
		}
		slog.Info("strategy target evaluated", "target", target, "results", len(decision.Results))
	}
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
