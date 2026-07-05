package strategybus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"alphaflow/go-service/pkg/strategy"
)

const (
	StreamPayloadField = "payload"
)

type DecisionEnvelope struct {
	Target    strategy.Target   `json:"target"`
	Results   []strategy.Result `json:"results"`
	TraceID   string            `json:"trace_id,omitempty"`
	SignalID  string            `json:"signal_id,omitempty"`
	CreatedAt int64             `json:"created_at"`
	ExpiresAt int64             `json:"expires_at"`
}

type DecisionMessage struct {
	ID            string
	Envelope      DecisionEnvelope
	DeliveryCount int64
	DecodeError   string
	RawPayload    []byte
}

func NewDecisionEnvelope(decision strategy.Decision, createdAt int64, ttl time.Duration) DecisionEnvelope {
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = createdAt + ttl.Milliseconds()
	}
	return DecisionEnvelope{
		Target:    decision.Target,
		Results:   decision.Results,
		TraceID:   NewTraceID(decision, createdAt),
		SignalID:  NewSignalID(decision),
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
	}
}

func NewSignalID(decision strategy.Decision) string {
	parts := []string{
		"v1",
		normalize(decision.Target.Exchange),
		normalize(decision.Target.Market),
		normalize(decision.Target.Symbol),
		normalize(decision.Target.Interval),
		normalize(decision.Target.Scope),
		normalize(decision.Target.Account),
		normalize(decision.Target.RunID),
	}
	resultParts := make([]string, 0, len(decision.Results))
	for _, result := range decision.Results {
		resultParts = append(resultParts, resultSignalPart(result))
	}
	sort.Strings(resultParts)
	parts = append(parts, resultParts...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "sig_" + hex.EncodeToString(sum[:16])
}

func NewResultSignalID(target strategy.Target, result strategy.Result) string {
	parts := []string{
		"v1",
		normalize(target.Exchange),
		normalize(target.Market),
		normalize(target.Symbol),
		normalize(target.Interval),
		normalize(target.Scope),
		normalize(target.Account),
		normalize(target.RunID),
		resultSignalPart(result),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "rsig_" + hex.EncodeToString(sum[:16])
}

func NewTraceID(decision strategy.Decision, createdAt int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		NewSignalID(decision),
		fmt.Sprintf("%d", createdAt),
	}, "|")))
	return "trc_" + hex.EncodeToString(sum[:16])
}

func EncodeDecision(envelope DecisionEnvelope) (string, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode strategy decision: %w", err)
	}
	return string(payload), nil
}

func normalize[T ~string](value T) string {
	return strings.TrimSpace(strings.ToLower(string(value)))
}

func resultSignalPart(result strategy.Result) string {
	return strings.Join([]string{
		normalize(result.StrategyName),
		normalize(result.Signal.Strategy),
		normalize(result.Signal.Side),
		fmt.Sprintf("%d", result.Signal.OpenTime),
	}, ":")
}

func DecodeDecision(payload string) (DecisionEnvelope, error) {
	var envelope DecisionEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return DecisionEnvelope{}, fmt.Errorf("decode strategy decision: %w", err)
	}
	return envelope, nil
}

func StreamValues(envelope DecisionEnvelope) (map[string]any, error) {
	payload, err := EncodeDecision(envelope)
	if err != nil {
		return nil, err
	}
	return map[string]any{StreamPayloadField: payload}, nil
}
