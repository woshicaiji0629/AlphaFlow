package admin

import (
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/pkg/configutil"
	"github.com/spf13/cobra"
)

type taskConfig struct {
	Exchange         string   `toml:"exchange"`
	Market           string   `toml:"market"`
	Symbol           string   `toml:"symbol"`
	Interval         string   `toml:"interval"`
	Intervals        []string `toml:"intervals"`
	Start            string   `toml:"start"`
	End              string   `toml:"end"`
	Timezone         string   `toml:"timezone"`
	Mode             string   `toml:"mode"`
	Limit            int      `toml:"limit"`
	BatchSize        int      `toml:"batch_size"`
	Concurrency      int      `toml:"concurrency"`
	FetchRetries     int      `toml:"fetch_retries"`
	WriteRetries     int      `toml:"write_retries"`
	RetryDelay       string   `toml:"retry_delay"`
	MaxMissingReport int      `toml:"max_missing_report"`
	WarmupBars       int64    `toml:"warmup_bars"`
}

func addTaskConfigFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "task-config", "", "path to kline task config file")
}

func loadTaskConfig(path string) (taskConfig, error) {
	value := strings.TrimSpace(path)
	if value == "" {
		return taskConfig{}, nil
	}
	var cfg taskConfig
	if err := configutil.DecodeTOMLFileStrict(value, &cfg); err != nil {
		return taskConfig{}, err
	}
	return cfg, nil
}

func applyTaskRangeConfig(cmd *cobra.Command, cfg taskConfig, opts *rangeOptions, rawIntervals *string, warmupBars *int64, maxMissingReport *int) {
	if !cmd.Flags().Changed("exchange") && cfg.Exchange != "" {
		opts.exchange = cfg.Exchange
	}
	if !cmd.Flags().Changed("market") && cfg.Market != "" {
		opts.market = cfg.Market
	}
	if !cmd.Flags().Changed("symbol") && cfg.Symbol != "" {
		opts.symbol = cfg.Symbol
	}
	if !cmd.Flags().Changed("interval") && cfg.Interval != "" {
		opts.interval = cfg.Interval
	}
	if rawIntervals != nil && !cmd.Flags().Changed("intervals") && len(cfg.Intervals) > 0 {
		*rawIntervals = strings.Join(cfg.Intervals, ",")
	}
	if !cmd.Flags().Changed("start") && cfg.Start != "" {
		opts.start = cfg.Start
	}
	if !cmd.Flags().Changed("end") && cfg.End != "" {
		opts.end = cfg.End
	}
	if !cmd.Flags().Changed("timezone") && cfg.Timezone != "" {
		opts.timezone = cfg.Timezone
	}
	if warmupBars != nil && !cmd.Flags().Changed("warmup-bars") {
		*warmupBars = cfg.WarmupBars
	}
	if maxMissingReport != nil && !cmd.Flags().Changed("max-missing-report") && cfg.MaxMissingReport > 0 {
		*maxMissingReport = cfg.MaxMissingReport
	}
}

func applyBackfillTaskConfig(cmd *cobra.Command, cfg taskConfig, opts *backfillOptions, rawIntervals *string) error {
	if !cmd.Flags().Changed("exchange") && cfg.Exchange != "" {
		opts.exchange = cfg.Exchange
	}
	if !cmd.Flags().Changed("symbol") && cfg.Symbol != "" {
		opts.symbol = cfg.Symbol
	}
	if !cmd.Flags().Changed("intervals") && len(cfg.Intervals) > 0 {
		*rawIntervals = strings.Join(cfg.Intervals, ",")
	}
	if !cmd.Flags().Changed("start") && cfg.Start != "" {
		opts.start = cfg.Start
	}
	if !cmd.Flags().Changed("end") && cfg.End != "" {
		opts.end = cfg.End
	}
	if !cmd.Flags().Changed("timezone") && cfg.Timezone != "" {
		opts.timezone = cfg.Timezone
	}
	if !cmd.Flags().Changed("mode") && cfg.Mode != "" {
		opts.mode = cfg.Mode
	}
	if !cmd.Flags().Changed("limit") && cfg.Limit > 0 {
		opts.limit = cfg.Limit
	}
	if !cmd.Flags().Changed("batch-size") && cfg.BatchSize > 0 {
		opts.batchSize = cfg.BatchSize
	}
	if !cmd.Flags().Changed("concurrency") && cfg.Concurrency > 0 {
		opts.concurrency = cfg.Concurrency
	}
	if !cmd.Flags().Changed("fetch-retries") && cfg.FetchRetries > 0 {
		opts.fetchRetries = cfg.FetchRetries
	}
	if !cmd.Flags().Changed("write-retries") && cfg.WriteRetries > 0 {
		opts.writeRetries = cfg.WriteRetries
	}
	if !cmd.Flags().Changed("retry-delay") && cfg.RetryDelay != "" {
		duration, err := time.ParseDuration(cfg.RetryDelay)
		if err != nil {
			return fmt.Errorf("parse task retry_delay: %w", err)
		}
		opts.retryDelay = duration
	}
	if !cmd.Flags().Changed("max-missing-report") && cfg.MaxMissingReport > 0 {
		opts.maxMissingReport = cfg.MaxMissingReport
	}
	if !cmd.Flags().Changed("warmup-bars") {
		opts.warmupBars = cfg.WarmupBars
	}
	return nil
}
