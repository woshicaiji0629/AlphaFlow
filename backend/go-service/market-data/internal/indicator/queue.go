package indicator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	defaultTaskStream            = "ALPHAFLOW_MARKET_INDICATOR"
	defaultTaskSubject           = "market.indicator.calculate"
	defaultTaskDurable           = "market-data-indicator-worker"
	defaultTaskDeadLetterSubject = "market.indicator.calculate.dead"
)

type TaskQueue interface {
	Publish(ctx context.Context, task Task) (string, error)
	Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]TaskMessage, error)
	Ack(ctx context.Context, messages []TaskMessage) error
	DeadLetter(ctx context.Context, message TaskMessage, reason string) error
	Close() error
}

type Task struct {
	Exchange     string `json:"exchange"`
	Market       string `json:"market"`
	Symbol       string `json:"symbol"`
	Interval     string `json:"interval"`
	LastOpenTime int64  `json:"last_open_time"`
}

type TaskMessage struct {
	ID            string
	Task          Task
	DeliveryCount int64
	DecodeError   string
	RawPayload    []byte
	raw           any
}

type NATSTaskQueueOptions struct {
	URL           string
	AckWait       time.Duration
	MaxDeliveries int
	MaxPending    int64
}

type natsTaskQueue struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	options natsTaskQueueOptions
}

type natsTaskQueueOptions struct {
	URL               string
	Stream            string
	Subject           string
	Durable           string
	AckWait           time.Duration
	MaxDeliveries     int
	MaxPending        int64
	DeadLetterSubject string
}

func NewNATSTaskQueue(options NATSTaskQueueOptions) (TaskQueue, error) {
	return newNATSTaskQueue(natsTaskQueueOptions{
		URL:           options.URL,
		AckWait:       options.AckWait,
		MaxDeliveries: options.MaxDeliveries,
		MaxPending:    options.MaxPending,
	})
}

func newNATSTaskQueue(options natsTaskQueueOptions) (*natsTaskQueue, error) {
	options = normalizeNATSTaskQueueOptions(options)
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	queue := &natsTaskQueue{
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

func (q *natsTaskQueue) Publish(ctx context.Context, task Task) (string, error) {
	if q == nil || q.js == nil {
		return "", fmt.Errorf("nats indicator task queue is nil")
	}
	payload, err := encodeTask(task)
	if err != nil {
		return "", err
	}
	ack, err := q.js.PublishMsg(&nats.Msg{
		Subject: q.options.Subject,
		Data:    payload,
	}, nats.Context(ctx), nats.MsgId(task.ID()))
	if err != nil {
		return "", fmt.Errorf("publish indicator task: %w", err)
	}
	return fmt.Sprintf("%d", ack.Sequence), nil
}

func (q *natsTaskQueue) Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]TaskMessage, error) {
	if q == nil || q.sub == nil {
		return nil, fmt.Errorf("nats indicator task queue is nil")
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
		return nil, fmt.Errorf("fetch indicator tasks: %w", err)
	}
	messages := make([]TaskMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		metadata, err := raw.Metadata()
		if err != nil {
			return nil, fmt.Errorf("read indicator task metadata: %w", err)
		}
		task, err := decodeTask(raw.Data)
		message := TaskMessage{
			ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
			DeliveryCount: int64(metadata.NumDelivered),
			raw:           raw,
		}
		if err != nil {
			message.DecodeError = err.Error()
			message.RawPayload = append([]byte(nil), raw.Data...)
		} else {
			message.Task = task
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (q *natsTaskQueue) Ack(ctx context.Context, messages []TaskMessage) error {
	for _, message := range messages {
		raw, ok := message.raw.(*nats.Msg)
		if !ok || raw == nil {
			continue
		}
		if err := raw.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack indicator task %s: %w", message.ID, err)
		}
	}
	return nil
}

func (q *natsTaskQueue) DeadLetter(ctx context.Context, message TaskMessage, reason string) error {
	if q == nil || q.js == nil {
		return fmt.Errorf("nats indicator task queue is nil")
	}
	payload := append([]byte(nil), message.RawPayload...)
	if len(payload) == 0 {
		encoded, err := encodeTask(message.Task)
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
		return fmt.Errorf("dead-letter indicator task %s: %w", message.ID, err)
	}
	return nil
}

func (q *natsTaskQueue) Close() error {
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

func (q *natsTaskQueue) ensureStream() error {
	cfg := &nats.StreamConfig{
		Name:       q.options.Stream,
		Subjects:   uniqueTaskSubjects(q.options.Subject, q.options.DeadLetterSubject),
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		MaxMsgs:    q.options.MaxPending,
		Duplicates: 10 * time.Minute,
		Discard:    nats.DiscardOld,
	}
	if _, err := q.js.StreamInfo(q.options.Stream); err == nil {
		if _, err := q.js.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update nats indicator stream: %w", err)
		}
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("read nats indicator stream: %w", err)
	}
	if _, err := q.js.AddStream(cfg); err != nil {
		return fmt.Errorf("create nats indicator stream: %w", err)
	}
	return nil
}

func (q *natsTaskQueue) ensureConsumer() error {
	cfg := &nats.ConsumerConfig{
		Durable:       q.options.Durable,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       q.options.AckWait,
		MaxDeliver:    q.options.MaxDeliveries,
		FilterSubject: q.options.Subject,
	}
	if _, err := q.js.ConsumerInfo(q.options.Stream, q.options.Durable); err == nil {
		if _, err := q.js.UpdateConsumer(q.options.Stream, cfg); err != nil {
			return fmt.Errorf("update nats indicator consumer: %w", err)
		}
	} else if errors.Is(err, nats.ErrConsumerNotFound) {
		if _, err := q.js.AddConsumer(q.options.Stream, cfg); err != nil {
			return fmt.Errorf("create nats indicator consumer: %w", err)
		}
	} else {
		return fmt.Errorf("read nats indicator consumer: %w", err)
	}
	sub, err := q.js.PullSubscribe(q.options.Subject, q.options.Durable, nats.Bind(q.options.Stream, q.options.Durable))
	if err != nil {
		return fmt.Errorf("subscribe nats indicator task queue: %w", err)
	}
	q.sub = sub
	return nil
}

func normalizeNATSTaskQueueOptions(options natsTaskQueueOptions) natsTaskQueueOptions {
	if strings.TrimSpace(options.URL) == "" {
		options.URL = "nats://localhost:4222"
	}
	if strings.TrimSpace(options.Stream) == "" {
		options.Stream = defaultTaskStream
	}
	if strings.TrimSpace(options.Subject) == "" {
		options.Subject = defaultTaskSubject
	}
	if strings.TrimSpace(options.Durable) == "" {
		options.Durable = defaultTaskDurable
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
	if strings.TrimSpace(options.DeadLetterSubject) == "" {
		options.DeadLetterSubject = defaultTaskDeadLetterSubject
	}
	return options
}

func encodeTask(task Task) ([]byte, error) {
	payload, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal indicator task: %w", err)
	}
	return payload, nil
}

func decodeTask(payload []byte) (Task, error) {
	var task Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return Task{}, fmt.Errorf("decode indicator task: %w", err)
	}
	if task.Exchange == "" || task.Market == "" || task.Symbol == "" || task.Interval == "" {
		return Task{}, fmt.Errorf("indicator task missing identity")
	}
	return task, nil
}

func (t Task) ID() string {
	return strings.Join([]string{
		t.Exchange,
		t.Market,
		t.Symbol,
		t.Interval,
		fmt.Sprintf("%d", t.LastOpenTime),
	}, "\x00")
}

func uniqueTaskSubjects(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
