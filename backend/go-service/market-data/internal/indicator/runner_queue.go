package indicator

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

func (r *Runner) runQueued(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if err := r.options.TaskQueue.Close(); err != nil {
			slog.Error("close indicator task queue failed", "error", err)
		}
	}()

	errCh := make(chan error, r.options.TaskWorkers+1)
	var wg sync.WaitGroup
	for worker := 0; worker < r.options.TaskWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- r.runTaskWorker(ctx)
		}()
	}

	r.enqueueDueJobsWithLogging(ctx)
	ticker := time.NewTicker(r.options.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			wg.Wait()
			return nil
		case err := <-errCh:
			cancel()
			wg.Wait()
			if err == nil || ctx.Err() != nil {
				return nil
			}
			return err
		case <-ticker.C:
			r.enqueueDueJobsWithLogging(ctx)
		}
	}
}

func (r *Runner) runTaskWorker(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		messages, err := r.options.TaskQueue.Fetch(ctx, r.options.TaskBatch, r.options.TaskMaxWait)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if err := r.processTaskMessage(ctx, message); err != nil {
				slog.Error("process indicator task failed", "message_id", message.ID, "error", err)
			}
		}
	}
}

func (r *Runner) processTaskMessage(ctx context.Context, message TaskMessage) error {
	if message.DecodeError != "" {
		if err := r.options.TaskQueue.DeadLetter(ctx, message, message.DecodeError); err != nil {
			return err
		}
		return r.options.TaskQueue.Ack(ctx, []TaskMessage{message})
	}
	rule := Rule{
		Exchange:  message.Task.Exchange,
		Market:    message.Task.Market,
		Symbols:   []string{message.Task.Symbol},
		Intervals: []string{message.Task.Interval},
	}
	err := r.calculateSymbolInterval(ctx, rule, message.Task.Symbol, message.Task.Interval)
	if err == nil {
		return r.options.TaskQueue.Ack(ctx, []TaskMessage{message})
	}
	if message.DeliveryCount >= int64(r.options.TaskMaxDeliveries) {
		if dlqErr := r.options.TaskQueue.DeadLetter(ctx, message, err.Error()); dlqErr != nil {
			return dlqErr
		}
		if ackErr := r.options.TaskQueue.Ack(ctx, []TaskMessage{message}); ackErr != nil {
			return ackErr
		}
		return err
	}
	return err
}

func (r *Runner) enqueueDueJobsWithLogging(ctx context.Context) {
	if err := r.EnqueueDueJobs(ctx); err != nil && ctx.Err() == nil {
		slog.Error("enqueue indicator tasks failed", "error", err)
	}
}

func (r *Runner) EnqueueDueJobs(ctx context.Context) error {
	if r.options.TaskQueue == nil {
		return r.RunOnce(ctx)
	}
	jobs, err := r.dueJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		lastOpenTime, ok, err := r.store.LastOpenTime(ctx, job.rule.Exchange, job.rule.Market, job.symbol, job.interval)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := r.options.TaskQueue.Publish(ctx, Task{
			Exchange:     job.rule.Exchange,
			Market:       job.rule.Market,
			Symbol:       job.symbol,
			Interval:     job.interval,
			LastOpenTime: lastOpenTime,
		}); err != nil {
			return err
		}
	}
	if len(jobs) > 0 {
		slog.Debug("enqueued indicator tasks", "tasks", len(jobs))
	}
	return nil
}
