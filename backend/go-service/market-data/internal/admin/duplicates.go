package admin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"github.com/spf13/cobra"
)

type duplicatesOptions struct {
	rangeOptions
	intervals []string
	limit     int
}

func newDuplicatesCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := duplicatesOptions{}
	var rawIntervals string
	cmd := &cobra.Command{
		Use:   "duplicates",
		Short: "Report physical duplicate kline rows in ClickHouse",
		Long: strings.TrimSpace(`
Report physical duplicate K line rows in ClickHouse for one exact market data
target and time range.

ClickHouse market_klines uses ReplacingMergeTree, so repeated logical rows are
allowed physically and are collapsed by FINAL reads. This command reads the raw
table without FINAL to show where duplicate versions exist.

The command is read-only.
`),
		Example: "market-data-admin duplicates --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000\n" +
			"market-data-admin duplicates --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000\n" +
			"market-data-admin duplicates --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000 --limit 50",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizeRangeOptions(&opts.rangeOptions)
			opts.intervals = parseList(rawIntervals)
			return runDuplicates(ctx, root.configPath, opts)
		},
	}
	addRangeFlags(cmd, &opts.rangeOptions)
	cmd.Flags().StringVar(&rawIntervals, "intervals", "", "comma-separated intervals, for example 1m,5m,1h")
	cmd.Flags().IntVar(&opts.limit, "limit", 100, "maximum duplicate open_time rows to print")
	return cmd
}

func runDuplicates(ctx context.Context, configPath string, opts duplicatesOptions) error {
	if err := validateDuplicatesOptions(opts); err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	start, end, err := timeRange(opts.start, opts.end, opts.timezone)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(opts.timezone)
	if err != nil {
		return fmt.Errorf("load timezone %q: %w", opts.timezone, err)
	}
	intervals := opts.intervals
	if len(intervals) == 0 {
		intervals = []string{opts.interval}
	}

	store, err := newAdminStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "SUMMARY")
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tLOGICAL_ROWS\tPHYSICAL_ROWS\tDUPLICATE_ROWS\tDUPLICATE_OPEN_TIMES\tMAX_VERSIONS")
	details := []duplicateRow{}
	for _, interval := range intervals {
		if err := printDuplicateSummary(ctx, writer, store, opts, interval, start, end); err != nil {
			return err
		}
		rows, err := store.DuplicateRows(
			ctx,
			opts.exchange,
			opts.market,
			opts.symbol,
			interval,
			start,
			end,
			opts.limit,
		)
		if err != nil {
			return err
		}
		details = append(details, rows...)
	}
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "DETAIL")
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tOPEN_TIME\tOPEN_TIME_MS\tVERSIONS")
	for _, row := range details {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			row.Exchange,
			row.Market,
			row.Symbol,
			row.Interval,
			time.UnixMilli(row.OpenTime).In(location).Format(time.RFC3339),
			row.OpenTime,
			row.Versions,
		)
	}
	return writer.Flush()
}

func printDuplicateSummary(
	ctx context.Context,
	writer *tabwriter.Writer,
	store *adminStore,
	opts duplicatesOptions,
	interval string,
	start int64,
	end int64,
) error {
	summary, err := store.DuplicateSummary(
		ctx,
		opts.exchange,
		opts.market,
		opts.symbol,
		interval,
		start,
		end,
	)
	if err != nil {
		return err
	}
	if len(summary) == 0 {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t0\t0\t0\t0\t0\n", opts.exchange, opts.market, opts.symbol, interval)
		return nil
	}
	for _, row := range summary {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
			row.Exchange,
			row.Market,
			row.Symbol,
			row.Interval,
			row.LogicalRows,
			row.PhysicalRows,
			row.DuplicateRows,
			row.DuplicateOpenTimes,
			row.MaxVersions,
		)
	}
	return nil
}

func validateDuplicatesOptions(opts duplicatesOptions) error {
	if opts.limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if opts.interval != "" && len(opts.intervals) > 0 {
		return fmt.Errorf("interval and intervals cannot be used together")
	}
	if opts.exchange == "" {
		return fmt.Errorf("exchange is required")
	}
	if opts.market == "" {
		return fmt.Errorf("market is required")
	}
	if opts.symbol == "" {
		return fmt.Errorf("symbol is required")
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
	if opts.interval != "" {
		if _, err := marketmodel.IntervalMillis(opts.interval); err != nil {
			return err
		}
	}
	if opts.interval == "" && len(opts.intervals) == 0 {
		return fmt.Errorf("interval or intervals is required")
	}
	for _, interval := range opts.intervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return err
		}
	}
	return nil
}
