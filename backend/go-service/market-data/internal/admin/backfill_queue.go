package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/config"
	"github.com/nats-io/nats.go"
)

type backfillTask struct {
	Exchange         string   `json:"exchange"`
	Symbol           string   `json:"symbol"`
	Intervals        []string `json:"intervals"`
	Start            string   `json:"start"`
	End              string   `json:"end"`
	Timezone         string   `json:"timezone"`
	Mode             string   `json:"mode"`
	Limit            int      `json:"limit"`
	BatchSize        int      `json:"batch_size"`
	Concurrency      int      `json:"concurrency"`
	FetchRetries     int      `json:"fetch_retries"`
	WriteRetries     int      `json:"write_retries"`
	RetryDelay       string   `json:"retry_delay"`
	MaxMissingReport int      `json:"max_missing_report"`
	WarmupBars       int64    `json:"warmup_bars"`
}

type backfillTaskMessage struct {
	ID            string
	Task          backfillTask
	DeliveryCount int64
	DecodeError   string
	RawPayload    []byte
	raw           any
}

type backfillTaskQueue interface {
	Publish(ctx context.Context, task backfillTask) (string, error)
	Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]backfillTaskMessage, error)
	Ack(ctx context.Context, messages []backfillTaskMessage) error
	DeadLetter(ctx context.Context, message backfillTaskMessage, reason string) error
	Close() error
}

type natsBackfillTaskQueue struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	options natsBackfillTaskQueueOptions
}

type natsBackfillTaskQueueOptions struct {
	URL               string
	Stream            string
	Subject           string
	Durable           string
	AckWait           time.Duration
	MaxDeliveries     int
	MaxPending        int64
	DeadLetterSubject string
}

const defaultBackfillStream = "ALPHAFLOW_MARKET_BACKFILL"

func newNATSBackfillTaskQueue(cfg config.Config) (*natsBackfillTaskQueue, error) {
	ackWait, err := config.BackfillAckWait(cfg)
	if err != nil {
		return nil, err
	}
	return newNATSBackfillTaskQueueWithOptions(natsBackfillTaskQueueOptions{
		URL:           cfg.NATS.URL,
		AckWait:       ackWait,
		MaxDeliveries: cfg.Backfill.MaxDeliveries,
		MaxPending:    cfg.Backfill.MaxPending,
	})
}

func newNATSBackfillTaskQueueWithOptions(options natsBackfillTaskQueueOptions) (*natsBackfillTaskQueue, error) {
	options = normalizeNATSBackfillTaskQueueOptions(options)
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	queue := &natsBackfillTaskQueue{
		conn:    conn,
		js:      js,
		options: options,
	}
	if err := queue.ensureStream(); err != nil {
		_ = queue.Close()
		return nil, err
	}
	if err := queue.ensureConsumer(); err != nil {
		_ = queue.Close()
		return nil, err
	}
	return queue, nil
}

func (q *natsBackfillTaskQueue) Publish(ctx context.Context, task backfillTask) (string, error) {
	if q == nil || q.js == nil {
		return "", fmt.Errorf("nats backfill task queue is nil")
	}
	payload, err := encodeBackfillTask(task)
	if err != nil {
		return "", err
	}
	ack, err := q.js.PublishMsg(&nats.Msg{
		Subject: q.options.Subject,
		Data:    payload,
	}, nats.Context(ctx))
	if err != nil {
		return "", fmt.Errorf("publish backfill task: %w", err)
	}
	return fmt.Sprintf("%d", ack.Sequence), nil
}

func (q *natsBackfillTaskQueue) Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]backfillTaskMessage, error) {
	if q == nil || q.sub == nil {
		return nil, fmt.Errorf("nats backfill task queue is nil")
	}
	if batch <= 0 {
		batch = 1
	}
	if maxWait <= 0 {
		maxWait = time.Second
	}
	fetchCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	rawMessages, err := q.sub.Fetch(batch, nats.Context(fetchCtx))
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch backfill tasks: %w", err)
	}
	messages := make([]backfillTaskMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		metadata, err := raw.Metadata()
		if err != nil {
			return nil, fmt.Errorf("read backfill task metadata: %w", err)
		}
		task, err := decodeBackfillTask(raw.Data)
		if err != nil {
			messages = append(messages, backfillTaskMessage{
				ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
				DeliveryCount: int64(metadata.NumDelivered),
				DecodeError:   err.Error(),
				RawPayload:    append([]byte(nil), raw.Data...),
				raw:           raw,
			})
			continue
		}
		messages = append(messages, backfillTaskMessage{
			ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
			Task:          task,
			DeliveryCount: int64(metadata.NumDelivered),
			raw:           raw,
		})
	}
	return messages, nil
}

func (q *natsBackfillTaskQueue) Ack(ctx context.Context, messages []backfillTaskMessage) error {
	for _, message := range messages {
		raw, ok := message.raw.(*nats.Msg)
		if !ok || raw == nil {
			continue
		}
		if err := raw.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack backfill task %s: %w", message.ID, err)
		}
	}
	return nil
}

func (q *natsBackfillTaskQueue) DeadLetter(ctx context.Context, message backfillTaskMessage, reason string) error {
	if q == nil || q.js == nil {
		return fmt.Errorf("nats backfill task queue is nil")
	}
	payload := append([]byte(nil), message.RawPayload...)
	if len(payload) == 0 {
		encoded, err := encodeBackfillTask(message.Task)
		if err != nil {
			return err
		}
		payload = encoded
	}
	if _, err := q.js.PublishMsg(&nats.Msg{
		Subject: q.options.DeadLetterSubject,
		Data:    payload,
		Header: nats.Header{
			"original_id":    []string{message.ID},
			"reason":         []string{reason},
			"delivery_count": []string{fmt.Sprintf("%d", message.DeliveryCount)},
			"failed_at":      []string{fmt.Sprintf("%d", time.Now().UnixMilli())},
		},
	}, nats.Context(ctx)); err != nil {
		return fmt.Errorf("dead-letter backfill task %s: %w", message.ID, err)
	}
	return nil
}

func (q *natsBackfillTaskQueue) Close() error {
	if q == nil || q.conn == nil {
		return nil
	}
	if q.sub != nil {
		_ = q.sub.Drain()
	}
	q.conn.Drain()
	q.conn.Close()
	return nil
}

func (q *natsBackfillTaskQueue) ensureStream() error {
	subjects := uniqueBackfillSubjects(q.options.Subject, q.options.DeadLetterSubject)
	cfg := &nats.StreamConfig{
		Name:      q.options.Stream,
		Subjects:  subjects,
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxMsgs:   q.options.MaxPending,
	}
	if _, err := q.js.StreamInfo(q.options.Stream); err == nil {
		if _, err := q.js.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update nats backfill stream: %w", err)
		}
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("read nats backfill stream: %w", err)
	}
	if _, err := q.js.AddStream(cfg); err != nil {
		return fmt.Errorf("create nats backfill stream: %w", err)
	}
	return nil
}

func (q *natsBackfillTaskQueue) ensureConsumer() error {
	sub, err := q.js.PullSubscribe(
		q.options.Subject,
		q.options.Durable,
		nats.BindStream(q.options.Stream),
		nats.ManualAck(),
		nats.AckWait(q.options.AckWait),
		nats.MaxDeliver(q.options.MaxDeliveries),
	)
	if err != nil {
		return fmt.Errorf("create nats backfill consumer: %w", err)
	}
	q.sub = sub
	return nil
}

func newBackfillTask(opts backfillOptions) backfillTask {
	return backfillTask{
		Exchange:         opts.exchange,
		Symbol:           opts.symbol,
		Intervals:        append([]string(nil), opts.intervals...),
		Start:            opts.start,
		End:              opts.end,
		Timezone:         opts.timezone,
		Mode:             opts.mode,
		Limit:            opts.limit,
		BatchSize:        opts.batchSize,
		Concurrency:      opts.concurrency,
		FetchRetries:     opts.fetchRetries,
		WriteRetries:     opts.writeRetries,
		RetryDelay:       opts.retryDelay.String(),
		MaxMissingReport: opts.maxMissingReport,
		WarmupBars:       opts.warmupBars,
	}
}

func (t backfillTask) options() (backfillOptions, error) {
	retryDelay, err := time.ParseDuration(strings.TrimSpace(t.RetryDelay))
	if err != nil {
		return backfillOptions{}, fmt.Errorf("parse backfill task retry_delay: %w", err)
	}
	return backfillOptions{
		exchange:         t.Exchange,
		symbol:           t.Symbol,
		intervals:        append([]string(nil), t.Intervals...),
		start:            t.Start,
		end:              t.End,
		timezone:         t.Timezone,
		mode:             t.Mode,
		limit:            t.Limit,
		batchSize:        t.BatchSize,
		concurrency:      t.Concurrency,
		fetchRetries:     t.FetchRetries,
		writeRetries:     t.WriteRetries,
		retryDelay:       retryDelay,
		maxMissingReport: t.MaxMissingReport,
		warmupBars:       t.WarmupBars,
	}, nil
}

func encodeBackfillTask(task backfillTask) ([]byte, error) {
	payload, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("encode backfill task: %w", err)
	}
	return payload, nil
}

func decodeBackfillTask(payload []byte) (backfillTask, error) {
	var task backfillTask
	if err := json.Unmarshal(payload, &task); err != nil {
		return backfillTask{}, fmt.Errorf("decode backfill task: %w", err)
	}
	return task, nil
}

func normalizeNATSBackfillTaskQueueOptions(options natsBackfillTaskQueueOptions) natsBackfillTaskQueueOptions {
	options.URL = strings.TrimSpace(options.URL)
	if options.URL == "" {
		options.URL = nats.DefaultURL
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = defaultBackfillStream
	}
	options.Subject = strings.TrimSpace(options.Subject)
	if options.Subject == "" {
		options.Subject = "market.kline.backfill"
	}
	options.Durable = strings.TrimSpace(options.Durable)
	if options.Durable == "" {
		options.Durable = "market-data-backfill-worker"
	}
	if options.AckWait <= 0 {
		options.AckWait = 30 * time.Minute
	}
	if options.MaxDeliveries <= 0 {
		options.MaxDeliveries = 3
	}
	if options.MaxPending <= 0 {
		options.MaxPending = 10000
	}
	options.DeadLetterSubject = strings.TrimSpace(options.DeadLetterSubject)
	if options.DeadLetterSubject == "" {
		options.DeadLetterSubject = "market.kline.backfill.dead"
	}
	return options
}

func uniqueBackfillSubjects(subjects ...string) []string {
	seen := make(map[string]struct{}, len(subjects))
	unique := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		unique = append(unique, subject)
	}
	return unique
}
