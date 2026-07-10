package executionbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ID       string
	Intent   execution.OrderIntent
	Delivery int64
	msg      *nats.Msg
}

type ReportMessage struct {
	ID       string
	Report   execution.ExecutionReport
	Delivery int64
	msg      *nats.Msg
}

type Options struct {
	URL, Stream, IntentSubject, ReportSubject, Durable string
	Batch                                              int
	Block, AckWait                                     time.Duration
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
	cfg := &nats.StreamConfig{Name: b.options.Stream, Subjects: []string{b.options.IntentSubject, b.options.ReportSubject}, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, MaxAge: 7 * 24 * time.Hour}
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
		sub, err := b.js.PullSubscribe(b.options.IntentSubject, b.options.Durable, nats.BindStream(b.options.Stream), nats.ManualAck(), nats.AckWait(b.options.AckWait))
		if err != nil {
			return nil, err
		}
		b.intentSub = sub
	}
	items, err := b.intentSub.Fetch(b.options.Batch, nats.Context(ctx), nats.MaxWait(b.options.Block))
	if errors.Is(err, nats.ErrTimeout) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]IntentMessage, 0, len(items))
	for _, item := range items {
		var intent execution.OrderIntent
		if err := json.Unmarshal(item.Data, &intent); err != nil {
			_ = item.Term()
			continue
		}
		meta, _ := item.Metadata()
		delivery := int64(0)
		if meta != nil {
			delivery = int64(meta.NumDelivered)
		}
		result = append(result, IntentMessage{ID: intent.IntentID, Intent: intent, Delivery: delivery, msg: item})
	}
	return result, nil
}

func (b *NATSBus) ReadReports(ctx context.Context, durable string) ([]ReportMessage, error) {
	if b.reportSub == nil {
		if durable == "" {
			durable = "position-engine-execution"
		}
		sub, err := b.js.PullSubscribe(b.options.ReportSubject, durable, nats.BindStream(b.options.Stream), nats.ManualAck(), nats.AckWait(b.options.AckWait))
		if err != nil {
			return nil, err
		}
		b.reportSub = sub
	}
	items, err := b.reportSub.Fetch(b.options.Batch, nats.Context(ctx), nats.MaxWait(b.options.Block))
	if errors.Is(err, nats.ErrTimeout) {
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
		if meta != nil {
			delivery = int64(meta.NumDelivered)
		}
		result = append(result, ReportMessage{ID: report.IntentID, Report: report, Delivery: delivery, msg: item})
	}
	return result, nil
}

func (b *NATSBus) PublishIntent(ctx context.Context, intent execution.OrderIntent) error {
	return b.publish(ctx, b.options.IntentSubject, intent, intent.IntentID)
}
func (b *NATSBus) PublishReport(ctx context.Context, report execution.ExecutionReport) error {
	return b.publish(ctx, b.options.ReportSubject, report, report.IntentID+":"+string(report.Status))
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
