package executionbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"alphaflow/go-service/pkg/execution"
	"github.com/nats-io/nats.go"
)

const (
	DefaultStream        = "ALPHAFLOW_EXECUTION"
	DefaultIntentSubject = "execution.intent"
	DefaultReportSubject = "execution.report"
)

type IntentMessage struct {
	ID             string
	Intent         execution.OrderIntent
	Delivery       int64
	StreamSequence uint64
	msg            *nats.Msg
}

type ReportMessage struct {
	ID             string
	Report         execution.ExecutionReport
	Delivery       int64
	StreamSequence uint64
	msg            *nats.Msg
}

type Options struct {
	URL, Stream, IntentSubject, ReportSubject, DeadLetterSubject, ReportDeadLetterSubject, Durable string
	Batch, MaxDeliver                                                                              int
	Block, AckWait                                                                                 time.Duration
}

type NATSBus struct {
	conn      *nats.Conn
	js        nats.JetStreamContext
	intentSub *nats.Subscription
	reportSub *nats.Subscription
	options   Options
}

func NewNATSBus(options Options) (*NATSBus, error) {
	if options.URL == "" {
		options.URL = nats.DefaultURL
	}
	if options.Stream == "" {
		options.Stream = DefaultStream
	}
	if options.IntentSubject == "" {
		options.IntentSubject = DefaultIntentSubject
	}
	if options.ReportSubject == "" {
		options.ReportSubject = DefaultReportSubject
	}
	if options.DeadLetterSubject == "" {
		options.DeadLetterSubject = options.IntentSubject + ".dead"
	}
	if options.ReportDeadLetterSubject == "" {
		options.ReportDeadLetterSubject = options.ReportSubject + ".dead"
	}
	if options.Durable == "" {
		options.Durable = "execution-engine"
	}
	if options.Batch <= 0 {
		options.Batch = 10
	}
	if options.Block <= 0 {
		options.Block = time.Second
	}
	if options.AckWait <= 0 {
		options.AckWait = 30 * time.Second
	}
	if options.MaxDeliver <= 0 {
		options.MaxDeliver = 5
	}
	conn, err := nats.Connect(options.URL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, err
	}
	b := &NATSBus{conn: conn, js: js, options: options}
	if err := b.ensure(); err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func (b *NATSBus) ensure() error {
	cfg := &nats.StreamConfig{Name: b.options.Stream, Subjects: []string{b.options.IntentSubject, b.options.ReportSubject, b.options.DeadLetterSubject, b.options.ReportDeadLetterSubject}, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, MaxAge: 7 * 24 * time.Hour}
	if _, err := b.js.StreamInfo(b.options.Stream); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = b.js.AddStream(cfg)
		return err
	} else if err != nil {
		return err
	}
	_, err := b.js.UpdateStream(cfg)
	return err
}

func (b *NATSBus) ReadIntents(ctx context.Context) ([]IntentMessage, error) {
	if b.intentSub == nil {
		if err := b.updateExistingConsumer(b.options.Durable); err != nil {
			return nil, err
		}
		sub, err := b.js.PullSubscribe(b.options.IntentSubject, b.options.Durable, nats.BindStream(b.options.Stream), nats.ManualAck(), nats.AckWait(b.options.AckWait))
		if err != nil {
			return nil, err
		}
		b.intentSub = sub
	}
	fetchCtx, cancel := context.WithTimeout(ctx, b.options.Block)
	defer cancel()
	items, err := b.intentSub.Fetch(b.options.Batch, nats.Context(fetchCtx))
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]IntentMessage, 0, len(items))
	for _, item := range items {
		var intent execution.OrderIntent
		if err := json.Unmarshal(item.Data, &intent); err != nil {
			if deadErr := b.deadLetterMalformedIntent(ctx, item, err); deadErr != nil {
				return nil, deadErr
			}
			continue
		}
		meta, _ := item.Metadata()
		delivery := int64(0)
		sequence := uint64(0)
		if meta != nil {
			delivery = int64(meta.NumDelivered)
			sequence = meta.Sequence.Stream
		}
		result = append(result, IntentMessage{ID: intent.IntentID, Intent: intent, Delivery: delivery, StreamSequence: sequence, msg: item})
	}
	return result, nil
}

func (b *NATSBus) deadLetterMalformedIntent(ctx context.Context, message *nats.Msg, decodeErr error) error {
	meta, _ := message.Metadata()
	if meta == nil {
		return fmt.Errorf("malformed execution intent metadata is unavailable")
	}
	sequence := meta.Sequence.Stream
	payload := struct {
		Payload        []byte `json:"payload"`
		Reason         string `json:"reason"`
		StreamSequence uint64 `json:"stream_sequence"`
	}{Payload: append([]byte(nil), message.Data...), Reason: decodeErr.Error(), StreamSequence: sequence}
	id := "malformed:" + strconv.FormatUint(sequence, 10)
	if err := b.publish(ctx, b.options.DeadLetterSubject, payload, id); err != nil {
		return fmt.Errorf("publish malformed execution intent to dead letter: %w", err)
	}
	if err := message.Term(); err != nil {
		return fmt.Errorf("terminate malformed execution intent: %w", err)
	}
	return nil
}

func (b *NATSBus) ReadReports(ctx context.Context, durable string) ([]ReportMessage, error) {
	if b.reportSub == nil {
		if durable == "" {
			durable = "position-engine-execution"
		}
		if err := b.updateExistingConsumer(durable); err != nil {
			return nil, err
		}
		sub, err := b.js.PullSubscribe(b.options.ReportSubject, durable, nats.BindStream(b.options.Stream), nats.ManualAck(), nats.AckWait(b.options.AckWait))
		if err != nil {
			return nil, err
		}
		b.reportSub = sub
	}
	fetchCtx, cancel := context.WithTimeout(ctx, b.options.Block)
	defer cancel()
	items, err := b.reportSub.Fetch(b.options.Batch, nats.Context(fetchCtx))
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]ReportMessage, 0, len(items))
	for _, item := range items {
		var report execution.ExecutionReport
		if err := json.Unmarshal(item.Data, &report); err != nil {
			_ = item.Term()
			continue
		}
		meta, _ := item.Metadata()
		delivery := int64(0)
		sequence := uint64(0)
		if meta != nil {
			delivery = int64(meta.NumDelivered)
			sequence = meta.Sequence.Stream
		}
		result = append(result, ReportMessage{ID: report.IntentID, Report: report, Delivery: delivery, StreamSequence: sequence, msg: item})
	}
	return result, nil
}

func (b *NATSBus) updateExistingConsumer(durable string) error {
	info, err := b.js.ConsumerInfo(b.options.Stream, durable)
	if errors.Is(err, nats.ErrConsumerNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read execution consumer %s: %w", durable, err)
	}
	cfg := info.Config
	if cfg.AckWait == b.options.AckWait && cfg.MaxDeliver == -1 {
		return nil
	}
	cfg.AckWait = b.options.AckWait
	cfg.MaxDeliver = -1
	if _, err := b.js.UpdateConsumer(b.options.Stream, &cfg); err != nil {
		return fmt.Errorf("update execution consumer %s: %w", durable, err)
	}
	return nil
}

func (b *NATSBus) PublishIntent(ctx context.Context, intent execution.OrderIntent) error {
	return b.publish(ctx, b.options.IntentSubject, intent, intent.IntentID)
}
func (b *NATSBus) PublishReport(ctx context.Context, report execution.ExecutionReport) error {
	return b.publish(ctx, b.options.ReportSubject, report, reportMessageID(report))
}
func reportMessageID(report execution.ExecutionReport) string {
	return report.IntentID + ":" + string(report.Status) + ":" + strconv.FormatInt(report.UpdatedAt, 10) + ":" + strconv.FormatFloat(report.FilledQuantity, 'g', -1, 64)
}
func (b *NATSBus) publish(ctx context.Context, subject string, value any, id string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{nats.MsgIdHdr: []string{id}}}
	_, err = b.js.PublishMsg(msg, nats.Context(ctx))
	return err
}
func (b *NATSBus) Ack(ctx context.Context, message IntentMessage) error {
	return message.msg.Ack(nats.Context(ctx))
}
func (b *NATSBus) AckReport(ctx context.Context, message ReportMessage) error {
	return message.msg.Ack(nats.Context(ctx))
}
func (b *NATSBus) DeadLetterReport(ctx context.Context, message ReportMessage, reason string) error {
	payload := struct {
		Report   execution.ExecutionReport `json:"report"`
		Reason   string                    `json:"reason"`
		Delivery int64                     `json:"delivery"`
	}{Report: message.Report, Reason: reason, Delivery: message.Delivery}
	id := reportMessageID(message.Report) + ":dead"
	if message.Report.IntentID == "" {
		id = "report-sequence:" + strconv.FormatUint(message.StreamSequence, 10) + ":dead"
	}
	if err := b.publish(ctx, b.options.ReportDeadLetterSubject, payload, id); err != nil {
		return err
	}
	return message.msg.Term()
}
func (b *NATSBus) DeadLetterIntent(ctx context.Context, message IntentMessage, reason string) error {
	payload := struct {
		Intent   execution.OrderIntent `json:"intent"`
		Reason   string                `json:"reason"`
		Delivery int64                 `json:"delivery"`
	}{Intent: message.Intent, Reason: reason, Delivery: message.Delivery}
	if err := b.publish(ctx, b.options.DeadLetterSubject, payload, deadLetterMessageID(message)); err != nil {
		return err
	}
	return message.msg.Term()
}
func deadLetterMessageID(message IntentMessage) string {
	if message.Intent.IntentID != "" {
		return message.Intent.IntentID + ":dead"
	}
	return "sequence:" + strconv.FormatUint(message.StreamSequence, 10) + ":dead"
}
func (b *NATSBus) Close() error {
	if b == nil || b.conn == nil {
		return nil
	}
	if b.intentSub != nil {
		_ = b.intentSub.Drain()
	}
	if b.reportSub != nil {
		_ = b.reportSub.Drain()
	}
	b.conn.Close()
	return nil
}
