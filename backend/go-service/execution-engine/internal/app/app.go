package app

import (
	"alphaflow/go-service/execution-engine/internal/config"
	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionadapters"
	"alphaflow/go-service/pkg/executionbus"
	"alphaflow/go-service/pkg/redisclient"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type bus interface {
	ReadIntents(context.Context) ([]executionbus.IntentMessage, error)
	PublishReport(context.Context, execution.ExecutionReport) error
	Ack(context.Context, executionbus.IntentMessage) error
	DeadLetterIntent(context.Context, executionbus.IntentMessage, string) error
	PublishIntent(context.Context, execution.OrderIntent) error
}

func Run(ctx context.Context, path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	block, err := config.Block(cfg)
	if err != nil {
		return err
	}
	ackWait, err := config.AckWait(cfg)
	if err != nil {
		return err
	}
	b, err := executionbus.NewNATSBus(executionbus.Options{URL: cfg.NATS.URL, Stream: cfg.NATS.Stream, IntentSubject: cfg.NATS.IntentSubject, ReportSubject: cfg.NATS.ReportSubject, DeadLetterSubject: cfg.NATS.DeadLetterSubject, Durable: cfg.NATS.Durable, Batch: cfg.NATS.Batch, MaxDeliver: cfg.NATS.MaxDeliveries, Block: block, AckWait: ackWait})
	if err != nil {
		return err
	}
	defer b.Close()
	redis, err := redisclient.New(ctx, redisclient.Config{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize, MinIdleConns: cfg.Redis.MinIdleConns})
	if err != nil {
		return err
	}
	defer redisclient.Close(redis)
	store := execution.NewRedisIntentStore(redis, "execution-engine:intent")
	if cfg.Execution.Mode == "paper" {
		return run(ctx, b, store, execution.NewPaperBroker(cfg.Execution.PaperPrice, func() int64 { return time.Now().UnixMilli() }), cfg.NATS.MaxDeliveries)
	}
	registry, err := executionadapters.NewDefaultRegistry()
	if err != nil {
		return err
	}
	router := newBrokerRouter()
	runtimes := make([]accountRuntime, 0, len(cfg.Accounts))
	for _, item := range cfg.Accounts {
		if !item.Enabled {
			continue
		}
		account, credential, err := item.Build()
		if err != nil {
			return err
		}
		adapter, err := registry.Build(account, credential)
		if err != nil {
			return err
		}
		if err := adapter.TestConnection(ctx); err != nil {
			return fmt.Errorf("test execution account %s: %w", account.ID, err)
		}
		if err := router.add(account.Exchange, account.ID, adapter); err != nil {
			return err
		}
		runtimes = append(runtimes, accountRuntime{adapter: adapter, symbols: item.Symbols, config: item, account: account})
	}
	handler := privateEventHandler{redis: redis, bus: b, store: store}
	startAccountStates(ctx, runtimes, handler.Handle)
	return runWithFanout(ctx, b, store, trackedBroker{router: router, redis: redis}, newAccountFanout(runtimes, b), cfg.NATS.MaxDeliveries)
}

func run(ctx context.Context, b bus, store execution.IntentStore, broker execution.Broker, maxDeliveries int) error {
	return runWithFanout(ctx, b, store, broker, nil, maxDeliveries)
}
func runWithFanout(ctx context.Context, b bus, store execution.IntentStore, broker execution.Broker, fanout *accountFanout, maxDeliveries int) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		messages, err := b.ReadIntents(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, message := range messages {
			if fanout != nil {
				if message.Intent.Account == "" {
					if err := fanout.Publish(ctx, message.Intent); err != nil {
						return err
					}
					if err := b.Ack(ctx, message); err != nil {
						return err
					}
					continue
				}
				prepared, ok, err := fanout.Prepare(ctx, message.Intent)
				if err != nil {
					if shouldDeadLetter(message, maxDeliveries) {
						if deadErr := b.DeadLetterIntent(ctx, message, err.Error()); deadErr != nil {
							return fmt.Errorf("dead-letter execution intent %s: %w", message.Intent.IntentID, deadErr)
						}
						continue
					}
					slog.Error("prepare account execution intent failed; leaving message unacked for retry", "intent_id", message.Intent.IntentID, "exchange", message.Intent.Exchange, "account", message.Intent.Account, "error", err)
					continue
				}
				if !ok {
					if err := b.Ack(ctx, message); err != nil {
						return err
					}
					continue
				}
				message.Intent = prepared
			}
			if err := process(ctx, b, store, broker, message); err != nil {
				if shouldDeadLetter(message, maxDeliveries) {
					if deadErr := b.DeadLetterIntent(ctx, message, err.Error()); deadErr != nil {
						return fmt.Errorf("dead-letter execution intent %s: %w", message.Intent.IntentID, deadErr)
					}
					slog.Error("execution account intent dead-lettered", "intent_id", message.Intent.IntentID, "delivery", message.Delivery, "error", err)
					continue
				}
				slog.Error("execution account intent failed; leaving message unacked for retry", "intent_id", message.Intent.IntentID, "exchange", message.Intent.Exchange, "account", message.Intent.Account, "error", err)
				continue
			}
			if err := b.Ack(ctx, message); err != nil {
				return err
			}
		}
	}
}

func shouldDeadLetter(message executionbus.IntentMessage, maxDeliveries int) bool {
	return maxDeliveries > 0 && message.Delivery >= int64(maxDeliveries)
}

func process(ctx context.Context, b bus, store execution.IntentStore, broker execution.Broker, message executionbus.IntentMessage) error {
	intent := message.Intent
	if intent.IntentID == "" {
		return fmt.Errorf("intent_id is required")
	}
	record, err := store.GetIntent(ctx, intent.IntentID)
	if err != nil {
		return err
	}
	if record != nil && (record.State == execution.IntentStateFilled || record.State == execution.IntentStateCompleted) {
		if err := b.PublishReport(ctx, record.Report); err != nil {
			return err
		}
		record.State = execution.IntentStateCompleted
		return store.SaveIntent(ctx, *record)
	}
	if record != nil && record.State == execution.IntentStateSubmitted {
		if recoverable, ok := broker.(execution.RecoverableBroker); ok {
			report, found, recoverErr := recoverable.Recover(ctx, intent)
			if recoverErr != nil {
				return recoverErr
			}
			if found {
				record.Report = report
				record.UpdatedAt = report.UpdatedAt
				if report.Status == execution.ExecutionStatusFilled {
					record.State = execution.IntentStateFilled
				}
				if err := store.SaveIntent(ctx, *record); err != nil {
					return err
				}
				return b.PublishReport(ctx, report)
			}
			return fmt.Errorf("submitted intent %s outcome is not visible yet", intent.IntentID)
		}
	}
	now := time.Now().UnixMilli()
	if err := store.SaveIntent(ctx, execution.IntentRecord{Intent: intent, State: execution.IntentStateSubmitted, UpdatedAt: now}); err != nil {
		return err
	}
	report, err := broker.Execute(ctx, intent)
	if err != nil {
		return err
	}
	state := execution.IntentStateSubmitted
	switch report.Status {
	case execution.ExecutionStatusFilled:
		state = execution.IntentStateFilled
	case execution.ExecutionStatusRejected, execution.ExecutionStatusCanceled:
		state = execution.IntentStateRejected
	}
	record = &execution.IntentRecord{Intent: intent, Report: report, State: state, UpdatedAt: report.UpdatedAt}
	if err := store.SaveIntent(ctx, *record); err != nil {
		return err
	}
	if err := b.PublishReport(ctx, report); err != nil {
		return err
	}
	if state == execution.IntentStateFilled || state == execution.IntentStateRejected {
		record.State = execution.IntentStateCompleted
		if err := store.SaveIntent(ctx, *record); err != nil {
			return err
		}
	}
	slog.Info("execution intent completed", "intent_id", intent.IntentID, "status", report.Status)
	return nil
}
