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

type inventoryOptions struct {
	exchange  string
	market    string
	symbol    string
	interval  string
	intervals []string
	start     string
	end       string
	timezone  string
}

func newInventoryCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := inventoryOptions{}
	var rawIntervals string
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "List available kline data ranges in ClickHouse",
		Long: strings.TrimSpace(`
List the data currently stored in ClickHouse, grouped by exchange, market,
symbol, and interval.

The report includes:
  - LOGICAL_ROWS by unique open_time
  - PHYSICAL_ROWS from market_klines
  - DUPLICATE_ROWS as physical minus logical rows
  - first and last kline open times for each group

Use filters to avoid dumping the whole database when many symbols are stored.

If --start and --end are provided, inventory switches to a range completeness
view and prints EXPECTED, KLINES, MISSING, and COMPLETE for each group. Use
--intervals with an exact exchange, market, and symbol to include intervals
that are fully missing from ClickHouse.
`),
		Example: "market-data-admin inventory\n" +
			"market-data-admin inventory --exchange binance --market um\n" +
			"market-data-admin inventory --exchange binance --market um --symbol ETHUSDT\n" +
			"market-data-admin inventory --exchange binance --market um --symbol ETHUSDT --interval 1m\n" +
			"market-data-admin inventory --exchange binance --market um --symbol ETHUSDT --start 202605010000 --end 202607010000\n" +
			"market-data-admin inventory --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.exchange = strings.ToLower(strings.TrimSpace(opts.exchange))
			opts.market = strings.ToLower(strings.TrimSpace(opts.market))
			opts.symbol = strings.ToUpper(strings.TrimSpace(opts.symbol))
			opts.interval = strings.TrimSpace(opts.interval)
			opts.intervals = parseList(rawIntervals)
			opts.start = strings.TrimSpace(opts.start)
			opts.end = strings.TrimSpace(opts.end)
			opts.timezone = strings.TrimSpace(opts.timezone)
			return runInventory(ctx, root.configPath, opts)
		},
	}
	cmd.Flags().StringVar(&opts.exchange, "exchange", "", "optional exchange filter")
	cmd.Flags().StringVar(&opts.market, "market", "", "optional market filter")
	cmd.Flags().StringVar(&opts.symbol, "symbol", "", "optional symbol filter")
	cmd.Flags().StringVar(&opts.interval, "interval", "", "optional interval filter")
	cmd.Flags().StringVar(&rawIntervals, "intervals", "", "optional comma-separated interval list for range completeness view")
	cmd.Flags().StringVar(&opts.start, "start", "", "optional inclusive start time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.end, "end", "", "optional exclusive end time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "Asia/Shanghai", "display timezone")
	return cmd
}

func runInventory(ctx context.Context, configPath string, opts inventoryOptions) error {
	if err := validateInventoryOptions(&opts); err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(opts.timezone)
	if err != nil {
		return fmt.Errorf("load timezone %q: %w", opts.timezone, err)
	}
	store, err := newAdminStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	filter := inventoryFilter{
		Exchange: opts.exchange,
		Market:   opts.market,
		Symbol:   opts.symbol,
		Interval: opts.interval,
	}
	if opts.start != "" || opts.end != "" {
		if opts.start == "" || opts.end == "" {
			return fmt.Errorf("start and end must be provided together")
		}
		start, end, err := timeRange(opts.start, opts.end, opts.timezone)
		if err != nil {
			return err
		}
		if len(opts.intervals) > 0 {
			filter.Interval = ""
		}
		return printInventoryRange(ctx, store, filter, opts.intervals, start, end, location)
	}

	rows, err := store.Inventory(ctx, filter)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tLOGICAL_ROWS\tPHYSICAL_ROWS\tDUPLICATE_ROWS\tFIRST_OPEN\tLAST_OPEN")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			row.Exchange,
			row.Market,
			row.Symbol,
			row.Interval,
			row.KlineRows,
			row.PhysicalRows,
			duplicateRows(row.KlineRows, row.PhysicalRows),
			time.UnixMilli(row.FirstOpenTime).In(location).Format(time.RFC3339),
			time.UnixMilli(row.LastOpenTime).In(location).Format(time.RFC3339),
		)
	}
	return writer.Flush()
}

func validateInventoryOptions(opts *inventoryOptions) error {
	hasRange := opts.start != "" || opts.end != ""
	if opts.interval != "" && len(opts.intervals) > 0 {
		return fmt.Errorf("interval and intervals cannot be used together")
	}
	if opts.interval != "" {
		if _, err := marketmodel.IntervalMillis(opts.interval); err != nil {
			return err
		}
	}
	if len(opts.intervals) > 0 && !hasRange {
		return fmt.Errorf("intervals can only be used with start and end")
	}
	for _, interval := range opts.intervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return err
		}
	}
	if hasRange {
		if opts.start == "" || opts.end == "" {
			return fmt.Errorf("start and end must be provided together")
		}
		if _, _, err := timeRange(opts.start, opts.end, opts.timezone); err != nil {
			return err
		}
	}
	if hasRange && opts.interval != "" {
		opts.intervals = []string{opts.interval}
	}
	if hasRange && len(opts.intervals) > 0 && (opts.exchange == "" || opts.market == "" || opts.symbol == "") {
		return fmt.Errorf("exchange, market, and symbol are required when range inventory uses interval or intervals")
	}
	return nil
}

func printInventoryRange(
	ctx context.Context,
	store *adminStore,
	filter inventoryFilter,
	intervals []string,
	start int64,
	end int64,
	location *time.Location,
) error {
	rows, err := store.InventoryRange(ctx, filter, start, end)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tEXPECTED\tLOGICAL_ROWS\tPHYSICAL_ROWS\tDUPLICATE_ROWS\tMISSING\tCOMPLETE\tFIRST_OPEN\tLAST_OPEN")
	if len(intervals) > 0 {
		rowsByInterval := make(map[string]inventoryRangeRow, len(rows))
		for _, row := range rows {
			rowsByInterval[row.Interval] = row
		}
		for _, interval := range intervals {
			row, ok := rowsByInterval[interval]
			if !ok {
				row = inventoryRangeRow{
					Exchange:     filter.Exchange,
					Market:       filter.Market,
					Symbol:       filter.Symbol,
					Interval:     interval,
					PhysicalRows: 0,
				}
			}
			if err := printInventoryRangeRow(writer, row, start, end, location); err != nil {
				return err
			}
		}
		return writer.Flush()
	}
	for _, row := range rows {
		if err := printInventoryRangeRow(writer, row, start, end, location); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func printInventoryRangeRow(
	writer *tabwriter.Writer,
	row inventoryRangeRow,
	start int64,
	end int64,
	location *time.Location,
) error {
	intervalMillis, err := marketmodel.IntervalMillis(row.Interval)
	if err != nil {
		return err
	}
	expected := expectedKlines(start, end, intervalMillis)
	missing := int64(expected) - int64(row.KlineRows)
	if missing < 0 {
		missing = 0
	}
	fmt.Fprintf(
		writer,
		"%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%t\t%s\t%s\n",
		row.Exchange,
		row.Market,
		row.Symbol,
		row.Interval,
		expected,
		row.KlineRows,
		row.PhysicalRows,
		duplicateRows(row.KlineRows, row.PhysicalRows),
		missing,
		uint64(expected) == row.KlineRows,
		formatOpenTime(row.FirstOpenTime, row.KlineRows, location),
		formatOpenTime(row.LastOpenTime, row.KlineRows, location),
	)
	return nil
}

func duplicateRows(logicalRows uint64, physicalRows uint64) uint64 {
	if physicalRows <= logicalRows {
		return 0
	}
	return physicalRows - logicalRows
}

func formatOpenTime(openTime int64, rows uint64, location *time.Location) string {
	if rows == 0 {
		return "-"
	}
	return time.UnixMilli(openTime).In(location).Format(time.RFC3339)
}

func expectedKlines(start int64, end int64, intervalMillis int64) uint64 {
	if end <= start {
		return 0
	}
	return uint64((end - start) / intervalMillis)
}
