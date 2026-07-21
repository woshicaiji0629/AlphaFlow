package datasetcheck

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/backtest-engine/internal/config"
	"alphaflow/go-service/backtest-engine/internal/reader"
	"alphaflow/go-service/pkg/clickhousemarket"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyregistry"
)

func Run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest-engine dataset-check", flag.ContinueOnError)
	configPath := flags.String("config", "", "path to backtest-engine config file")
	startOverride := flags.String("start", "", "override start time (RFC3339)")
	endOverride := flags.String("end", "", "override end time (RFC3339)")
	warmupOverride := flags.Int64("warmup", -1, "override warmup bars")
	jsonOutput := flags.Bool("json", false, "print JSON report")
	missingLimit := flags.Int("missing-limit", 20, "maximum missing timestamps printed per phase")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return run(ctx, *configPath, *startOverride, *endOverride, *warmupOverride, *jsonOutput, *missingLimit)
}

func run(ctx context.Context, configPath string, startOverride string, endOverride string, warmupOverride int64, jsonOutput bool, missingLimit int) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	start, err := overrideTime(startOverride, config.StartTime, cfg)
	if err != nil {
		return fmt.Errorf("start time: %w", err)
	}
	end, err := overrideTime(endOverride, config.EndTime, cfg)
	if err != nil {
		return fmt.Errorf("end time: %w", err)
	}
	if !end.After(start) {
		return fmt.Errorf("end time must be after start time")
	}
	warmup := cfg.Data.WarmupBars
	if warmupOverride >= 0 {
		warmup = warmupOverride
	}
	item, err := strategyregistry.BuildSpec(config.StrategySpec(cfg))
	if err != nil {
		return err
	}
	intervals, err := strategy.NewEngine([]strategy.Strategy{item}).RequiredIntervals(strategy.Target{Interval: cfg.Data.Interval})
	if err != nil {
		return err
	}
	confirmIntervals := mergeConfirmIntervals(cfg.Data.Interval, intervals[1:], cfg.Data.ConfirmIntervals)
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return err
	}
	store, err := clickhousemarket.NewStore(ctx, clickhousemarket.Options{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: dialTimeout, ReadTimeout: readTimeout, SkipSchemaInit: true,
	})
	if err != nil {
		return err
	}
	defer store.Close()
	klineReader, err := reader.New(store)
	if err != nil {
		return err
	}
	report, err := klineReader.CheckDataset(ctx, reader.DatasetRequest{
		Exchange: cfg.Data.Exchange, Market: cfg.Data.Market, Symbols: cfg.Data.Symbols,
		Interval: cfg.Data.Interval, ConfirmIntervals: confirmIntervals,
		Start: start.UnixMilli(), End: end.UnixMilli(), WarmupBars: warmup,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(payload))
		return nil
	}
	printReport(report, missingLimit)
	return nil
}

func overrideTime(value string, fallback func(config.Config) (time.Time, error), cfg config.Config) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return fallback(cfg)
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(value))
}

func mergeConfirmIntervals(entry string, groups ...[]string) []string {
	seen := map[string]struct{}{entry: {}}
	result := []string{}
	for _, group := range groups {
		for _, interval := range group {
			interval = strings.TrimSpace(interval)
			if interval == "" {
				continue
			}
			if _, ok := seen[interval]; ok {
				continue
			}
			seen[interval] = struct{}{}
			result = append(result, interval)
		}
	}
	return result
}

func printReport(report reader.DatasetIntegrity, missingLimit int) {
	fmt.Printf("complete: %v\n", report.Complete)
	for _, item := range report.Series {
		fmt.Printf("%s %s rows=%d unique=%d duplicates=%d missing_warmup=%d missing_trading=%d available_warmup=%d longest_run=%d [%s,%s]\n",
			item.Symbol, item.Interval, item.Rows, item.UniqueRows, len(item.DuplicateOpenTimes),
			len(item.MissingWarmupOpenTimes), len(item.MissingTradingOpenTimes), item.AvailableWarmupBars,
			item.LongestRunBars, formatMillis(item.LongestRunStart), formatMillis(item.LongestRunEnd),
		)
		printMissing("  warmup", item.MissingWarmupOpenTimes, missingLimit)
		printMissing("  trading", item.MissingTradingOpenTimes, missingLimit)
	}
}

func printMissing(label string, values []int64, limit int) {
	if len(values) == 0 || limit <= 0 {
		return
	}
	if limit > len(values) {
		limit = len(values)
	}
	formatted := make([]string, 0, limit)
	for _, value := range values[:limit] {
		formatted = append(formatted, formatMillis(value))
	}
	fmt.Printf("%s missing: %s", label, strings.Join(formatted, ", "))
	if limit < len(values) {
		fmt.Printf(" ... +%d", len(values)-limit)
	}
	fmt.Println()
}

func formatMillis(value int64) string {
	if value == 0 {
		return "-"
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339)
}
