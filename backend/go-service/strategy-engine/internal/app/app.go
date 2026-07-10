package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/marketbus"
	"alphaflow/go-service/pkg/position"
	"alphaflow/go-service/pkg/redisclient"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategybus"
	"alphaflow/go-service/pkg/strategyregistry"
	"alphaflow/go-service/strategy-engine/internal/config"
	"alphaflow/go-service/strategy-engine/internal/marketstate"
	"alphaflow/go-service/strategy-engine/internal/reader"
	"alphaflow/go-service/strategy-engine/internal/runner"
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

	publisher, closePublisher, err := buildDecisionPublisher(cfg)
	if err != nil {
		return err
	}
	defer closePublisher()
	redisReader := reader.NewRedisHashReader(redisClient)
	runtime, err := buildRuntime(cfg, redisReader, redisReader, positionStore, eventStore, publisher)
	if err != nil {
		return err
	}
	marketBus, closeMarketBus, err := buildMarketInputBus(cfg)
	if err != nil {
		return err
	}
	defer closeMarketBus()
	if err := runLoop(ctx, cfg, runtime, marketBus); err != nil {
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
	reader      *reader.Reader
	runner      *runner.Runner
	marketState *marketstate.Store
}

func buildRuntime(
	cfg config.Config,
	hashes reader.HashReader,
	strings reader.StringReader,
	positionStore position.Store,
	eventStore position.EventStore,
	publisher runner.DecisionPublisher,
) (runtimeState, error) {
	snapshotReader, err := reader.New(reader.Options{Hashes: hashes, Strings: strings})
	if err != nil {
		return runtimeState{}, err
	}
	maxMessageAge, err := config.MarketInputMaxMessageAge(cfg)
	if err != nil {
		return runtimeState{}, err
	}
	realtimeStaleAfter, err := config.MarketInputRealtimeStaleAfter(cfg)
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
	strategies, err := strategyregistry.BuildSpecs(config.StrategySpecs(cfg))
	if err != nil {
		return runtimeState{}, err
	}
	strategyRunner, err := runner.New(runner.Options{
		Engine:          strategy.NewEngine(strategies),
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
		marketState: marketstate.New(marketstate.Options{
			MaxMessageAge:     maxMessageAge,
			RealtimeStaleAge:  realtimeStaleAfter,
			ClosedStaleFactor: cfg.MarketInput.ClosedStaleFactor,
		}),
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
	slog.Info("strategy decision published", "message_id", messageID, "target", decision.Target, "results", len(decision.Results), "failures", len(decision.Failures))
	return nil
}

func buildDecisionPublisher(cfg config.Config) (runner.DecisionPublisher, func(), error) {
	if cfg.Output.Mode == "local" {
		return nil, func() {}, nil
	}
	ttl, err := config.OutputDefaultTTL(cfg)
	if err != nil {
		return nil, nil, err
	}
	publisher, err := strategybus.NewNATSPublisher(strategybus.NATSPublisherOptions{
		URL:     cfg.NATS.URL,
		Stream:  cfg.Output.Stream,
		Subject: cfg.Output.Subject,
	})
	if err != nil {
		return nil, nil, err
	}
	return busDecisionPublisher{
			publisher: publisher,
			ttl:       ttl,
			now:       func() int64 { return time.Now().UnixMilli() },
		}, func() {
			if err := publisher.Close(); err != nil {
				slog.Error("close nats publisher failed", "error", err)
			}
		}, nil
}

type marketInputBus interface {
	ReadSnapshots(ctx context.Context) ([]marketbus.SnapshotMessage, error)
	Ack(ctx context.Context, ids ...string) error
	DeadLetter(ctx context.Context, message marketbus.SnapshotMessage, reason string) error
}

func buildMarketInputBus(cfg config.Config) (marketInputBus, func(), error) {
	if cfg.MarketInput.Mode != "bus" {
		return nil, func() {}, nil
	}
	block, err := config.MarketInputBlock(cfg)
	if err != nil {
		return nil, nil, err
	}
	ackWait, err := config.MarketInputAckWait(cfg)
	if err != nil {
		return nil, nil, err
	}
	bus, err := marketbus.NewNATSBus(marketbus.NATSOptions{
		URL:               cfg.NATS.URL,
		Stream:            cfg.MarketInput.Stream,
		ClosedSubject:     cfg.MarketInput.ClosedSubject,
		RealtimeSubject:   cfg.MarketInput.RealtimeSubject,
		Durable:           cfg.MarketInput.Durable,
		Batch:             cfg.MarketInput.Batch,
		Block:             block,
		AckWait:           ackWait,
		MaxDeliveries:     cfg.MarketInput.MaxDeliveries,
		DeadLetterSubject: cfg.MarketInput.DeadLetterSubject,
	})
	if err != nil {
		return nil, nil, err
	}
	return bus, func() {
		if err := bus.Close(); err != nil {
			slog.Error("close market input bus failed", "error", err)
		}
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

func runLoop(ctx context.Context, cfg config.Config, runtime runtimeState, marketBus marketInputBus) error {
	if err := seedMarketState(ctx, cfg, runtime); err != nil {
		slog.Warn("seed market state from redis skipped", "error", err)
	}
	if cfg.MarketInput.Mode == "bus" {
		return runMarketBusLoop(ctx, cfg, runtime, marketBus)
	}
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

func seedMarketState(ctx context.Context, cfg config.Config, runtime runtimeState) error {
	targets := config.Targets(cfg)
	for index, target := range targets {
		intervals, err := targetIntervals(runtime, target, config.ConfirmIntervals(cfg.Targets[index]))
		if err != nil {
			return err
		}
		input, err := runtime.reader.Read(ctx, target, intervals)
		if err != nil {
			return err
		}
		runtime.marketState.Seed(input)
	}
	return nil
}

func runMarketBusLoop(ctx context.Context, cfg config.Config, runtime runtimeState, marketBus marketInputBus) error {
	if marketBus == nil {
		return fmt.Errorf("market input bus is required")
	}
	targets := config.Targets(cfg)
	for {
		messages, err := marketBus.ReadSnapshots(ctx)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if err := handleMarketSnapshotMessage(ctx, cfg, runtime, marketBus, message, targets); err != nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func handleMarketSnapshotMessage(
	ctx context.Context,
	cfg config.Config,
	runtime runtimeState,
	marketBus marketInputBus,
	message marketbus.SnapshotMessage,
	targets []strategy.Target,
) error {
	now := time.Now().UnixMilli()
	attrs := marketSnapshotLogAttrs(message, now)
	applied, err := runtime.marketState.Apply(message.Envelope)
	if err != nil {
		slog.Warn("market snapshot skipped", append(attrs, "error", err)...)
		return marketBus.Ack(ctx, message.ID)
	}
	if !applied {
		slog.Debug("market snapshot consumed", append(attrs, "applied", false, "matched_targets", 0)...)
		return marketBus.Ack(ctx, message.ID)
	}
	matchedTargets := 0
	for index, target := range targets {
		intervals, err := targetIntervals(runtime, target, config.ConfirmIntervals(cfg.Targets[index]))
		if err != nil {
			return err
		}
		if !messageTriggersTarget(message.Envelope, target) {
			continue
		}
		matchedTargets++
		if err := runTargetFromState(ctx, cfg, runtime, target, intervals); err != nil {
			return err
		}
	}
	slog.Debug("market snapshot consumed", append(attrs, "applied", true, "matched_targets", matchedTargets)...)
	return marketBus.Ack(ctx, message.ID)
}

func messageTriggersTarget(envelope marketbus.SnapshotEnvelope, target strategy.Target) bool {
	return envelope.Type == marketbus.SnapshotTypeClosed &&
		strings.EqualFold(envelope.Target.Exchange, target.Exchange) &&
		strings.EqualFold(envelope.Target.Market, target.Market) &&
		strings.EqualFold(envelope.Target.Symbol, target.Symbol) &&
		envelope.Target.Interval == target.Interval
}

func marketSnapshotLogAttrs(message marketbus.SnapshotMessage, now int64) []any {
	lagMillis := int64(0)
	if message.Envelope.CreatedAt > 0 {
		lagMillis = now - message.Envelope.CreatedAt
	}
	expiresInMillis := int64(0)
	if message.Envelope.ExpiresAt > 0 {
		expiresInMillis = message.Envelope.ExpiresAt - now
	}
	return []any{
		"message_id", message.ID,
		"snapshot_type", message.Envelope.Type,
		"trace_id", message.Envelope.TraceID,
		"target", message.Envelope.Target,
		"lag_ms", lagMillis,
		"expires_in_ms", expiresInMillis,
		"delivery_count", message.DeliveryCount,
	}
}

func runTargetFromState(
	ctx context.Context,
	cfg config.Config,
	runtime runtimeState,
	target strategy.Target,
	intervals []string,
) error {
	input, degraded, reason, err := runtime.marketState.BuildContext(target, intervals)
	if err != nil {
		slog.Warn("build strategy context from market state skipped", "target", target, "error", err)
		return nil
	}
	rejectOpen := degraded && cfg.MarketInput.RejectOpenWhenDegraded
	decision, err := runtime.runner.HandleWithDegradation(ctx, input, rejectOpen, reason)
	if err != nil {
		return fmt.Errorf("handle strategy target %s/%s/%s/%s: %w", target.Exchange, target.Market, target.Symbol, target.Interval, err)
	}
	logStrategyFailures(target, decision.Failures)
	slog.Info("strategy target evaluated", "target", target, "results", len(decision.Results), "degraded", degraded, "degraded_reason", reason)
	return nil
}

func runOnce(ctx context.Context, cfg config.Config, runtime runtimeState, targets []strategy.Target) error {
	for index, target := range targets {
		intervals, err := targetIntervals(runtime, target, config.ConfirmIntervals(cfg.Targets[index]))
		if err != nil {
			return err
		}
		input, err := runtime.reader.Read(ctx, target, intervals)
		if err != nil {
			slog.Warn("read strategy snapshot skipped", "target", target, "error", err)
			continue
		}
		decision, err := runtime.runner.Handle(ctx, input)
		if err != nil {
			return fmt.Errorf("handle strategy target %s/%s/%s/%s: %w", target.Exchange, target.Market, target.Symbol, target.Interval, err)
		}
		logStrategyFailures(target, decision.Failures)
		slog.Info("strategy target evaluated", "target", target, "results", len(decision.Results))
	}
	return nil
}

func logStrategyFailures(target strategy.Target, failures []strategy.StrategyFailure) {
	for _, failure := range failures {
		slog.Error(
			"strategy evaluation failed",
			"target", target,
			"strategy", failure.StrategyName,
			"error", failure.Error,
			"duration_ms", failure.DurationMillis,
		)
	}
}

func targetIntervals(runtime runtimeState, target strategy.Target, configured []string) ([]string, error) {
	required, err := runtime.runner.RequiredIntervals(target)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(required)+len(configured))
	intervals := make([]string, 0, len(required)+len(configured))
	for _, interval := range append(required, configured...) {
		interval = strings.TrimSpace(interval)
		if interval == "" || interval == target.Interval {
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
