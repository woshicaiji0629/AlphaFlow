package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/control-api/internal/app"
)

func main() {
	configPath := flag.String("config", "", "path to control-api config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, *configPath); err != nil {
		slog.Error("control-api failed", "error", err)
		os.Exit(1)
	}
}
