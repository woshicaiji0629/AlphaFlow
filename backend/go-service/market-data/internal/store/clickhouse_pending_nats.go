package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	DefaultClickHousePendingStream            = "ALPHAFLOW_MARKET_PENDING"
	DefaultClickHousePendingSubject           = "market.clickhouse.pending.kline"
	DefaultClickHousePendingDurable           = "market-data-clickhouse-pending"
	DefaultClickHousePendingDeadLetterSubject = "market.clickhouse.pending.kline.dead"
)

type NATSPendingQueueOptions struct {
	URL               string
	Stream            string
	Subject           string
	Durable           string
	AckWait           time.Duration
	MaxDeliveries     int
	MaxPending        int64
	DeadLetterSubject string
}

type NATSPendingQueue struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	options NATSPendingQueueOptions
}

func NewNATSPendingQueue(options NATSPendingQueueOptions) (*NATSPendingQueue, error) {
	options = normalizeNATSPendingQueueOptions(options)
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	queue := &NATSPendingQueue{
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

func (q *NATSPendingQueue) Publish(ctx context.Context, payloads [][]byte) error {
	if q == nil || q.js == nil {
		return fmt.Errorf("nats pending queue is nil")
	}
	for _, payload := range payloads {
		if _, err := q.js.PublishMsg(&nats.Msg{
			Subject: q.options.Subject,
			Data:    payload,
		}, nats.Context(ctx)); err != nil {
			return fmt.Errorf("publish clickhouse pending record: %w", err)
		}
	}
	return nil
}

func (q *NATSPendingQueue) Fetch(
	ctx context.Context,
	batch int,
	maxWait time.Duration,
) ([]pendingQueueMessage, error) {
	if q == nil || q.sub == nil {
		return nil, fmt.Errorf("nats pending queue is nil")
	}
	if batch <= 0 {
		batch = 100
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
		return nil, fmt.Errorf("fetch clickhouse pending records: %w", err)
	}
	messages := make([]pendingQueueMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		metadata, err := raw.Metadata()
		if err != nil {
			return nil, fmt.Errorf("read clickhouse pending metadata: %w", err)
		}
		messages = append(messages, pendingQueueMessage{
			ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
			Payload:       raw.Data,
			DeliveryCount: int64(metadata.NumDelivered),
			raw:           raw,
		})
	}
	return messages, nil
}

func (q *NATSPendingQueue) Ack(ctx context.Context, messages []pendingQueueMessage) error {
	if len(messages) == 0 {
		return nil
	}
	for _, message := range messages {
		raw, ok := message.raw.(*nats.Msg)
		if !ok || raw == nil {
			continue
		}
		if err := raw.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack clickhouse pending record %s: %w", message.ID, err)
		}
	}
	return nil
}

func (q *NATSPendingQueue) DeadLetter(ctx context.Context, message pendingQueueMessage, reason string) error {
	if q == nil || q.js == nil {
		return fmt.Errorf("nats pending queue is nil")
	}
	if _, err := q.js.PublishMsg(&nats.Msg{
		Subject: q.options.DeadLetterSubject,
		Data:    message.Payload,
		Header: nats.Header{
			"original_id":    []string{message.ID},
			"reason":         []string{reason},
			"delivery_count": []string{fmt.Sprintf("%d", message.DeliveryCount)},
			"failed_at":      []string{fmt.Sprintf("%d", time.Now().UnixMilli())},
		},
	}, nats.Context(ctx)); err != nil {
		return fmt.Errorf("dead-letter clickhouse pending record %s: %w", message.ID, err)
	}
	return nil
}

func (q *NATSPendingQueue) Close() error {
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

func (q *NATSPendingQueue) ensureStream() error {
	subjects := uniquePendingSubjects(q.options.Subject, q.options.DeadLetterSubject)
	cfg := &nats.StreamConfig{
		Name:      q.options.Stream,
		Subjects:  subjects,
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxMsgs:   q.options.MaxPending,
	}
	if _, err := q.js.StreamInfo(q.options.Stream); err == nil {
		if _, err := q.js.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update nats clickhouse pending stream: %w", err)
		}
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("read nats clickhouse pending stream: %w", err)
	}
	if _, err := q.js.AddStream(cfg); err != nil {
		return fmt.Errorf("create nats clickhouse pending stream: %w", err)
	}
	return nil
}

func (q *NATSPendingQueue) ensureConsumer() error {
	sub, err := q.js.PullSubscribe(
		q.options.Subject,
		q.options.Durable,
		nats.BindStream(q.options.Stream),
		nats.ManualAck(),
		nats.AckWait(q.options.AckWait),
		nats.MaxDeliver(q.options.MaxDeliveries),
	)
	if err != nil {
		return fmt.Errorf("create nats clickhouse pending consumer: %w", err)
	}
	q.sub = sub
	return nil
}

func normalizeNATSPendingQueueOptions(options NATSPendingQueueOptions) NATSPendingQueueOptions {
	options.URL = strings.TrimSpace(options.URL)
	if options.URL == "" {
		options.URL = nats.DefaultURL
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = DefaultClickHousePendingStream
	}
	options.Subject = strings.TrimSpace(options.Subject)
	if options.Subject == "" {
		options.Subject = DefaultClickHousePendingSubject
	}
	options.Durable = strings.TrimSpace(options.Durable)
	if options.Durable == "" {
		options.Durable = DefaultClickHousePendingDurable
	}
	if options.AckWait <= 0 {
		options.AckWait = 30 * time.Second
	}
	if options.MaxDeliveries <= 0 {
		options.MaxDeliveries = 5
	}
	if options.MaxPending <= 0 {
		options.MaxPending = 100000
	}
	options.DeadLetterSubject = strings.TrimSpace(options.DeadLetterSubject)
	if options.DeadLetterSubject == "" {
		options.DeadLetterSubject = DefaultClickHousePendingDeadLetterSubject
	}
	return options
}

func uniquePendingSubjects(subjects ...string) []string {
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
