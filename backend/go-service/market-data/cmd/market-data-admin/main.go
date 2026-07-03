package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/market-data/internal/admin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := admin.Execute(ctx); err != nil {
		log.Printf("market-data admin failed: %v", err)
		os.Exit(1)
	}
}
