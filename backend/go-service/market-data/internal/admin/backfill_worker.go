package admin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"alphaflow/go-service/market-data/internal/backfillqueue"
	"github.com/spf13/cobra"
)

type backfillWorkerOptions struct {
	batch   int
	maxWait time.Duration
	once    bool
}

type BackfillWorkerOptions struct {
	Batch   int
	MaxWait time.Duration
	Once    bool
}

func RunBackfillWorker(ctx context.Context, configPath string, opts BackfillWorkerOptions) error {
	return runBackfillWorker(ctx, configPath, backfillWorkerOptions{
		batch:   opts.Batch,
		maxWait: opts.MaxWait,
		once:    opts.Once,
	})
}

func newBackfillWorkerCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := backfillWorkerOptions{}
	cmd := &cobra.Command{
		Use:   "backfill-worker",
		Short: "Consume asynchronous historical kline backfill tasks from NATS JetStream",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackfillWorker(ctx, root.configPath, opts)
		},
	}
	cmd.Flags().IntVar(&opts.batch, "batch", 1, "maximum backfill tasks to fetch per poll")
	cmd.Flags().DurationVar(&opts.maxWait, "max-wait", time.Second, "maximum time to wait for tasks per poll")
	cmd.Flags().BoolVar(&opts.once, "once", false, "process one fetch batch and exit")
	return cmd
}

func runBackfillWorker(ctx context.Context, configPath string, opts backfillWorkerOptions) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	queue, err := newNATSBackfillTaskQueue(cfg)
	if err != nil {
		return err
	}
	defer queue.Close()
	for {
		processed, err := processBackfillTaskBatch(ctx, configPath, queue, opts, cfg.Backfill.MaxDeliveries)
		if err != nil {
			return err
		}
		if opts.once {
			return nil
		}
		if processed == 0 {
			if err := sleepContext(ctx, opts.maxWait); err != nil {
				return nil
			}
		}
	}
}

func processBackfillTaskBatch(
	ctx context.Context,
	configPath string,
	queue backfillTaskQueue,
	opts backfillWorkerOptions,
	maxDeliveries int,
) (int, error) {
	if queue == nil {
		return 0, fmt.Errorf("backfill task queue is nil")
	}
	messages, err := queue.Fetch(ctx, opts.batch, opts.maxWait)
	if err != nil {
		return 0, err
	}
	for _, message := range messages {
		if err := processBackfillTaskMessage(ctx, configPath, queue, message, maxDeliveries); err != nil {
			return 0, err
		}
	}
	return len(messages), nil
}

func processBackfillTaskMessage(
	ctx context.Context,
	configPath string,
	queue backfillTaskQueue,
	message backfillTaskMessage,
	maxDeliveries int,
) error {
	startedAt := time.Now()
	if message.DecodeError != "" {
		slog.Warn("backfill task invalid", "event", "backfill_task_dead_lettered", "message_id", message.ID, "failure_type", "decode", "delivery_count", message.DeliveryCount, "duration_ms", time.Since(startedAt).Milliseconds(), "error", message.DecodeError)
		if deadErr := queue.DeadLetter(ctx, message, message.DecodeError); deadErr != nil {
			return deadErr
		}
		return queue.Ack(ctx, []backfillTaskMessage{message})
	}
	taskID := backfillqueue.TaskID(message.Task)
	attrs := backfillTaskLogAttrs(message, taskID)
	slog.Info("backfill task started", append([]any{"event", "backfill_task_started", "duration_ms", int64(0)}, attrs...)...)
	opts, err := backfillTaskOptions(message.Task)
	if err != nil {
		slog.Warn("backfill task invalid", append([]any{"event", "backfill_task_dead_lettered", "failure_type", "validation", "duration_ms", time.Since(startedAt).Milliseconds(), "error", err}, attrs...)...)
		if deadErr := queue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
			return deadErr
		}
		return queue.Ack(ctx, []backfillTaskMessage{message})
	}
	if err := validateBackfillOptions(opts); err != nil {
		slog.Warn("backfill task invalid", append([]any{"event", "backfill_task_dead_lettered", "failure_type", "validation", "duration_ms", time.Since(startedAt).Milliseconds(), "error", err}, attrs...)...)
		if deadErr := queue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
			return deadErr
		}
		return queue.Ack(ctx, []backfillTaskMessage{message})
	}
	if err := runBackfill(ctx, configPath, opts); err != nil {
		if shouldDeadLetterBackfillTask(message, maxDeliveries) {
			slog.Warn("backfill task dead-lettered", append([]any{"event", "backfill_task_dead_lettered", "failure_type", "execution", "duration_ms", time.Since(startedAt).Milliseconds(), "max_deliveries", maxDeliveries, "error", err}, attrs...)...)
			if deadErr := queue.DeadLetter(ctx, message, err.Error()); deadErr != nil {
				return deadErr
			}
			if ackErr := queue.Ack(ctx, []backfillTaskMessage{message}); ackErr != nil {
				return ackErr
			}
			return nil
		}
		slog.Warn("backfill task retrying", append([]any{"event", "backfill_task_retrying", "failure_type", "execution", "duration_ms", time.Since(startedAt).Milliseconds(), "max_deliveries", maxDeliveries, "error", err}, attrs...)...)
		return nil
	}
	slog.Info("backfill task completed", append([]any{"event", "backfill_task_completed", "duration_ms", time.Since(startedAt).Milliseconds()}, attrs...)...)
	return queue.Ack(ctx, []backfillTaskMessage{message})
}

func backfillTaskLogAttrs(message backfillTaskMessage, taskID string) []any {
	task := message.Task
	return []any{
		"message_id", message.ID,
		"task_id", taskID,
		"source", task.Source,
		"reason", task.Reason,
		"exchange", task.Exchange,
		"symbol", task.Symbol,
		"intervals", task.Intervals,
		"start", task.Start,
		"end", task.End,
		"delivery_count", message.DeliveryCount,
	}
}

func shouldDeadLetterBackfillTask(message backfillTaskMessage, maxDeliveries int) bool {
	if maxDeliveries <= 0 {
		return false
	}
	return message.DeliveryCount >= int64(maxDeliveries)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
