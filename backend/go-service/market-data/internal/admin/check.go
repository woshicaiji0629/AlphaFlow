package admin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	rangeOptions
	intervals        []string
	maxMissingReport int
	warmupBars       int64
}

func newCheckCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := checkOptions{}
	var rawIntervals string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check kline completeness for one exchange, market, symbol, interval list, and date range",
		Long: strings.TrimSpace(`
Check whether market_klines contains every expected kline open_time for one
exchange, market, symbol, interval, and time range. Use --interval for one
interval or --intervals for a comma-separated interval list.

The check uses kline open_time with left-closed, right-open semantics:

  start <= open_time < end

The command validates K line completeness only. Indicator history is treated as
a derived cache and is not required for backtesting; indicators can be computed
from complete K lines at backtest time.

If any kline is missing, the command prints missing open_time values and keeps
the command successful so batch checks can continue across intervals.
`),
		Example: "market-data-admin check --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202606010000 --end 202607010000\n" +
			"market-data-admin check --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202606010000 --end 202607010000 --warmup-bars 300 --max-missing-report 20",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizeRangeOptions(&opts.rangeOptions)
			opts.intervals = parseList(rawIntervals)
			return runCheck(ctx, root.configPath, opts)
		},
	}
	addRangeFlags(cmd, &opts.rangeOptions)
	cmd.Flags().StringVar(&rawIntervals, "intervals", "", "comma-separated intervals, for example 1m,5m,1h")
	cmd.Flags().IntVar(&opts.maxMissingReport, "max-missing-report", 200, "maximum missing klines to log")
	addWarmupFlag(cmd, &opts.warmupBars)
	return cmd
}

func runCheck(ctx context.Context, configPath string, opts checkOptions) error {
	if err := validateCheckOptions(&opts); err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	store, err := newAdminStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	start, end, err := timeRange(opts.start, opts.end, opts.timezone)
	if err != nil {
		return err
	}
	for _, interval := range opts.intervals {
		intervalMillis, err := marketmodel.IntervalMillis(interval)
		if err != nil {
			return err
		}
		checkRange, err := effectiveWarmupRange(start, end, interval, opts.warmupBars)
		if err != nil {
			return err
		}
		if err := checkIntegrity(ctx, store, opts.exchange, opts.market, opts.symbol, interval, checkRange.EffectiveStart, checkRange.End, intervalMillis, opts.timezone, opts.maxMissingReport, false, checkRange); err != nil {
			return err
		}
	}
	return nil
}

func validateCheckOptions(opts *checkOptions) error {
	if opts.exchange == "" {
		return fmt.Errorf("exchange is required")
	}
	if opts.market == "" {
		return fmt.Errorf("market is required")
	}
	if opts.symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if opts.interval != "" && len(opts.intervals) > 0 {
		return fmt.Errorf("interval and intervals cannot be used together")
	}
	if opts.interval != "" {
		opts.intervals = []string{opts.interval}
	}
	if len(opts.intervals) == 0 {
		return fmt.Errorf("interval or intervals is required")
	}
	for _, interval := range opts.intervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return err
		}
	}
	if opts.start == "" {
		return fmt.Errorf("start is required")
	}
	if opts.end == "" {
		return fmt.Errorf("end is required")
	}
	if _, _, err := timeRange(opts.start, opts.end, opts.timezone); err != nil {
		return err
	}
	if opts.maxMissingReport < 0 {
		return fmt.Errorf("max-missing-report cannot be negative")
	}
	if err := validateWarmupBars(opts.warmupBars); err != nil {
		return err
	}
	return nil
}

func checkIntegrity(
	ctx context.Context,
	store *adminStore,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
	intervalMillis int64,
	timezone string,
	maxMissingReport int,
	failOnMissing bool,
	checkRange warmupRange,
) error {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	existing, err := store.ExistingOpenTimes(ctx, exchange, market, symbol, interval, start, end)
	if err != nil {
		return err
	}

	summary := summarizeIntegrity(existing, start, end, intervalMillis)
	warmupSummary := summarizeIntegrity(existing, checkRange.EffectiveStart, checkRange.RequestedStart, intervalMillis)
	tradingSummary := summarizeIntegrity(existing, checkRange.RequestedStart, checkRange.End, intervalMillis)
	complete := len(warmupSummary.Missing) == 0 && len(tradingSummary.Missing) == 0

	slog.Info(
		"integrity",
		"exchange", exchange,
		"market", market,
		"symbol", symbol,
		"interval", interval,
		"complete", complete,
		"expected", summary.Expected,
		"actual", len(existing),
		"missing", len(summary.Missing),
		"warmup_expected", warmupSummary.Expected,
		"warmup_actual", warmupSummary.Actual,
		"warmup_missing", len(warmupSummary.Missing),
		"trading_expected", tradingSummary.Expected,
		"trading_actual", tradingSummary.Actual,
		"trading_missing", len(tradingSummary.Missing),
		"requested_start", checkRange.RequestedStart,
		"effective_start", checkRange.EffectiveStart,
		"end_exclusive", end,
		"warmup_bars", checkRange.WarmupBars,
	)
	missingDetails := integrityMissingDetails(warmupSummary.Missing, tradingSummary.Missing)
	for index, detail := range missingDetails {
		if index >= maxMissingReport {
			slog.Info(
				"missing truncated",
				"exchange", exchange,
				"market", market,
				"symbol", symbol,
				"interval", interval,
				"remaining", len(missingDetails)-maxMissingReport,
			)
			break
		}
		slog.Warn(
			"missing kline",
			"exchange", exchange,
			"market", market,
			"symbol", symbol,
			"interval", interval,
			"phase", detail.Phase,
			"open_time", time.UnixMilli(detail.OpenTime).In(location).Format(time.RFC3339),
			"open_time_ms", detail.OpenTime,
		)
	}
	if !complete {
		slog.Warn(
			"integrity incomplete",
			"exchange", exchange,
			"market", market,
			"symbol", symbol,
			"interval", interval,
			"missing", len(summary.Missing),
			"expected", summary.Expected,
			"warmup_missing", len(warmupSummary.Missing),
			"trading_missing", len(tradingSummary.Missing),
		)
		if failOnMissing {
			return fmt.Errorf(
				"%s %s %s integrity check failed: warmup missing %d of %d, trading missing %d of %d",
				exchange,
				symbol,
				interval,
				len(warmupSummary.Missing),
				warmupSummary.Expected,
				len(tradingSummary.Missing),
				tradingSummary.Expected,
			)
		}
	}
	return nil
}

type integritySummary struct {
	Expected int
	Actual   int
	Missing  []int64
}

type integrityMissingDetail struct {
	Phase    string
	OpenTime int64
}

func summarizeIntegrity(existing map[int64]struct{}, start int64, end int64, intervalMillis int64) integritySummary {
	summary := integritySummary{
		Missing: make([]int64, 0),
	}
	for openTime := start; openTime < end; openTime += intervalMillis {
		summary.Expected++
		if _, ok := existing[openTime]; ok {
			summary.Actual++
		} else {
			summary.Missing = append(summary.Missing, openTime)
		}
	}
	return summary
}

func integrityMissingDetails(warmupMissing []int64, tradingMissing []int64) []integrityMissingDetail {
	details := make([]integrityMissingDetail, 0, len(warmupMissing)+len(tradingMissing))
	for _, openTime := range warmupMissing {
		details = append(details, integrityMissingDetail{Phase: "warmup", OpenTime: openTime})
	}
	for _, openTime := range tradingMissing {
		details = append(details, integrityMissingDetail{Phase: "trading", OpenTime: openTime})
	}
	return details
}
