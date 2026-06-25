package logger

import (
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
	path := filepath.Join(t.TempDir(), "logs", "test.log")
	err := Setup(Config{
		Level:      "debug",
		Format:     "json",
		Output:     "file",
		FilePath:   path,
		AddSource:  true,
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
