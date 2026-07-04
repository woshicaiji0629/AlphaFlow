package strategybus

import (
	"encoding/json"
	"fmt"
	"time"

	"alphaflow/go-service/pkg/strategy"
)

const (
	DefaultDecisionStream = "st:decision:stream"
	StreamPayloadField    = "payload"
)

type DecisionEnvelope struct {
	Target    strategy.Target   `json:"target"`
	Results   []strategy.Result `json:"results"`
	CreatedAt int64             `json:"created_at"`
	ExpiresAt int64             `json:"expires_at"`
}

func NewDecisionEnvelope(decision strategy.Decision, createdAt int64, ttl time.Duration) DecisionEnvelope {
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = createdAt + ttl.Milliseconds()
	}
	return DecisionEnvelope{
		Target:    decision.Target,
		Results:   decision.Results,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
	}
}

func EncodeDecision(envelope DecisionEnvelope) (string, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode strategy decision: %w", err)
	}
	return string(payload), nil
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
