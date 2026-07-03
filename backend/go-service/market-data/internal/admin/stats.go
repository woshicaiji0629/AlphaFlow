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

type statsOptions struct {
	rangeOptions
	intervals        []string
	showMissing      bool
	maxMissingReport int
}

type statsRow struct {
	Exchange      string
	Market        string
	Symbol        string
	Interval      string
	Expected      int
	LogicalRows   uint64
	PhysicalRows  uint64
	DuplicateRows uint64
	Missing       []int64
	FirstOpenTime int64
	LastOpenTime  int64
}

func newStatsCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := statsOptions{}
	var rawIntervals string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print a kline health summary for one symbol and interval list",
		Long: strings.TrimSpace(`
Print a read-only health summary for one exchange, market, symbol, interval
list, and time range.

The report combines completeness and physical duplicate information:
  - EXPECTED: expected kline open_time count
  - LOGICAL_ROWS: unique open_time count
  - PHYSICAL_ROWS: raw ClickHouse row count
  - DUPLICATE_ROWS: physical minus logical rows
  - MISSING: expected minus actual open_time count

By default, stats prints only the summary table. Use --show-missing to print
missing open_time details after the table.
`),
		Example: "market-data-admin stats --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000\n" +
			"market-data-admin stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizeRangeOptions(&opts.rangeOptions)
			opts.intervals = parseList(rawIntervals)
			return runStats(ctx, root.configPath, opts)
		},
	}
	addRangeFlags(cmd, &opts.rangeOptions)
	cmd.Flags().StringVar(&rawIntervals, "intervals", "", "comma-separated intervals, for example 1m,5m,1h")
	cmd.Flags().BoolVar(&opts.showMissing, "show-missing", false, "print missing open_time details")
	cmd.Flags().IntVar(&opts.maxMissingReport, "max-missing-report", 100, "maximum missing klines to print per interval")
	return cmd
}

func runStats(ctx context.Context, configPath string, opts statsOptions) error {
	if err := validateStatsOptions(&opts); err != nil {
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

	rows, err := buildStatsRows(ctx, store, opts, intervals, start, end)
	if err != nil {
		return err
	}
	return printStatsRows(rows, opts, location)
}

func validateStatsOptions(opts *statsOptions) error {
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
	return nil
}

func buildStatsRows(
	ctx context.Context,
	store *adminStore,
	opts statsOptions,
	intervals []string,
	start int64,
	end int64,
) ([]statsRow, error) {
	filter := inventoryFilter{
		Exchange: opts.exchange,
		Market:   opts.market,
		Symbol:   opts.symbol,
	}
	inventoryRows, err := store.InventoryRange(ctx, filter, start, end)
	if err != nil {
		return nil, err
	}
	inventoryByInterval := make(map[string]inventoryRangeRow, len(inventoryRows))
	for _, row := range inventoryRows {
		inventoryByInterval[row.Interval] = row
	}

	rows := make([]statsRow, 0, len(intervals))
	for _, interval := range intervals {
		intervalMillis, err := marketmodel.IntervalMillis(interval)
		if err != nil {
			return nil, err
		}
		existing, err := store.ExistingOpenTimes(ctx, opts.exchange, opts.market, opts.symbol, interval, start, end)
		if err != nil {
			return nil, err
		}
		summary := summarizeIntegrity(existing, start, end, intervalMillis)
		inventory := inventoryByInterval[interval]
		rows = append(rows, statsRow{
			Exchange:      opts.exchange,
			Market:        opts.market,
			Symbol:        opts.symbol,
			Interval:      interval,
			Expected:      summary.Expected,
			LogicalRows:   inventory.KlineRows,
			PhysicalRows:  inventory.PhysicalRows,
			DuplicateRows: duplicateRows(inventory.KlineRows, inventory.PhysicalRows),
			Missing:       summary.Missing,
			FirstOpenTime: inventory.FirstOpenTime,
			LastOpenTime:  inventory.LastOpenTime,
		})
	}
	return rows, nil
}

func printStatsRows(rows []statsRow, opts statsOptions, location *time.Location) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tEXPECTED\tLOGICAL_ROWS\tPHYSICAL_ROWS\tDUPLICATE_ROWS\tMISSING\tCOMPLETE\tDUP_RATIO\tFIRST_OPEN\tLAST_OPEN")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%t\t%s\t%s\t%s\n",
			row.Exchange,
			row.Market,
			row.Symbol,
			row.Interval,
			row.Expected,
			row.LogicalRows,
			row.PhysicalRows,
			row.DuplicateRows,
			len(row.Missing),
			len(row.Missing) == 0,
			duplicateRatio(row.DuplicateRows, row.PhysicalRows),
			formatOpenTime(row.FirstOpenTime, row.LogicalRows, location),
			formatOpenTime(row.LastOpenTime, row.LogicalRows, location),
		)
	}
	if opts.showMissing {
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "MISSING")
		fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tOPEN_TIME\tOPEN_TIME_MS")
		for _, row := range rows {
			limit := len(row.Missing)
			if opts.maxMissingReport < limit {
				limit = opts.maxMissingReport
			}
			for index := 0; index < limit; index++ {
				openTime := row.Missing[index]
				fmt.Fprintf(
					writer,
					"%s\t%s\t%s\t%s\t%s\t%d\n",
					row.Exchange,
					row.Market,
					row.Symbol,
					row.Interval,
					time.UnixMilli(openTime).In(location).Format(time.RFC3339),
					openTime,
				)
			}
		}
	}
	return writer.Flush()
}

func duplicateRatio(duplicateRows uint64, physicalRows uint64) string {
	if physicalRows == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(duplicateRows)/float64(physicalRows)*100)
}
