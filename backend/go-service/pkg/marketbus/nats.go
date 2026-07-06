package marketbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type NATSPublisherOptions struct {
	URL             string
	Stream          string
	ClosedSubject   string
	RealtimeSubject string
}

type NATSOptions struct {
	URL               string
	Stream            string
	ClosedSubject     string
	RealtimeSubject   string
	Durable           string
	Block             time.Duration
	Batch             int
	AckWait           time.Duration
	MaxDeliveries     int
	DeadLetterSubject string
}

type NATSPublisher struct {
	conn            *nats.Conn
	js              nats.JetStreamContext
	stream          string
	closedSubject   string
	realtimeSubject string
}

type NATSBus struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	options NATSOptions
	mu      sync.Mutex
	pending map[string]*nats.Msg
}

func NewNATSPublisher(options NATSPublisherOptions) (*NATSPublisher, error) {
	normalized := normalizePublisherOptions(options)
	conn, err := nats.Connect(normalized.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	publisher := &NATSPublisher{
		conn:            conn,
		js:              js,
		stream:          normalized.Stream,
		closedSubject:   normalized.ClosedSubject,
		realtimeSubject: normalized.RealtimeSubject,
	}
	if err := publisher.ensureStream(); err != nil {
		_ = publisher.Close()
		return nil, err
	}
	return publisher, nil
}

func (p *NATSPublisher) PublishSnapshot(ctx context.Context, envelope SnapshotEnvelope) (string, error) {
	if p == nil || p.js == nil {
		return "", fmt.Errorf("nats publisher is nil")
	}
	payload, err := EncodeSnapshot(envelope)
	if err != nil {
		return "", err
	}
	subject := p.closedSubject
	if envelope.Type == SnapshotTypeRealtime {
		subject = p.realtimeSubject
	}
	ack, err := p.js.PublishMsg(&nats.Msg{
		Subject: subject,
		Data:    []byte(payload),
	}, nats.Context(ctx))
	if err != nil {
		return "", fmt.Errorf("publish market snapshot: %w", err)
	}
	return fmt.Sprintf("%d", ack.Sequence), nil
}

func (p *NATSPublisher) Close() error {
	if p == nil || p.conn == nil {
		return nil
	}
	p.conn.Drain()
	p.conn.Close()
	return nil
}

func (p *NATSPublisher) ensureStream() error {
	if p == nil || p.js == nil {
		return fmt.Errorf("nats publisher is nil")
	}
	return ensureStream(p.js, p.stream, p.closedSubject, p.realtimeSubject)
}

func NewNATSBus(options NATSOptions) (*NATSBus, error) {
	normalized := normalizeOptions(options)
	conn, err := nats.Connect(normalized.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	return &NATSBus{
		conn:    conn,
		js:      js,
		options: normalized,
		pending: make(map[string]*nats.Msg),
	}, nil
}

func (b *NATSBus) EnsureConsumer(ctx context.Context) error {
	if b == nil || b.js == nil {
		return nil
	}
	if err := ensureStream(b.js, b.options.Stream, b.options.ClosedSubject, b.options.RealtimeSubject, b.options.DeadLetterSubject); err != nil {
		return err
	}
	subject := commonSubject(b.options.ClosedSubject, b.options.RealtimeSubject)
	sub, err := b.js.PullSubscribe(
		subject,
		b.options.Durable,
		nats.BindStream(b.options.Stream),
		nats.ManualAck(),
		nats.AckWait(b.options.AckWait),
		nats.MaxDeliver(b.options.MaxDeliveries),
		nats.Context(ctx),
	)
	if err != nil {
		return fmt.Errorf("create nats market snapshot consumer: %w", err)
	}
	b.sub = sub
	return nil
}

func (b *NATSBus) ReadSnapshots(ctx context.Context) ([]SnapshotMessage, error) {
	if b == nil || b.js == nil {
		return nil, fmt.Errorf("nats bus is nil")
	}
	if b.sub == nil {
		if err := b.EnsureConsumer(ctx); err != nil {
			return nil, err
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, b.options.Block)
	defer cancel()
	rawMessages, err := b.sub.Fetch(b.options.Batch, nats.Context(fetchCtx))
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read market snapshots: %w", err)
	}
	messages := make([]SnapshotMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		message, err := decodeNATSMessage(raw)
		if err != nil {
			return nil, err
		}
		if message.DecodeError != "" {
			if err := b.DeadLetter(ctx, message, message.DecodeError); err != nil {
				return nil, err
			}
			if err := raw.Ack(nats.Context(ctx)); err != nil {
				return nil, fmt.Errorf("ack invalid market snapshot %s: %w", message.ID, err)
			}
			continue
		}
		b.mu.Lock()
		b.pending[message.ID] = raw
		b.mu.Unlock()
		messages = append(messages, message)
	}
	return messages, nil
}

func (b *NATSBus) DeadLetter(ctx context.Context, message SnapshotMessage, reason string) error {
	if b == nil || b.js == nil {
		return fmt.Errorf("nats bus is nil")
	}
	payload := append([]byte(nil), message.RawPayload...)
	if len(payload) == 0 {
		encoded, err := EncodeSnapshot(message.Envelope)
		if err != nil {
			return err
		}
		payload = []byte(encoded)
	}
	msg := &nats.Msg{
		Subject: b.options.DeadLetterSubject,
		Data:    payload,
		Header: nats.Header{
			"original_id":    []string{message.ID},
			"reason":         []string{reason},
			"delivery_count": []string{fmt.Sprintf("%d", message.DeliveryCount)},
			"failed_at":      []string{fmt.Sprintf("%d", time.Now().UnixMilli())},
		},
	}
	if _, err := b.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
		return fmt.Errorf("dead-letter market snapshot %s: %w", message.ID, err)
	}
	return nil
}

func (b *NATSBus) Ack(ctx context.Context, ids ...string) error {
	if b == nil || len(ids) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		b.mu.Lock()
		msg := b.pending[id]
		delete(b.pending, id)
		b.mu.Unlock()
		if msg == nil {
			continue
		}
		if err := msg.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack market snapshot %s: %w", id, err)
		}
	}
	return nil
}

func (b *NATSBus) Close() error {
	if b == nil || b.conn == nil {
		return nil
	}
	if b.sub != nil {
		_ = b.sub.Drain()
	}
	b.conn.Drain()
	b.conn.Close()
	return nil
}

func normalizePublisherOptions(options NATSPublisherOptions) NATSPublisherOptions {
	options.URL = strings.TrimSpace(options.URL)
	if options.URL == "" {
		options.URL = DefaultNATSURL
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = DefaultSnapshotStreamName
	}
	options.ClosedSubject = strings.TrimSpace(options.ClosedSubject)
	if options.ClosedSubject == "" {
		options.ClosedSubject = DefaultClosedSubject
	}
	options.RealtimeSubject = strings.TrimSpace(options.RealtimeSubject)
	if options.RealtimeSubject == "" {
		options.RealtimeSubject = DefaultRealtimeSubject
	}
	return options
}

func normalizeOptions(options NATSOptions) NATSOptions {
	options.URL = strings.TrimSpace(options.URL)
	if options.URL == "" {
		options.URL = DefaultNATSURL
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = DefaultSnapshotStreamName
	}
	options.ClosedSubject = strings.TrimSpace(options.ClosedSubject)
	if options.ClosedSubject == "" {
		options.ClosedSubject = DefaultClosedSubject
	}
	options.RealtimeSubject = strings.TrimSpace(options.RealtimeSubject)
	if options.RealtimeSubject == "" {
		options.RealtimeSubject = DefaultRealtimeSubject
	}
	options.Durable = strings.TrimSpace(options.Durable)
	if options.Durable == "" {
		options.Durable = "strategy-engine-market"
	}
	if options.Batch <= 0 {
		options.Batch = 32
	}
	if options.Block <= 0 {
		options.Block = time.Second
	}
	if options.AckWait <= 0 {
		options.AckWait = 30 * time.Second
	}
	if options.MaxDeliveries <= 0 {
		options.MaxDeliveries = 5
	}
	options.DeadLetterSubject = strings.TrimSpace(options.DeadLetterSubject)
	if options.DeadLetterSubject == "" {
		options.DeadLetterSubject = DefaultDeadLetterSubject
	}
	return options
}

func ensureStream(js nats.JetStreamContext, stream string, subjects ...string) error {
	if js == nil {
		return fmt.Errorf("nats jetstream context is nil")
	}
	cleanSubjects := uniqueSubjects(subjects...)
	if len(cleanSubjects) == 0 {
		return fmt.Errorf("nats stream subjects cannot be empty")
	}
	cfg := &nats.StreamConfig{
		Name:      stream,
		Subjects:  cleanSubjects,
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
	}
	if _, err := js.StreamInfo(stream); err == nil {
		_, err = js.UpdateStream(cfg)
		if err != nil {
			return fmt.Errorf("update nats market stream: %w", err)
		}
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("read nats market stream: %w", err)
	}
	if _, err := js.AddStream(cfg); err != nil {
		return fmt.Errorf("create nats market stream: %w", err)
	}
	return nil
}

func uniqueSubjects(subjects ...string) []string {
	seen := make(map[string]struct{}, len(subjects))
	clean := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		clean = append(clean, subject)
	}
	return clean
}

func commonSubject(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" || left == right {
		if left != "" {
			return left
		}
		return right
	}
	if strings.HasPrefix(left, "market.snapshot.") && strings.HasPrefix(right, "market.snapshot.") {
		return "market.snapshot.*"
	}
	return left
}

func decodeNATSMessage(message *nats.Msg) (SnapshotMessage, error) {
	if message == nil {
		return SnapshotMessage{}, fmt.Errorf("market snapshot message is nil")
	}
	metadata, err := message.Metadata()
	if err != nil {
		return SnapshotMessage{}, fmt.Errorf("read market snapshot metadata: %w", err)
	}
	envelope, err := DecodeSnapshot(string(message.Data))
	if err != nil {
		return SnapshotMessage{
			ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
			DeliveryCount: int64(metadata.NumDelivered),
			DecodeError:   fmt.Sprintf("decode market snapshot message %d: %v", metadata.Sequence.Stream, err),
			RawPayload:    append([]byte(nil), message.Data...),
		}, nil
	}
	return SnapshotMessage{
		ID:            fmt.Sprintf("%d", metadata.Sequence.Stream),
		Envelope:      envelope,
		DeliveryCount: int64(metadata.NumDelivered),
	}, nil
}
