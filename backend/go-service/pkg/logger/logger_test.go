package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo,
	}

	for input, want := range tests {
		if got := parseLevel(input); got != want {
			t.Fatalf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSetupFileLoggerCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	err := Setup(Config{
		Service:    "test-service",
		Level:      "debug",
		Format:     "json",
		Output:     "file",
		Dir:        dir,
		Filename:   "test.log",
		MaxSizeMB:  1,
		MaxBackups: 1,
		MaxAgeDays: 1,
		Compress:   false,
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
}

func TestSetupRejectsUnsupportedFormat(t *testing.T) {
	err := Setup(Config{Format: "xml"})
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestSetupRejectsFileOutputWithoutFilename(t *testing.T) {
	err := Setup(Config{Output: "file"})
	if err == nil {
		t.Fatal("expected missing filename error")
	}
}

func TestSourceFields(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		AddSource:   true,
		ReplaceAttr: replaceSourceAttr,
	})
	logger := slog.New(handler).With("service", "test-service")

	logger.Info("hello")

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if payload["service"] != "test-service" {
		t.Fatalf("service = %v, want test-service", payload["service"])
	}
	if payload["time"] == "" {
		t.Fatalf("time missing in %#v", payload)
	}
	if payload["level"] != "INFO" {
		t.Fatalf("level = %v, want INFO", payload["level"])
	}
	source, ok := payload["source"].(map[string]any)
	if !ok {
		t.Fatalf("source = %#v, want object", payload["source"])
	}
	for _, key := range []string{"path", "file", "function", "line"} {
		if _, ok := source[key]; !ok {
			t.Fatalf("source.%s missing in %#v", key, source)
		}
	}
}
