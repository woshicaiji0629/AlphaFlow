package strategybus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisOptions struct {
	Stream           string
	Group            string
	Consumer         string
	Block            time.Duration
	Batch            int64
	PendingIdle      time.Duration
	DeadLetterStream string
	MaxDeliveries    int64
}

type RedisBus struct {
	client  *redis.Client
	options RedisOptions
}

func NewRedisBus(client *redis.Client, options RedisOptions) (*RedisBus, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	options.Stream = strings.TrimSpace(options.Stream)
	if options.Stream == "" {
		options.Stream = DefaultDecisionStream
	}
	options.Group = strings.TrimSpace(options.Group)
	if options.Group == "" {
		return nil, fmt.Errorf("redis stream group cannot be empty")
	}
	options.Consumer = strings.TrimSpace(options.Consumer)
	if options.Consumer == "" {
		return nil, fmt.Errorf("redis stream consumer cannot be empty")
	}
	if options.Batch <= 0 {
		options.Batch = 10
	}
	if options.Block <= 0 {
		options.Block = 5 * time.Second
	}
	if options.PendingIdle <= 0 {
		options.PendingIdle = 30 * time.Second
	}
	options.DeadLetterStream = strings.TrimSpace(options.DeadLetterStream)
	if options.DeadLetterStream == "" {
		options.DeadLetterStream = options.Stream + ":dead"
	}
	if options.MaxDeliveries <= 0 {
		options.MaxDeliveries = 5
	}
	return &RedisBus{client: client, options: options}, nil
}

func (b *RedisBus) EnsureConsumerGroup(ctx context.Context) error {
	if b == nil || b.client == nil {
		return nil
	}
	err := b.client.XGroupCreateMkStream(ctx, b.options.Stream, b.options.Group, "0").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return fmt.Errorf("create redis decision stream group: %w", err)
}

func (b *RedisBus) PublishDecision(ctx context.Context, envelope DecisionEnvelope) (string, error) {
	if b == nil || b.client == nil {
		return "", fmt.Errorf("redis bus is nil")
	}
	values, err := StreamValues(envelope)
	if err != nil {
		return "", err
	}
	id, err := b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: b.options.Stream,
		Values: values,
	}).Result()
	if err != nil {
		return "", fmt.Errorf("publish strategy decision: %w", err)
	}
	return id, nil
}

func (b *RedisBus) ReadDecisions(ctx context.Context) ([]DecisionMessage, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("redis bus is nil")
	}
	streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    b.options.Group,
		Consumer: b.options.Consumer,
		Streams:  []string{b.options.Stream, ">"},
		Count:    b.options.Batch,
		Block:    b.options.Block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read strategy decisions: %w", err)
	}
	messages := []DecisionMessage{}
	for _, stream := range streams {
		for _, raw := range stream.Messages {
			message, err := decodeMessage(raw, 0)
			if err != nil {
				return nil, err
			}
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (b *RedisBus) ClaimPending(ctx context.Context) ([]DecisionMessage, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("redis bus is nil")
	}
	messages, _, err := b.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   b.options.Stream,
		Group:    b.options.Group,
		Consumer: b.options.Consumer,
		MinIdle:  b.options.PendingIdle,
		Start:    "0-0",
		Count:    b.options.Batch,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim pending strategy decisions: %w", err)
	}
	deliveryCounts, err := b.pendingDeliveryCounts(ctx)
	if err != nil {
		return nil, err
	}
	return decodeMessages(messages, deliveryCounts)
}

func (b *RedisBus) DeadLetter(ctx context.Context, message DecisionMessage, reason string) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("redis bus is nil")
	}
	payload, err := EncodeDecision(message.Envelope)
	if err != nil {
		return err
	}
	values := map[string]any{
		StreamPayloadField: payload,
		"original_id":      message.ID,
		"reason":           reason,
		"delivery_count":   message.DeliveryCount,
		"failed_at":        time.Now().UnixMilli(),
	}
	if _, err := b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: b.options.DeadLetterStream,
		Values: values,
	}).Result(); err != nil {
		return fmt.Errorf("dead-letter strategy decision %s: %w", message.ID, err)
	}
	return nil
}

func (b *RedisBus) Ack(ctx context.Context, ids ...string) error {
	if b == nil || b.client == nil || len(ids) == 0 {
		return nil
	}
	if err := b.client.XAck(ctx, b.options.Stream, b.options.Group, ids...).Err(); err != nil {
		return fmt.Errorf("ack strategy decisions: %w", err)
	}
	return nil
}

func (b *RedisBus) pendingDeliveryCounts(ctx context.Context) (map[string]int64, error) {
	items, err := b.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream:   b.options.Stream,
		Group:    b.options.Group,
		Start:    "-",
		End:      "+",
		Count:    b.options.Batch,
		Consumer: b.options.Consumer,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending strategy decision metadata: %w", err)
	}
	counts := make(map[string]int64, len(items))
	for _, item := range items {
		counts[item.ID] = item.RetryCount
	}
	return counts, nil
}

func decodeMessages(messages []redis.XMessage, deliveryCounts map[string]int64) ([]DecisionMessage, error) {
	decoded := make([]DecisionMessage, 0, len(messages))
	for _, message := range messages {
		item, err := decodeMessage(message, deliveryCounts[message.ID])
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, item)
	}
	return decoded, nil
}

func decodeMessage(message redis.XMessage, deliveryCount int64) (DecisionMessage, error) {
	payload, ok := message.Values[StreamPayloadField].(string)
	if !ok || payload == "" {
		return DecisionMessage{}, fmt.Errorf("decision message %s missing payload", message.ID)
	}
	envelope, err := DecodeDecision(payload)
	if err != nil {
		return DecisionMessage{}, fmt.Errorf("decode decision message %s: %w", message.ID, err)
	}
	return DecisionMessage{
		ID:            message.ID,
		Envelope:      envelope,
		DeliveryCount: deliveryCount,
	}, nil
}

type DecisionMessage struct {
	ID            string
	Envelope      DecisionEnvelope
	DeliveryCount int64
}
