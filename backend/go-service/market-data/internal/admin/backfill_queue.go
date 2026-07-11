package admin

import (
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/backfillqueue"
	"alphaflow/go-service/market-data/internal/config"
)

type backfillTask = backfillqueue.Task
type backfillTaskMessage = backfillqueue.Message
type backfillTaskQueue = backfillqueue.Queue
type natsBackfillTaskQueue = backfillqueue.NATSQueue
type natsBackfillTaskQueueOptions = backfillqueue.NATSOptions

const defaultBackfillStream = backfillqueue.DefaultStream

func newNATSBackfillTaskQueue(cfg config.Config) (*natsBackfillTaskQueue, error) {
	ackWait, err := config.BackfillAckWait(cfg)
	if err != nil {
		return nil, err
	}
	return backfillqueue.NewNATS(backfillqueue.NATSOptions{
		URL: cfg.NATS.URL, AckWait: ackWait, MaxDeliveries: cfg.Backfill.MaxDeliveries, MaxPending: cfg.Backfill.MaxPending,
	})
}

func newNATSBackfillTaskQueueWithOptions(options natsBackfillTaskQueueOptions) (*natsBackfillTaskQueue, error) {
	return backfillqueue.NewNATS(options)
}

func newBackfillTask(opts backfillOptions) backfillTask {
	return backfillTask{
		Exchange: opts.exchange, Symbol: opts.symbol, Intervals: append([]string(nil), opts.intervals...),
		Start: opts.start, End: opts.end, Timezone: opts.timezone, Mode: opts.mode,
		Limit: opts.limit, BatchSize: opts.batchSize, Concurrency: opts.concurrency,
		FetchRetries: opts.fetchRetries, WriteRetries: opts.writeRetries, RetryDelay: opts.retryDelay.String(),
		MaxMissingReport: opts.maxMissingReport, WarmupBars: opts.warmupBars,
	}
}

func backfillTaskOptions(task backfillTask) (backfillOptions, error) {
	retryDelay, err := time.ParseDuration(strings.TrimSpace(task.RetryDelay))
	if err != nil {
		return backfillOptions{}, fmt.Errorf("parse backfill task retry_delay: %w", err)
	}
	return backfillOptions{
		exchange: task.Exchange, symbol: task.Symbol, intervals: append([]string(nil), task.Intervals...),
		start: task.Start, end: task.End, timezone: task.Timezone, mode: task.Mode,
		limit: task.Limit, batchSize: task.BatchSize, concurrency: task.Concurrency,
		fetchRetries: task.FetchRetries, writeRetries: task.WriteRetries, retryDelay: retryDelay,
		maxMissingReport: task.MaxMissingReport, warmupBars: task.WarmupBars,
	}, nil
}

func encodeBackfillTask(task backfillTask) ([]byte, error) { return backfillqueue.EncodeTask(task) }
func decodeBackfillTask(payload []byte) (backfillTask, error) {
	return backfillqueue.DecodeTask(payload)
}
func normalizeNATSBackfillTaskQueueOptions(options natsBackfillTaskQueueOptions) natsBackfillTaskQueueOptions {
	return backfillqueue.NormalizeNATSOptions(options)
}
func uniqueBackfillSubjects(subjects ...string) []string {
	return backfillqueue.UniqueSubjects(subjects...)
}
