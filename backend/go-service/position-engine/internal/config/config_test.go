package config

import (
	"os"
	"path/filepath"
	"testing"

	"alphaflow/go-service/pkg/strategyroute"
)

func TestLoadNormalizesAndValidatesConfig(t *testing.T) {
	path := writeConfig(t, `
[runtime]
service = "position-engine"

[[routes]]
strategy = " supertrend "
sink = "PAPER"
account = "paper-main"
enabled = true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	routes, err := Routes(cfg)
	if err != nil {
		t.Fatalf("Routes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes len = %d, want 1", len(routes))
	}
	if routes[0].StrategyName != "supertrend" {
		t.Fatalf("strategy = %q, want supertrend", routes[0].StrategyName)
	}
	if routes[0].Sink != strategyroute.SinkPaper {
		t.Fatalf("sink = %q, want paper", routes[0].Sink)
	}
	options, err := RedisBusOptions(cfg)
	if err != nil {
		t.Fatalf("RedisBusOptions() error = %v", err)
	}
	if options.Stream == "" || options.Group == "" || options.Consumer == "" {
		t.Fatalf("redis bus options incomplete: %#v", options)
	}
	if options.PendingIdle <= 0 {
		t.Fatalf("pending idle = %s, want positive", options.PendingIdle)
	}
	if options.DeadLetterStream == "" {
		t.Fatal("dead letter stream is empty")
	}
	if options.MaxDeliveries <= 0 {
		t.Fatalf("max deliveries = %d, want positive", options.MaxDeliveries)
	}
	defaultTTL, err := InputDefaultTTL(cfg)
	if err != nil {
		t.Fatalf("InputDefaultTTL() error = %v", err)
	}
	if defaultTTL <= 0 {
		t.Fatalf("default ttl = %s, want positive", defaultTTL)
	}
}

func TestLoadRejectsUnsupportedSink(t *testing.T) {
	path := writeConfig(t, `
[[routes]]
strategy = "supertrend"
sink = "unknown"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unsupported sink error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
unknown = true

[[routes]]
strategy = "supertrend"
sink = "paper"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
