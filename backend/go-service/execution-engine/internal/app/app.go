package app

import (
	"alphaflow/go-service/execution-engine/internal/config"
	"alphaflow/go-service/pkg/execution"
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
	b, err := executionbus.NewNATSBus(executionbus.Options{URL: cfg.NATS.URL, Stream: cfg.NATS.Stream, IntentSubject: cfg.NATS.IntentSubject, ReportSubject: cfg.NATS.ReportSubject, Durable: cfg.NATS.Durable, Batch: cfg.NATS.Batch, Block: block, AckWait: ackWait})
	if err != nil {
		return err
	}
	defer b.Close()
	redis, err := redisclient.New(ctx, redisclient.Config{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize, MinIdleConns: cfg.Redis.MinIdleConns})
	if err != nil {
		return err
	}
	defer redisclient.Close(redis)
	return run(ctx, b, execution.NewRedisIntentStore(redis, "execution-engine:intent"), execution.NewPaperBroker(cfg.Execution.PaperPrice, func() int64 { return time.Now().UnixMilli() }))
}

func run(ctx context.Context, b bus, store execution.IntentStore, broker execution.Broker) error {
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
			if err := process(ctx, b, store, broker, message); err != nil {
				return err
			}
			if err := b.Ack(ctx, message); err != nil {
				return err
			}
		}
	}
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
	now := time.Now().UnixMilli()
	if err := store.SaveIntent(ctx, execution.IntentRecord{Intent: intent, State: execution.IntentStateSubmitted, UpdatedAt: now}); err != nil {
		return err
	}
	report, err := broker.Execute(ctx, intent)
	if err != nil {
		return err
	}
	state := execution.IntentStateRejected
	if report.Status == execution.ExecutionStatusFilled {
		state = execution.IntentStateFilled
	}
	record = &execution.IntentRecord{Intent: intent, Report: report, State: state, UpdatedAt: report.UpdatedAt}
	if err := store.SaveIntent(ctx, *record); err != nil {
		return err
	}
	if err := b.PublishReport(ctx, report); err != nil {
		return err
	}
	record.State = execution.IntentStateCompleted
	if err := store.SaveIntent(ctx, *record); err != nil {
		return err
	}
	slog.Info("execution intent completed", "intent_id", intent.IntentID, "status", report.Status)
	return nil
}
