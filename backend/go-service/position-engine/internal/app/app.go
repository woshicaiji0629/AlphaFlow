package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/idempotency"
	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/pkg/position"
	paperhandler "alphaflow/go-service/pkg/positionhandler/paper"
	"alphaflow/go-service/pkg/redisclient"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategybus"
	"alphaflow/go-service/pkg/strategyroute"
	"alphaflow/go-service/position-engine/internal/config"
)

type decisionReader interface {
	EnsureConsumerGroup(ctx context.Context) error
	ReadDecisions(ctx context.Context) ([]strategybus.DecisionMessage, error)
	ClaimPending(ctx context.Context) ([]strategybus.DecisionMessage, error)
	DeadLetter(ctx context.Context, message strategybus.DecisionMessage, reason string) error
	Ack(ctx context.Context, ids ...string) error
}

type decisionProcessor interface {
	ProcessDecision(ctx context.Context, message strategybus.DecisionMessage) (bool, error)
}

var buildDecisionReader = buildRedisDecisionReader
var buildDecisionProcessor = buildPaperDecisionProcessor
var buildIdempotencyStore = buildRedisIdempotencyStore

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
	routes, err := config.Routes(cfg)
	if err != nil {
		return err
	}
	decisionReader, closeReader, err := buildDecisionReader(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeReader()
	if err := decisionReader.EnsureConsumerGroup(ctx); err != nil {
		return err
	}
	processor, closeProcessor, err := buildDecisionProcessor(ctx, cfg, routes)
	if err != nil {
		return err
	}
	defer closeProcessor()
	idempotencyStore, closeIdempotencyStore, err := buildIdempotencyStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeIdempotencyStore()
	if err := runLoop(ctx, cfg, routes, decisionReader, processor, idempotencyStore); err != nil {
		if ctx.Err() != nil {
			slog.Info("position-engine stopped")
			return nil
		}
		return err
	}
	slog.Info("position-engine stopped")
	return nil
}

type processingStats struct {
	decisions    int
	processed    int
	acked        int
	deadLettered int
}

func runLoop(
	ctx context.Context,
	cfg config.Config,
	routes []strategyroute.Route,
	decisionReader decisionReader,
	processor decisionProcessor,
	idempotencyStore idempotency.Store,
) error {
	enabledRoutes := 0
	for _, route := range routes {
		if route.Enabled {
			enabledRoutes++
		}
	}
	for {
		stats, err := processDecisionBatch(ctx, cfg, decisionReader, processor, idempotencyStore)
		if err != nil {
			return err
		}
		slog.Info(
			"position-engine decisions processed",
			"routes", len(routes),
			"enabled_routes", enabledRoutes,
			"decisions", stats.decisions,
			"processed", stats.processed,
			"acked", stats.acked,
			"dead_lettered", stats.deadLettered,
		)
		if err := ctx.Err(); err != nil {
			return nil
		}
		if stats.decisions == 0 {
			delay, err := idleBackoff(cfg)
			if err != nil {
				return err
			}
			if err := sleepContext(ctx, delay); err != nil {
				return nil
			}
		}
	}
}

func idleBackoff(cfg config.Config) (time.Duration, error) {
	block, err := config.InputBlock(cfg)
	if err != nil {
		return 0, err
	}
	if block > time.Second {
		return time.Second, nil
	}
	return block, nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func processDecisionBatch(
	ctx context.Context,
	cfg config.Config,
	decisionReader decisionReader,
	processor decisionProcessor,
	idempotencyStore idempotency.Store,
) (processingStats, error) {
	messages, err := decisionReader.ReadDecisions(ctx)
	if err != nil {
		return processingStats{}, err
	}
	pendingMessages, err := decisionReader.ClaimPending(ctx)
	if err != nil {
		return processingStats{}, err
	}
	messages = append(messages, pendingMessages...)
	stats := processingStats{
		decisions: len(messages),
	}
	for _, message := range messages {
		result, err := processDecisionMessage(ctx, cfg, decisionReader, processor, idempotencyStore, message)
		if err != nil {
			return processingStats{}, err
		}
		if result.ack {
			if err := decisionReader.Ack(ctx, message.ID); err != nil {
				return processingStats{}, fmt.Errorf("ack decision message %s: %w", message.ID, err)
			}
			stats.acked++
		}
		if result.deadLettered {
			stats.deadLettered++
		}
		stats.processed++
	}
	return stats, nil
}

type messageProcessingResult struct {
	ack          bool
	deadLettered bool
}

func processDecisionMessage(
	ctx context.Context,
	cfg config.Config,
	decisionReader decisionReader,
	processor decisionProcessor,
	idempotencyStore idempotency.Store,
	message strategybus.DecisionMessage,
) (messageProcessingResult, error) {
	if idempotencyStore == nil {
		return processDecisionMessageWithoutIdempotency(ctx, cfg, decisionReader, processor, message)
	}
	if len(message.Envelope.Results) == 0 {
		return processDecisionMessageKey(ctx, cfg, decisionReader, processor, idempotencyStore, message, idempotencyKey(message))
	}

	allResultsDone := true
	for _, result := range message.Envelope.Results {
		singleResultMessage := message
		singleResultMessage.Envelope.Results = []strategy.Result{result}
		key := resultIdempotencyKey(message, result)
		status, err := idempotencyStore.Begin(ctx, key)
		if err != nil {
			return messageProcessingResult{}, err
		}
		switch status {
		case idempotency.StatusCompleted:
			continue
		case idempotency.StatusProcessing:
			slog.Warn("position-engine decision result already processing", "message_id", message.ID, "strategy", result.StrategyName)
			allResultsDone = false
			continue
		}

		shouldAck, err := processor.ProcessDecision(ctx, singleResultMessage)
		if err != nil {
			if shouldDeadLetter(message, cfg.Input.MaxDeliveries) {
				if err := decisionReader.DeadLetter(ctx, message, err.Error()); err != nil {
					return messageProcessingResult{}, fmt.Errorf("dead-letter decision message %s: %w", message.ID, err)
				}
				if err := idempotencyStore.Complete(ctx, key); err != nil {
					return messageProcessingResult{}, err
				}
				return messageProcessingResult{ack: true, deadLettered: true}, nil
			}
			slog.Warn("position-engine decision result processing failed", "message_id", message.ID, "strategy", result.StrategyName, "delivery_count", message.DeliveryCount, "error", err)
			if err := idempotencyStore.Fail(ctx, key); err != nil {
				return messageProcessingResult{}, err
			}
			return messageProcessingResult{ack: false}, nil
		}
		if shouldAck {
			if err := idempotencyStore.Complete(ctx, key); err != nil {
				return messageProcessingResult{}, err
			}
			continue
		}
		if err := idempotencyStore.Fail(ctx, key); err != nil {
			return messageProcessingResult{}, err
		}
		allResultsDone = false
	}
	return messageProcessingResult{ack: allResultsDone}, nil
}

func processDecisionMessageWithoutIdempotency(
	ctx context.Context,
	cfg config.Config,
	decisionReader decisionReader,
	processor decisionProcessor,
	message strategybus.DecisionMessage,
) (messageProcessingResult, error) {
	shouldAck, err := processor.ProcessDecision(ctx, message)
	if err != nil {
		if shouldDeadLetter(message, cfg.Input.MaxDeliveries) {
			if err := decisionReader.DeadLetter(ctx, message, err.Error()); err != nil {
				return messageProcessingResult{}, fmt.Errorf("dead-letter decision message %s: %w", message.ID, err)
			}
			return messageProcessingResult{ack: true, deadLettered: true}, nil
		}
		slog.Warn("position-engine decision processing failed", "message_id", message.ID, "delivery_count", message.DeliveryCount, "error", err)
		return messageProcessingResult{ack: false}, nil
	}
	return messageProcessingResult{ack: shouldAck}, nil
}

func processDecisionMessageKey(
	ctx context.Context,
	cfg config.Config,
	decisionReader decisionReader,
	processor decisionProcessor,
	idempotencyStore idempotency.Store,
	message strategybus.DecisionMessage,
	key string,
) (messageProcessingResult, error) {
	status, err := idempotencyStore.Begin(ctx, key)
	if err != nil {
		return messageProcessingResult{}, err
	}
	switch status {
	case idempotency.StatusCompleted:
		return messageProcessingResult{ack: true}, nil
	case idempotency.StatusProcessing:
		slog.Warn("position-engine decision already processing", "message_id", message.ID)
		return messageProcessingResult{ack: false}, nil
	}
	result, err := processDecisionMessageWithoutIdempotency(ctx, cfg, decisionReader, processor, message)
	if err != nil {
		return messageProcessingResult{}, err
	}
	if result.ack {
		if err := idempotencyStore.Complete(ctx, key); err != nil {
			return messageProcessingResult{}, err
		}
		return result, nil
	}
	if err := idempotencyStore.Fail(ctx, key); err != nil {
		return messageProcessingResult{}, err
	}
	return result, nil
}

func idempotencyKey(message strategybus.DecisionMessage) string {
	if message.Envelope.SignalID != "" {
		return "signal:" + message.Envelope.SignalID
	}
	return "message:" + message.ID
}

func resultIdempotencyKey(message strategybus.DecisionMessage, result strategy.Result) string {
	return "result:" + strategybus.NewResultSignalID(message.Envelope.Target, result)
}

func shouldDeadLetter(message strategybus.DecisionMessage, maxDeliveries int64) bool {
	if maxDeliveries <= 0 {
		return false
	}
	return message.DeliveryCount >= maxDeliveries
}

func buildRedisDecisionReader(ctx context.Context, cfg config.Config) (decisionReader, func(), error) {
	redisClient, err := redisclient.New(ctx, config.RedisClientConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis: %w", err)
	}
	closeReader := func() {
		if err := redisclient.Close(redisClient); err != nil {
			slog.Error("close redis failed", "error", err)
		}
	}
	options, err := config.RedisBusOptions(cfg)
	if err != nil {
		closeReader()
		return nil, nil, err
	}
	bus, err := strategybus.NewRedisBus(redisClient, options)
	if err != nil {
		closeReader()
		return nil, nil, err
	}
	return bus, closeReader, nil
}

func buildRedisIdempotencyStore(ctx context.Context, cfg config.Config) (idempotency.Store, func(), error) {
	redisClient, err := redisclient.New(ctx, config.RedisClientConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis idempotency store: %w", err)
	}
	closeStore := func() {
		if err := redisclient.Close(redisClient); err != nil {
			slog.Error("close idempotency redis failed", "error", err)
		}
	}
	processingTTL, err := config.IdempotencyProcessingTTL(cfg)
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	completedTTL, err := config.IdempotencyCompletedTTL(cfg)
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	store, err := idempotency.NewRedisStore(redisClient, idempotency.RedisOptions{
		Prefix:        cfg.Idempotency.Prefix,
		ProcessingTTL: processingTTL,
		CompletedTTL:  completedTTL,
	})
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	return store, closeStore, nil
}

type paperDecisionProcessor struct {
	dispatcher     *strategyroute.Dispatcher
	positionStore  position.Store
	prices         priceReader
	defaultScope   strategy.PositionScope
	defaultAccount string
	defaultTTL     time.Duration
	now            func() int64
}

func buildPaperDecisionProcessor(ctx context.Context, cfg config.Config, routes []strategyroute.Route) (decisionProcessor, func(), error) {
	redisClient, err := redisclient.New(ctx, config.RedisClientConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis position store: %w", err)
	}
	closeProcessor := func() {
		if err := redisclient.Close(redisClient); err != nil {
			slog.Error("close position redis failed", "error", err)
		}
	}
	positionStore := position.NewRedisStore(redisClient, position.RedisStoreOptions{})
	positionManager := position.NewManager(position.ManagerConfig{
		MaxPositionSize:      cfg.Sizing.MaxPositionSize,
		MarginQuote:          cfg.Sizing.MarginQuote,
		Leverage:             cfg.Sizing.Leverage,
		MinOpenConfidence:    cfg.Sizing.MinOpenConfidence,
		DisableShortExposure: cfg.Sizing.DisableShortExposure,
	})
	now := func() int64 { return time.Now().UnixMilli() }
	defaultTTL, err := config.InputDefaultTTL(cfg)
	if err != nil {
		closeProcessor()
		return nil, nil, err
	}
	paperHandler, err := paperhandler.New(paperhandler.Options{
		PositionManager: positionManager,
		PositionStore:   positionStore,
		Broker:          execution.NewPaperBroker("", now),
		FeeConfig: paperhandler.FeeConfig{
			FeeRate:   cfg.Fee.FeeRate,
			RebatePct: cfg.Fee.RebatePct,
		},
		SizingConfig: paperhandler.SizingConfig{
			MarginQuote: cfg.Sizing.MarginQuote,
			Leverage:    cfg.Sizing.Leverage,
		},
		Now: now,
	})
	if err != nil {
		closeProcessor()
		return nil, nil, err
	}
	dispatcher, err := strategyroute.NewDispatcher(strategyroute.DispatcherOptions{
		Routes: routes,
		Handlers: map[strategyroute.Sink]strategyroute.ResultHandler{
			strategyroute.SinkPaper: paperHandler,
		},
	})
	if err != nil {
		closeProcessor()
		return nil, nil, err
	}
	return &paperDecisionProcessor{
		dispatcher:     dispatcher,
		positionStore:  positionStore,
		prices:         newRedisPriceReader(redisClient),
		defaultScope:   config.PositionScope(cfg),
		defaultAccount: cfg.Position.Account,
		defaultTTL:     defaultTTL,
		now:            now,
	}, closeProcessor, nil
}

func (p *paperDecisionProcessor) ProcessDecision(ctx context.Context, message strategybus.DecisionMessage) (bool, error) {
	decision := strategy.Decision{
		Target:  p.normalizeTarget(message.Envelope.Target),
		Results: message.Envelope.Results,
	}
	input, err := p.contextForDecision(ctx, decision)
	if err != nil {
		return false, err
	}
	if p.envelopeExpired(message.Envelope) {
		shouldAck, err := p.processExpiredDecision(ctx, input, decision)
		if err != nil {
			return false, err
		}
		slog.Warn("position-engine decision expired", "message_id", message.ID, "target", decision.Target, "ack", shouldAck)
		return shouldAck, nil
	}
	if err := p.dispatcher.Dispatch(ctx, input, decision); err != nil {
		return false, err
	}
	return true, nil
}

func (p *paperDecisionProcessor) normalizeTarget(target strategy.Target) strategy.Target {
	if target.Scope == "" {
		target.Scope = p.defaultScope
	}
	if target.Account == "" {
		target.Account = p.defaultAccount
	}
	return target
}

func (p *paperDecisionProcessor) envelopeExpired(envelope strategybus.DecisionEnvelope) bool {
	now := p.now()
	expiresAt := envelope.ExpiresAt
	if expiresAt <= 0 && envelope.CreatedAt > 0 && p.defaultTTL > 0 {
		expiresAt = envelope.CreatedAt + p.defaultTTL.Milliseconds()
	}
	return expiresAt > 0 && now > expiresAt
}

func (p *paperDecisionProcessor) processExpiredDecision(ctx context.Context, input strategy.Context, decision strategy.Decision) (bool, error) {
	riskResults := make([]strategy.Result, 0, len(decision.Results))
	for _, result := range decision.Results {
		currentPosition := input.Positions[result.StrategyName]
		if currentPosition == nil || currentPosition.IsFlat() {
			continue
		}
		if isExpiredExitSignal(currentPosition, result.Signal.Side) {
			riskResults = append(riskResults, expiredRiskRecheckResult(result))
		}
	}
	if len(riskResults) == 0 {
		return true, nil
	}
	if priceFromInput(input) == "" {
		return false, nil
	}
	if err := p.dispatcher.Dispatch(ctx, input, strategy.Decision{
		Target:  decision.Target,
		Results: riskResults,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func expiredRiskRecheckResult(result strategy.Result) strategy.Result {
	result.Signal.Side = strategy.SignalSideHold
	result.Signal.Confidence = 0
	if result.Signal.Reason == "" {
		result.Signal.Reason = "expired_exit_recheck"
	}
	return result
}

func priceFromInput(input strategy.Context) string {
	snapshot, ok := input.Snapshots[input.Target.Interval]
	if !ok {
		return ""
	}
	if snapshot.Price.LastPrice != "" {
		return snapshot.Price.LastPrice
	}
	if snapshot.Price.MarkPrice != "" {
		return snapshot.Price.MarkPrice
	}
	return snapshot.Current.Close
}

func isExpiredExitSignal(currentPosition *strategy.Position, side strategy.SignalSide) bool {
	switch currentPosition.Side {
	case strategy.PositionSideLong:
		return side == strategy.SignalSideSell
	case strategy.PositionSideShort:
		return side == strategy.SignalSideBuy
	default:
		return false
	}
}

func (p *paperDecisionProcessor) contextForDecision(ctx context.Context, decision strategy.Decision) (strategy.Context, error) {
	positions := map[string]*strategy.Position{}
	for _, result := range decision.Results {
		currentPosition, err := p.positionStore.GetPosition(ctx, position.Key{
			Scope:        decision.Target.Scope,
			RunID:        decision.Target.RunID,
			Account:      decision.Target.Account,
			Exchange:     decision.Target.Exchange,
			Market:       decision.Target.Market,
			Symbol:       decision.Target.Symbol,
			StrategyName: result.StrategyName,
			PositionSide: strategy.ExchangePositionSideNet,
		})
		if err != nil {
			return strategy.Context{}, fmt.Errorf("read position for strategy %s: %w", result.StrategyName, err)
		}
		positions[result.StrategyName] = currentPosition
	}
	price, err := p.readPrice(ctx, decision.Target)
	if err != nil {
		return strategy.Context{}, err
	}
	return strategy.Context{
		Target:    decision.Target,
		Snapshots: snapshotsWithPrice(decision.Target, price),
		Positions: positions,
	}, nil
}

func (p *paperDecisionProcessor) readPrice(ctx context.Context, target strategy.Target) (strategy.PriceView, error) {
	if p.prices == nil {
		return strategy.PriceView{}, nil
	}
	price, err := p.prices.ReadPrice(ctx, target)
	if err != nil {
		return strategy.PriceView{}, err
	}
	return price, nil
}

func snapshotsWithPrice(target strategy.Target, price strategy.PriceView) map[string]strategy.Snapshot {
	if target.Interval == "" && price.LastPrice == "" && price.MarkPrice == "" {
		return map[string]strategy.Snapshot{}
	}
	return map[string]strategy.Snapshot{
		target.Interval: {
			Target: target,
			Price:  price,
		},
	}
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
