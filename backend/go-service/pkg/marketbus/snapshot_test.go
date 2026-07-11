package marketbus

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

func TestClosedEnvelopeRoundTrip(t *testing.T) {
	envelope := NewClosedEnvelope(
		marketmodel.IndicatorSnapshot{
			Exchange:  "binance",
			Market:    "um",
			Symbol:    "ETHUSDT",
			Interval:  "3m",
			OpenTime:  1000,
			CloseTime: 2000,
			Values:    map[string]string{"ema7": "100"},
			UpdatedAt: 3000,
		},
		marketmodel.IndicatorWindowSnapshot{
			Exchange:  "binance",
			Market:    "um",
			Symbol:    "ETHUSDT",
			Interval:  "3m",
			OpenTime:  1000,
			CloseTime: 2000,
			Version:   "v1",
			Values:    map[string]string{"sample_count": "20"},
			UpdatedAt: 3000,
		},
		4000,
		30*time.Second,
	)

	payload, err := EncodeSnapshot(envelope)
	if err != nil {
		t.Fatalf("EncodeSnapshot() error = %v", err)
	}
	got, err := DecodeSnapshot(payload)
	if err != nil {
		t.Fatalf("DecodeSnapshot() error = %v", err)
	}
	if got.Type != SnapshotTypeClosed {
		t.Fatalf("type = %q, want %q", got.Type, SnapshotTypeClosed)
	}
	if got.Target.Symbol != "ETHUSDT" {
		t.Fatalf("symbol = %q, want ETHUSDT", got.Target.Symbol)
	}
	if got.Window == nil || got.Window.Values["sample_count"] != "20" {
		t.Fatalf("window not decoded: %#v", got.Window)
	}
	if got.ExpiresAt != 34000 {
		t.Fatalf("expires_at = %d, want 34000", got.ExpiresAt)
	}
	if got.TraceID == "" {
		t.Fatal("trace id is empty")
	}
}

func TestRealtimeEnvelopeRoundTrip(t *testing.T) {
	envelope := NewRealtimeEnvelope(
		marketmodel.IndicatorRealtimeSnapshot{
			Exchange:  "binance",
			Market:    "um",
			Symbol:    "ETHUSDT",
			Interval:  "3m",
			OpenTime:  2000,
			CloseTime: 3000,
			Kline: marketmodel.Kline{
				Exchange: "binance",
				Market:   "um",
				Symbol:   "ETHUSDT",
				Interval: "3m",
				Close:    "101",
			},
			Values: map[string]string{"last_price": "101"},
			Feature: marketmodel.FeatureMetadata{
				SchemaVersion:     "indicators.v1",
				CalculatorVersion: "go-indicator.v1",
				ParameterHash:     "params",
			},
			UpdatedAt: 3500,
		},
		4000,
		10*time.Second,
	)

	payload, err := EncodeSnapshot(envelope)
	if err != nil {
		t.Fatalf("EncodeSnapshot() error = %v", err)
	}
	got, err := DecodeSnapshot(payload)
	if err != nil {
		t.Fatalf("DecodeSnapshot() error = %v", err)
	}
	if got.Type != SnapshotTypeRealtime {
		t.Fatalf("type = %q, want %q", got.Type, SnapshotTypeRealtime)
	}
	if got.Kline == nil || got.Kline.Close != "101" {
		t.Fatalf("kline not decoded: %#v", got.Kline)
	}
	if got.Indicator == nil || got.Indicator.Values["last_price"] != "101" {
		t.Fatalf("indicator not decoded: %#v", got.Indicator)
	}
	if got.Indicator.Feature.SchemaVersion != "indicators.v1" {
		t.Fatalf("feature schema version = %q, want indicators.v1", got.Indicator.Feature.SchemaVersion)
	}
}

func TestDecodeSnapshotRejectsIncompleteRealtime(t *testing.T) {
	_, err := DecodeSnapshot(`{"type":"realtime","target":{"exchange":"binance","market":"um","symbol":"ETHUSDT","interval":"3m"}}`)
	if err == nil {
		t.Fatal("DecodeSnapshot() error = nil, want error")
	}
}

func TestSubjectForType(t *testing.T) {
	if got := SubjectForType(SnapshotTypeRealtime); got != DefaultRealtimeSubject {
		t.Fatalf("realtime subject = %q, want %q", got, DefaultRealtimeSubject)
	}
	if got := SubjectForType(SnapshotTypeClosed); got != DefaultClosedSubject {
		t.Fatalf("closed subject = %q, want %q", got, DefaultClosedSubject)
	}
}

func TestMarketStreamConfigExpiresMessages(t *testing.T) {
	config := marketStreamConfig("MARKET", []string{"market.snapshot.*"})
	if config.MaxAge != time.Hour {
		t.Fatalf("max age = %s, want %s", config.MaxAge, time.Hour)
	}
}
