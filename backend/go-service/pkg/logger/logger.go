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
	Service    string
	Level      string
	Format     string
	Output     string
	Dir        string
	Filename   string
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
		Level:       parseLevel(cfg.Level),
		AddSource:   true,
		ReplaceAttr: replaceSourceAttr,
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

	base := slog.New(handler)
	if cfg.Service != "" {
		base = base.With("service", cfg.Service)
	}
	slog.SetDefault(base)
	return nil
}

func replaceSourceAttr(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key != slog.SourceKey {
		return attr
	}
	source, ok := attr.Value.Any().(*slog.Source)
	if !ok || source == nil {
		return attr
	}
	return slog.Group(
		slog.SourceKey,
		slog.String("path", source.File),
		slog.String("file", filepath.Base(source.File)),
		slog.String("function", source.Function),
		slog.Int("line", source.Line),
	)
}

func writer(cfg Config) (io.Writer, error) {
	switch cfg.Output {
	case "", "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "file":
		path, err := filePath(cfg)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		return &lumberjack.Logger{
			Filename:   path,
			MaxSize:    positiveOrDefault(cfg.MaxSizeMB, 100),
			MaxBackups: positiveOrDefault(cfg.MaxBackups, 10),
			MaxAge:     positiveOrDefault(cfg.MaxAgeDays, 30),
			Compress:   cfg.Compress,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported log output %q", cfg.Output)
	}
}

func filePath(cfg Config) (string, error) {
	if cfg.Filename == "" {
		return "", fmt.Errorf("log filename cannot be empty when output is file")
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "logs"
	}
	return filepath.Join(dir, cfg.Filename), nil
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
