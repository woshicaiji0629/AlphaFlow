package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/backtest-engine/internal/research/analysis"
	"alphaflow/go-service/backtest-engine/internal/research/forwardlabel"
	"alphaflow/go-service/backtest-engine/internal/research/marketstructure"
	"alphaflow/go-service/backtest-engine/internal/research/supertrend"
	"alphaflow/go-service/backtest-engine/internal/research/swing"
)

const defaultConfigPath = "configs/supertrend-signal-research.ethusdt-20250801-20251101.toml"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := run(ctx, os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "market research failed:", err)
		os.Exit(1)
	}
	if result == nil {
		return
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode market research report:", err)
		os.Exit(1)
	}
	fmt.Println(string(payload))
}

func run(ctx context.Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("subcommand is required: swing, analysis, forward-label, structure-regime, or supertrend-signal")
	}
	switch args[0] {
	case "swing":
		flags := flag.NewFlagSet("market-research swing", flag.ContinueOnError)
		configPath := flags.String("config", defaultConfigPath, "market data and ClickHouse config")
		minimumMovePoints := flags.Float64("minimum-move-points", 30, "minimum absolute price move")
		reversalPoints := flags.Float64("reversal-points", 10, "absolute reversal used to confirm a pivot")
		if err := flags.Parse(args[1:]); err != nil {
			return nil, err
		}
		return swing.Run(ctx, *configPath, *minimumMovePoints, *reversalPoints)
	case "analysis":
		flags := flag.NewFlagSet("market-research analysis", flag.ContinueOnError)
		configPath := flags.String("config", defaultConfigPath, "market data and ClickHouse config")
		if err := flags.Parse(args[1:]); err != nil {
			return nil, err
		}
		return analysis.Run(ctx, *configPath)
	case "forward-label":
		if err := forwardlabel.Run(ctx, args[1:]); err != nil {
			return nil, err
		}
		return nil, nil
	case "structure-regime":
		if err := marketstructure.Run(ctx, args[1:]); err != nil {
			return nil, err
		}
		return nil, nil
	case "supertrend-signal":
		if err := supertrend.Run(ctx, args[1:]); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown subcommand %q", args[0])
	}
}
