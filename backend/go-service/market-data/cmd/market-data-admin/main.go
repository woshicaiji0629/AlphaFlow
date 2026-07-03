package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/market-data/internal/admin"
	"alphaflow/go-service/pkg/logger"
)

func main() {
	setupLogger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := admin.Execute(ctx); err != nil {
		slog.Error("market-data admin failed", "error", err)
		os.Exit(1)
	}
}

func setupLogger() {
	if err := logger.Setup(logger.Config{
		Service: "market-data-admin",
		Level:   "info",
		Format:  "text",
		Output:  "stderr",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		os.Exit(1)
	}
}
