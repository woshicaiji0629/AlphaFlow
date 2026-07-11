package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/polymarket-research/internal/app"
)

func main() {
	configPath := flag.String("config", "", "path to polymarket research config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, *configPath); err != nil {
		slog.Error("polymarket research failed", "error", err)
		os.Exit(1)
	}
}
