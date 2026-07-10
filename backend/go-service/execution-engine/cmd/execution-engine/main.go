package main

import (
	"alphaflow/go-service/execution-engine/internal/app"
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	path := flag.String("config", "configs/execution-engine.local.toml", "config path")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, *path); err != nil {
		slog.Error("execution-engine failed", "error", err)
		os.Exit(1)
	}
}
