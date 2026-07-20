package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	options, err := parseCommandOptions()
	if err != nil {
		slog.Error("parse Supertrend signal research options failed", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, options); err != nil {
		slog.Error("Supertrend signal research failed", "error", err)
		os.Exit(1)
	}
}
