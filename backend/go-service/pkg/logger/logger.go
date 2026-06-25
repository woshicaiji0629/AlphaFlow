package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level      string
	Format     string
	Output     string
	FilePath   string
	AddSource  bool
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

func Setup(cfg Config) error {
	writer, err := writer(cfg)
	if err != nil {
		return err
	}

	options := &slog.HandlerOptions{
		Level:     parseLevel(cfg.Level),
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	switch cfg.Format {
	case "", "json":
		handler = slog.NewJSONHandler(writer, options)
	case "text":
		handler = slog.NewTextHandler(writer, options)
	default:
		return fmt.Errorf("unsupported log format %q", cfg.Format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

func writer(cfg Config) (io.Writer, error) {
	switch cfg.Output {
	case "", "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "file":
		if cfg.FilePath == "" {
			return nil, fmt.Errorf("log file_path cannot be empty when output is file")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.FilePath), 0o755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		return &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    positiveOrDefault(cfg.MaxSizeMB, 100),
			MaxBackups: positiveOrDefault(cfg.MaxBackups, 10),
			MaxAge:     positiveOrDefault(cfg.MaxAgeDays, 30),
			Compress:   cfg.Compress,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported log output %q", cfg.Output)
	}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func positiveOrDefault(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
