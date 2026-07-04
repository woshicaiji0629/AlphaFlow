package strategybus

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/strategy"
)

func TestEncodeDecodeDecision(t *testing.T) {
	envelope := DecisionEnvelope{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
			Scope:    strategy.PositionScopePaper,
		},
		Results: []strategy.Result{{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Strategy:   "supertrend",
				Side:       strategy.SignalSideBuy,
				Confidence: 0.9,
				OpenTime:   1000,
			},
		}},
		CreatedAt: 2000,
	}

	payload, err := EncodeDecision(envelope)
	if err != nil {
		t.Fatalf("EncodeDecision() error = %v", err)
	}
	got, err := DecodeDecision(payload)
	if err != nil {
		t.Fatalf("DecodeDecision() error = %v", err)
	}
	if got.Target.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", got.Target.Symbol)
	}
	if got.Results[0].StrategyName != "supertrend" {
		t.Fatalf("strategy = %q, want supertrend", got.Results[0].StrategyName)
	}
}

func TestNewDecisionEnvelopeSetsExpiresAt(t *testing.T) {
	decision := strategy.Decision{
		Target: strategy.Target{
			Symbol: "ETHUSDT",
		},
	}
	envelope := NewDecisionEnvelope(decision, 2_000, 30*time.Second)
	if envelope.ExpiresAt != 32_000 {
		t.Fatalf("expires_at = %d, want 32000", envelope.ExpiresAt)
	}
}

func TestStreamValuesUsesPayloadField(t *testing.T) {
	values, err := StreamValues(DecisionEnvelope{CreatedAt: 2000})
	if err != nil {
		t.Fatalf("StreamValues() error = %v", err)
	}
	if values[StreamPayloadField] == "" {
		t.Fatal("payload field is empty")
	}
}
