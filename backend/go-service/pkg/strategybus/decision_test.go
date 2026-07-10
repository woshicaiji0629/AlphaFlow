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
	if got.TraceID != envelope.TraceID {
		t.Fatalf("trace id = %q, want %q", got.TraceID, envelope.TraceID)
	}
	if got.SignalID != envelope.SignalID {
		t.Fatalf("signal id = %q, want %q", got.SignalID, envelope.SignalID)
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
	if envelope.TraceID == "" {
		t.Fatal("trace id is empty")
	}
	if envelope.SignalID == "" {
		t.Fatal("signal id is empty")
	}
}

func TestNewSignalIDIsStableAcrossResultOrder(t *testing.T) {
	first := strategy.Decision{
		Target: strategy.Target{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "3m",
		},
		Results: []strategy.Result{
			{
				StrategyName: "supertrend",
				Signal: strategy.Signal{
					Side:     strategy.SignalSideBuy,
					OpenTime: 1000,
				},
			},
			{
				StrategyName: "breakout",
				Signal: strategy.Signal{
					Side:     strategy.SignalSideHold,
					OpenTime: 1000,
				},
			},
		},
	}
	second := strategy.Decision{
		Target:  first.Target,
		Results: []strategy.Result{first.Results[1], first.Results[0]},
	}
	if NewSignalID(first) != NewSignalID(second) {
		t.Fatalf("signal id should be stable across result order: %q != %q", NewSignalID(first), NewSignalID(second))
	}
}

func TestNewSignalIDChangesWithSignalSide(t *testing.T) {
	first := strategy.Decision{
		Target: strategy.Target{Symbol: "ETHUSDT"},
		Results: []strategy.Result{{
			StrategyName: "supertrend",
			Signal: strategy.Signal{
				Side:     strategy.SignalSideBuy,
				OpenTime: 1000,
			},
		}},
	}
	second := first
	second.Results = []strategy.Result{{
		StrategyName: "supertrend",
		Signal: strategy.Signal{
			Side:     strategy.SignalSideSell,
			OpenTime: 1000,
		},
	}}
	if NewSignalID(first) == NewSignalID(second) {
		t.Fatalf("signal id should change when side changes: %q", NewSignalID(first))
	}
}

func TestNewResultSignalIDChangesByResult(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	first := strategy.Result{
		StrategyName: "supertrend",
		Signal: strategy.Signal{
			Side:     strategy.SignalSideBuy,
			OpenTime: 1000,
		},
	}
	second := first
	second.Signal.Side = strategy.SignalSideSell
	if NewResultSignalID(target, first) == NewResultSignalID(target, second) {
		t.Fatalf("result signal id should change when side changes: %q", NewResultSignalID(target, first))
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

func TestDecisionFailuresRoundTripAndOldPayloadCompatibility(t *testing.T) {
	decision := strategy.Decision{
		Target: strategy.Target{Symbol: "ETHUSDT"},
		Failures: []strategy.StrategyFailure{{
			StrategyName:   "broken",
			Error:          "boom",
			DurationMillis: 12,
		}},
	}
	envelope := NewDecisionEnvelope(decision, 2000, time.Minute)
	payload, err := EncodeDecision(envelope)
	if err != nil {
		t.Fatalf("EncodeDecision() error = %v", err)
	}
	decoded, err := DecodeDecision(payload)
	if err != nil {
		t.Fatalf("DecodeDecision() error = %v", err)
	}
	if len(decoded.Failures) != 1 || decoded.Failures[0].StrategyName != "broken" {
		t.Fatalf("failures = %#v", decoded.Failures)
	}
	legacy, err := DecodeDecision(`{"target":{"Symbol":"ETHUSDT"},"results":[],"created_at":1000,"expires_at":2000}`)
	if err != nil {
		t.Fatalf("DecodeDecision(legacy) error = %v", err)
	}
	if len(legacy.Failures) != 0 {
		t.Fatalf("legacy failures = %#v, want empty", legacy.Failures)
	}
}
