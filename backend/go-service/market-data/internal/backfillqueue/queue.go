package backfillqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const DefaultStream = "ALPHAFLOW_MARKET_BACKFILL"

type Task struct {
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
	Source           string   `json:"source,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

type Message struct {
	ID            string
	Task          Task
	DeliveryCount int64
	DecodeError   string
	RawPayload    []byte
	raw           *nats.Msg
}

type Queue interface {
	Publish(context.Context, Task) (string, error)
	Fetch(context.Context, int, time.Duration) ([]Message, error)
	Ack(context.Context, []Message) error
	DeadLetter(context.Context, Message, string) error
	Close() error
}

type NATSOptions struct {
	URL               string
	Stream            string
	Subject           string
	Durable           string
	AckWait           time.Duration
	MaxDeliveries     int
	MaxPending        int64
	DeadLetterSubject string
}

type NATSQueue struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	options NATSOptions
}

type NATSPublisher struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	options NATSOptions
}

func DefaultTask() Task {
	return Task{Timezone: "UTC", Mode: "skip-existing", Limit: 1000, BatchSize: 1000, Concurrency: 2, FetchRetries: 3, WriteRetries: 3, RetryDelay: time.Second.String(), MaxMissingReport: 200}
}

func TaskID(task Task) string {
	intervals := append([]string(nil), task.Intervals...)
	for index := range intervals {
		intervals[index] = strings.ToLower(strings.TrimSpace(intervals[index]))
	}
	sort.Strings(intervals)
	parts := []string{"v2", strings.ToLower(strings.TrimSpace(task.Exchange)), strings.ToUpper(strings.TrimSpace(task.Symbol)), strings.Join(intervals, ","), strings.TrimSpace(task.Start), strings.TrimSpace(task.End), strings.ToUpper(strings.TrimSpace(task.Timezone)), strings.ToLower(strings.TrimSpace(task.Mode)), strings.ToLower(strings.TrimSpace(task.Source))}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "kbf_" + hex.EncodeToString(sum[:16])
}

func NewNATS(options NATSOptions) (*NATSQueue, error) {
	options = NormalizeNATSOptions(options)
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	queue := &NATSQueue{conn: conn, js: js, options: options}
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

func NewNATSPublisher(options NATSOptions) (*NATSPublisher, error) {
	options = NormalizeNATSOptions(options)
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	publisher := &NATSPublisher{conn: conn, js: js, options: options}
	if err := ensureStream(js, options); err != nil {
		_ = publisher.Close()
		return nil, err
	}
	return publisher, nil
}

func (q *NATSQueue) Publish(ctx context.Context, task Task) (string, error) {
	if q == nil || q.js == nil {
		return "", fmt.Errorf("nats backfill task queue is nil")
	}
	payload, err := EncodeTask(task)
	if err != nil {
		return "", err
	}
	ack, err := q.js.PublishMsg(&nats.Msg{Subject: q.options.Subject, Data: payload, Header: nats.Header{"Nats-Msg-Id": []string{TaskID(task)}}}, nats.Context(ctx))
	if err != nil {
		return "", fmt.Errorf("publish backfill task: %w", err)
	}
	return fmt.Sprintf("%d", ack.Sequence), nil
}

func (p *NATSPublisher) Publish(ctx context.Context, task Task) (string, error) {
	if p == nil || p.js == nil {
		return "", fmt.Errorf("nats backfill publisher is nil")
	}
	payload, err := EncodeTask(task)
	if err != nil {
		return "", err
	}
	ack, err := p.js.PublishMsg(&nats.Msg{Subject: p.options.Subject, Data: payload, Header: nats.Header{"Nats-Msg-Id": []string{TaskID(task)}}}, nats.Context(ctx))
	if err != nil {
		return "", fmt.Errorf("publish backfill task: %w", err)
	}
	return fmt.Sprintf("%d", ack.Sequence), nil
}

func (p *NATSPublisher) Close() error {
	if p == nil || p.conn == nil {
		return nil
	}
	_ = p.conn.Drain()
	p.conn.Close()
	return nil
}

func (q *NATSQueue) Fetch(ctx context.Context, batch int, maxWait time.Duration) ([]Message, error) {
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
	messages := make([]Message, 0, len(rawMessages))
	for _, raw := range rawMessages {
		metadata, err := raw.Metadata()
		if err != nil {
			return nil, fmt.Errorf("read backfill task metadata: %w", err)
		}
		message := Message{ID: fmt.Sprintf("%d", metadata.Sequence.Stream), DeliveryCount: int64(metadata.NumDelivered), raw: raw}
		task, err := DecodeTask(raw.Data)
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

func (q *NATSQueue) Ack(ctx context.Context, messages []Message) error {
	for _, message := range messages {
		if message.raw == nil {
			continue
		}
		if err := message.raw.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack backfill task %s: %w", message.ID, err)
		}
	}
	return nil
}

func (q *NATSQueue) DeadLetter(ctx context.Context, message Message, reason string) error {
	if q == nil || q.js == nil {
		return fmt.Errorf("nats backfill task queue is nil")
	}
	payload := append([]byte(nil), message.RawPayload...)
	if len(payload) == 0 {
		encoded, err := EncodeTask(message.Task)
		if err != nil {
			return err
		}
		payload = encoded
	}
	if _, err := q.js.PublishMsg(&nats.Msg{Subject: q.options.DeadLetterSubject, Data: payload, Header: nats.Header{
		"original_id": []string{message.ID}, "reason": []string{reason},
		"delivery_count": []string{fmt.Sprintf("%d", message.DeliveryCount)},
		"failed_at":      []string{fmt.Sprintf("%d", time.Now().UnixMilli())},
	}}, nats.Context(ctx)); err != nil {
		return fmt.Errorf("dead-letter backfill task %s: %w", message.ID, err)
	}
	return nil
}

func (q *NATSQueue) Close() error {
	if q == nil || q.conn == nil {
		return nil
	}
	if q.sub != nil {
		_ = q.sub.Drain()
	}
	_ = q.conn.Drain()
	q.conn.Close()
	return nil
}

func (q *NATSQueue) ensureStream() error {
	return ensureStream(q.js, q.options)
}

func ensureStream(js nats.JetStreamContext, options NATSOptions) error {
	cfg := &nats.StreamConfig{Name: options.Stream, Subjects: UniqueSubjects(options.Subject, options.DeadLetterSubject), Storage: nats.FileStorage, Retention: nats.LimitsPolicy, MaxMsgs: options.MaxPending}
	if _, err := js.StreamInfo(options.Stream); err == nil {
		if _, err := js.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update nats backfill stream: %w", err)
		}
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("read nats backfill stream: %w", err)
	}
	if _, err := js.AddStream(cfg); err != nil {
		return fmt.Errorf("create nats backfill stream: %w", err)
	}
	return nil
}

func (q *NATSQueue) ensureConsumer() error {
	sub, err := q.js.PullSubscribe(q.options.Subject, q.options.Durable, nats.BindStream(q.options.Stream), nats.ManualAck(), nats.AckWait(q.options.AckWait), nats.MaxDeliver(q.options.MaxDeliveries))
	if err != nil {
		return fmt.Errorf("create nats backfill consumer: %w", err)
	}
	q.sub = sub
	return nil
}

func EncodeTask(task Task) ([]byte, error) {
	payload, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("encode backfill task: %w", err)
	}
	return payload, nil
}

func DecodeTask(payload []byte) (Task, error) {
	var task Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return Task{}, fmt.Errorf("decode backfill task: %w", err)
	}
	return task, nil
}

func NormalizeNATSOptions(options NATSOptions) NATSOptions {
	options.URL = strings.TrimSpace(options.URL)
	if options.URL == "" {
		options.URL = nats.DefaultURL
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = DefaultStream
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

func UniqueSubjects(subjects ...string) []string {
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
