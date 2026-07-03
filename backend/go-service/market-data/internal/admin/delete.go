package admin

import (
	"context"
	"log"
	"strings"

	"github.com/spf13/cobra"
)

type deleteOptions struct {
	rangeOptions
	confirm bool
}

func newDeleteCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := deleteOptions{}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete kline history for one exchange, market, symbol, interval, and date range",
		Long: strings.TrimSpace(`
Delete historical market data for one exchange, market, symbol, interval, and
time range.

This command targets ClickHouse K line history in market_klines.

The default mode is dry-run. Without --confirm, the command only prints how many
rows would be deleted from each table and does not submit mutations.

With --confirm, ClickHouse ALTER TABLE ... DELETE mutations are submitted.
ClickHouse mutations are asynchronous, so physical deletion may not be visible
immediately.

Time ranges use kline open_time with left-closed, right-open semantics:

  start <= open_time < end
`),
		Example: "market-data-admin delete --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202606010000 --end 202607010000\n" +
			"market-data-admin delete --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202606010000 --end 202607010000 --confirm",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizeRangeOptions(&opts.rangeOptions)
			return runDelete(ctx, root.configPath, opts)
		},
	}
	addRangeFlags(cmd, &opts.rangeOptions)
	cmd.Flags().BoolVar(&opts.confirm, "confirm", false, "submit ClickHouse delete mutations; without this flag the command is dry-run")
	return cmd
}

func runDelete(ctx context.Context, configPath string, opts deleteOptions) error {
	if err := validateRangeOptions(opts.rangeOptions); err != nil {
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
	counts, err := store.DeleteCounts(ctx, opts.exchange, opts.market, opts.symbol, opts.interval, start, end)
	if err != nil {
		return err
	}
	log.Printf(
		"delete dry_run=%t exchange=%s market=%s symbol=%s interval=%s start=%d end_exclusive=%d",
		!opts.confirm,
		opts.exchange,
		opts.market,
		opts.symbol,
		opts.interval,
		start,
		end,
	)
	for _, count := range counts {
		log.Printf("delete target table=%s rows=%d", count.Table, count.Rows)
	}
	if !opts.confirm {
		log.Printf("delete skipped: pass --confirm to submit ClickHouse mutations")
		return nil
	}
	if err := store.DeleteRange(ctx, opts.exchange, opts.market, opts.symbol, opts.interval, start, end); err != nil {
		return err
	}
	for _, count := range counts {
		log.Printf("delete submitted table=%s estimated_rows=%d", count.Table, count.Rows)
	}
	return nil
}
