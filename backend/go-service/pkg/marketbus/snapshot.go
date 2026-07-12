package marketbus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

const (
	DefaultNATSURL            = "nats://localhost:4222"
	DefaultSnapshotStreamName = "ALPHAFLOW_MARKET"
	DefaultClosedSubject      = "market.snapshot.closed"
	DefaultRealtimeSubject    = "market.snapshot.realtime"
	DefaultDeadLetterSubject  = "market.snapshot.dead"
	SnapshotTypeClosed        = "closed"
	SnapshotTypeRealtime      = "realtime"
)

type SnapshotTarget struct {
	Exchange string `json:"exchange"`
	Market   string `json:"market"`
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"`
}

type Health struct {
	OK        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

type SnapshotEnvelope struct {
	Type      string                               `json:"type"`
	Target    SnapshotTarget                       `json:"target"`
	Kline     *marketmodel.Kline                   `json:"kline,omitempty"`
	Indicator *marketmodel.IndicatorSnapshot       `json:"indicator,omitempty"`
	Window    *marketmodel.IndicatorWindowSnapshot `json:"window,omitempty"`
	Health    Health                               `json:"health"`
	TraceID   string                               `json:"trace_id,omitempty"`
	CreatedAt int64                                `json:"created_at"`
	ExpiresAt int64                                `json:"expires_at,omitempty"`
}

type SnapshotMessage struct {
	ID            string
	Envelope      SnapshotEnvelope
	DeliveryCount int64
	DecodeError   string
	RawPayload    []byte
}

func NewClosedEnvelope(
	snapshot marketmodel.IndicatorSnapshot,
	window marketmodel.IndicatorWindowSnapshot,
	createdAt int64,
	ttl time.Duration,
) SnapshotEnvelope {
	envelope := SnapshotEnvelope{
		Type: SnapshotTypeClosed,
		Target: SnapshotTarget{
			Exchange: snapshot.Exchange,
			Market:   snapshot.Market,
			Symbol:   snapshot.Symbol,
			Interval: snapshot.Interval,
		},
		Indicator: &snapshot,
		Window:    &window,
		Health: Health{
			OK:        true,
			UpdatedAt: createdAt,
		},
		CreatedAt: createdAt,
	}
	if ttl > 0 {
		envelope.ExpiresAt = createdAt + ttl.Milliseconds()
	}
	envelope.TraceID = NewTraceID(envelope)
	return envelope
}

func NewRealtimeEnvelope(
	snapshot marketmodel.IndicatorRealtimeSnapshot,
	createdAt int64,
	ttl time.Duration,
) SnapshotEnvelope {
	indicator := marketmodel.IndicatorSnapshot{
		Exchange:      snapshot.Exchange,
		Market:        snapshot.Market,
		Symbol:        snapshot.Symbol,
		Interval:      snapshot.Interval,
		OpenTime:      snapshot.OpenTime,
		CloseTime:     snapshot.CloseTime,
		Values:        snapshot.Values,
		NumericValues: snapshot.NumericValues,
		Signals:       snapshot.Signals,
		Feature:       snapshot.Feature,
		UpdatedAt:     snapshot.UpdatedAt,
	}
	envelope := SnapshotEnvelope{
		Type: SnapshotTypeRealtime,
		Target: SnapshotTarget{
			Exchange: snapshot.Exchange,
			Market:   snapshot.Market,
			Symbol:   snapshot.Symbol,
			Interval: snapshot.Interval,
		},
		Kline:     &snapshot.Kline,
		Indicator: &indicator,
		Health: Health{
			OK:        true,
			UpdatedAt: createdAt,
		},
		CreatedAt: createdAt,
	}
	if ttl > 0 {
		envelope.ExpiresAt = createdAt + ttl.Milliseconds()
	}
	envelope.TraceID = NewTraceID(envelope)
	return envelope
}

func EncodeSnapshot(envelope SnapshotEnvelope) (string, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode market snapshot: %w", err)
	}
	return string(payload), nil
}

func DecodeSnapshot(payload string) (SnapshotEnvelope, error) {
	var envelope SnapshotEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("decode market snapshot: %w", err)
	}
	if err := validateEnvelope(envelope); err != nil {
		return SnapshotEnvelope{}, err
	}
	return envelope, nil
}

func SubjectForType(snapshotType string) string {
	if snapshotType == SnapshotTypeRealtime {
		return DefaultRealtimeSubject
	}
	return DefaultClosedSubject
}

func NewTraceID(envelope SnapshotEnvelope) string {
	parts := []string{
		"v1",
		normalize(envelope.Type),
		normalize(envelope.Target.Exchange),
		normalize(envelope.Target.Market),
		normalize(envelope.Target.Symbol),
		normalize(envelope.Target.Interval),
		fmt.Sprintf("%d", envelope.CreatedAt),
	}
	if envelope.Indicator != nil {
		parts = append(parts, fmt.Sprintf("%d", envelope.Indicator.OpenTime))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "mkt_" + hex.EncodeToString(sum[:16])
}

func validateEnvelope(envelope SnapshotEnvelope) error {
	switch envelope.Type {
	case SnapshotTypeClosed:
		if envelope.Window == nil || envelope.Indicator == nil {
			return fmt.Errorf("closed market snapshot requires indicator and window")
		}
	case SnapshotTypeRealtime:
		if envelope.Kline == nil || envelope.Indicator == nil {
			return fmt.Errorf("realtime market snapshot requires kline and indicator")
		}
	default:
		return fmt.Errorf("unsupported market snapshot type %q", envelope.Type)
	}
	if strings.TrimSpace(envelope.Target.Exchange) == "" {
		return fmt.Errorf("market snapshot target exchange is required")
	}
	if strings.TrimSpace(envelope.Target.Market) == "" {
		return fmt.Errorf("market snapshot target market is required")
	}
	if strings.TrimSpace(envelope.Target.Symbol) == "" {
		return fmt.Errorf("market snapshot target symbol is required")
	}
	if strings.TrimSpace(envelope.Target.Interval) == "" {
		return fmt.Errorf("market snapshot target interval is required")
	}
	return nil
}

func normalize[T ~string](value T) string {
	return strings.TrimSpace(strings.ToLower(string(value)))
}
