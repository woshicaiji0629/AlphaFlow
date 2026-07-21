package forwardlabel

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
)

const (
	defaultStart = "2024-08-01T00:00:00Z"
	defaultEnd   = "2024-11-01T00:00:00Z"
)

func Run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("market-research forward-label", flag.ContinueOnError)
	var (
		exchange = flags.String("exchange", "binance", "exchange name")
		market   = flags.String("market", "um", "market name")
		symbol   = flags.String("symbol", "ETHUSDT", "symbol")
		interval = flags.String("interval", "3m", "kline interval")
		start    = flags.String("start", defaultStart, "sample start in RFC3339")
		end      = flags.String("end", defaultEnd, "sample end in RFC3339")
		output   = flags.String("output", "", "optional JSON output path; stdout when empty")
		addr     = flags.String("clickhouse-addr", envOrDefault("ALPHAFLOW_CLICKHOUSE_ADDR", "localhost:9000"), "ClickHouse address")
		database = flags.String("clickhouse-database", envOrDefault("ALPHAFLOW_CLICKHOUSE_DATABASE", "alphaflow"), "ClickHouse database")
		username = flags.String("clickhouse-username", envOrDefault("ALPHAFLOW_CLICKHOUSE_USERNAME", "alphaflow"), "ClickHouse username")
		password = flags.String("clickhouse-password", envOrDefault("ALPHAFLOW_CLICKHOUSE_PASSWORD", "alphaflow"), "ClickHouse password")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}

	startTime, err := time.Parse(time.RFC3339, strings.TrimSpace(*start))
	if err != nil {
		return fmt.Errorf("parse start: %w", err)
	}
	endTime, err := time.Parse(time.RFC3339, strings.TrimSpace(*end))
	if err != nil {
		return fmt.Errorf("parse end: %w", err)
	}
	intervalMillis, err := marketmodel.IntervalMillis(strings.TrimSpace(*interval))
	if err != nil {
		return err
	}
	maxHorizon := signalresearch.DefaultForwardHorizons[len(signalresearch.DefaultForwardHorizons)-1]

	store, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: *addr, Database: *database, Username: *username, Password: *password,
		DialTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, SkipSchemaInit: true,
	})
	if err != nil {
		return err
	}
	defer store.Close()
	klineReader, err := reader.New(store)
	if err != nil {
		return err
	}
	result, err := klineReader.ReadKlines(ctx, reader.Request{
		Exchange: strings.ToLower(strings.TrimSpace(*exchange)), Market: strings.ToLower(strings.TrimSpace(*market)),
		Symbol: strings.ToUpper(strings.TrimSpace(*symbol)), Interval: strings.TrimSpace(*interval),
		Start: startTime.UnixMilli(), End: endTime.UnixMilli() + int64(maxHorizon)*intervalMillis,
	})
	if err != nil {
		return err
	}
	report, err := signalresearch.BuildForwardDistribution(
		result.Klines, startTime.UnixMilli(), endTime.UnixMilli(), intervalMillis, signalresearch.DefaultForwardHorizons[:],
	)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode distribution report: %w", err)
	}
	encoded = append(encoded, '\n')
	if strings.TrimSpace(*output) == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		return fmt.Errorf("write distribution report: %w", err)
	}
	return nil
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
