package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/market-data/internal/app"
)

func main() {
	configPath := flag.String("config", "", "path to market-data config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, *configPath); err != nil {
		slog.Error("market-data failed", "error", err)
		os.Exit(1)
	}
}
