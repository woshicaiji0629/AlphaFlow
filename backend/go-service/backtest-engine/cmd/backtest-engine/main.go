package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"alphaflow/go-service/backtest-engine/internal/app"
	"alphaflow/go-service/backtest-engine/internal/datasetcheck"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("backtest-engine failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "dataset-check" {
		return datasetcheck.Run(ctx, args[1:])
	}
	if len(args) > 0 && args[0] != "run" && !strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown subcommand %q: expected run or dataset-check", args[0])
	}
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	flags := flag.NewFlagSet("backtest-engine run", flag.ContinueOnError)
	configPath := flags.String("config", "", "path to backtest-engine config file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return app.Run(ctx, *configPath)
}
