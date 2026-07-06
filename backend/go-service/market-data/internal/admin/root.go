package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/pkg/exchangeclient/binance"
	"alphaflow/go-service/pkg/exchangeclient/bitget"
	"alphaflow/go-service/pkg/exchangeclient/bybit"
	"alphaflow/go-service/pkg/exchangeclient/gate"
	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/pkg/marketmodel"
	"github.com/spf13/cobra"
)

type restClient interface {
	Exchange() string
	Market() string
	FetchKlines(
		ctx context.Context,
		symbol string,
		interval string,
		limit int,
		startTime int64,
	) ([]marketmodel.Kline, error)
}

type rootOptions struct {
	configPath string
}

type rangeOptions struct {
	exchange string
	market   string
	symbol   string
	interval string
	start    string
	end      string
	timezone string
}

type warmupRange struct {
	RequestedStart int64
	EffectiveStart int64
	End            int64
	WarmupBars     int64
}

func Execute(ctx context.Context) error {
	opts := rootOptions{}
	cmd := &cobra.Command{
		Use:   "market-data-admin",
		Short: "Inspect and maintain AlphaFlow market data stored in ClickHouse",
		Long: strings.TrimSpace(`
market-data-admin is an offline ClickHouse maintenance tool for AlphaFlow market data.

It does not run as a service. Each command connects to ClickHouse, performs one
maintenance task, prints a report, and exits.

Time ranges use minute precision and are interpreted in --timezone.
Format: YYYYMMDDHHmm, for example 202606010000.
Range semantics are always left-closed and right-open:

  start <= open_time < end

For example, --start 202606010000 --end 202607010000 covers June 2026 and does
not include the 2026-07-01 00:00 opening kline.
`),
		Example: "market-data-admin --config configs/market-data.local.toml inventory --exchange binance --market um --symbol ETHUSDT\n" +
			"market-data-admin --config configs/market-data.local.toml check --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202606010000 --end 202607010000\n" +
			"market-data-admin --config configs/market-data.local.toml backfill --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202606010000 --end 202607010000\n" +
			"market-data-admin --config configs/market-data.local.toml backfill --async --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m --start 202606010000 --end 202607010000\n" +
			"market-data-admin --config configs/market-data.local.toml queue-status\n" +
			"market-data-admin --config configs/market-data.local.toml market-health --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m\n" +
			"market-data-admin --config configs/market-data.local.toml stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m --start 202606010000 --end 202607010000\n" +
			"market-data-admin --config configs/market-data.local.toml duplicates --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m --start 202606010000 --end 202607010000\n" +
			"market-data-admin --config configs/market-data.local.toml delete --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202606010000 --end 202607010000",
	}
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "path to market-data config file")
	cmd.AddCommand(newInventoryCommand(ctx, &opts))
	cmd.AddCommand(newCheckCommand(ctx, &opts))
	cmd.AddCommand(newBackfillCommand(ctx, &opts))
	cmd.AddCommand(newBackfillWorkerCommand(ctx, &opts))
	cmd.AddCommand(newQueueStatusCommand(ctx, &opts))
	cmd.AddCommand(newMarketHealthCommand(ctx, &opts))
	cmd.AddCommand(newStatsCommand(ctx, &opts))
	cmd.AddCommand(newDuplicatesCommand(ctx, &opts))
	cmd.AddCommand(newDeleteCommand(ctx, &opts))
	return cmd.ExecuteContext(ctx)
}

func loadConfig(configPath string) (config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("load config: %w", err)
	}
	if !cfg.ClickHouse.Enabled {
		return config.Config{}, fmt.Errorf("clickhouse must be enabled in config")
	}
	return cfg, nil
}

func addRangeFlags(cmd *cobra.Command, opts *rangeOptions) {
	cmd.Flags().StringVar(&opts.exchange, "exchange", "", "exchange: binance, gate, bitget, bybit")
	cmd.Flags().StringVar(&opts.market, "market", "", "market, for example um, usdt, linear")
	cmd.Flags().StringVar(&opts.symbol, "symbol", "", "symbol, for example ETHUSDT or ETH_USDT")
	cmd.Flags().StringVar(&opts.interval, "interval", "", "interval, for example 1m,5m,1h")
	cmd.Flags().StringVar(&opts.start, "start", "", "inclusive start time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.end, "end", "", "exclusive end time in YYYYMMDDHHmm")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "Asia/Shanghai", "date boundary timezone")
}

func addWarmupFlag(cmd *cobra.Command, target *int64) {
	cmd.Flags().Int64Var(target, "warmup-bars", 0, "extra kline bars before start for indicator warm-up")
}

func normalizeRangeOptions(opts *rangeOptions) {
	opts.exchange = strings.ToLower(strings.TrimSpace(opts.exchange))
	opts.market = strings.ToLower(strings.TrimSpace(opts.market))
	opts.symbol = strings.ToUpper(strings.TrimSpace(opts.symbol))
	opts.interval = strings.TrimSpace(opts.interval)
	opts.timezone = strings.TrimSpace(opts.timezone)
}

func validateRangeOptions(opts rangeOptions) error {
	if opts.exchange == "" {
		return fmt.Errorf("exchange is required")
	}
	if opts.market == "" {
		return fmt.Errorf("market is required")
	}
	if opts.symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if opts.interval == "" {
		return fmt.Errorf("interval is required")
	}
	if _, err := marketmodel.IntervalMillis(opts.interval); err != nil {
		return err
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
	return nil
}

func validateWarmupBars(warmupBars int64) error {
	if warmupBars < 0 {
		return fmt.Errorf("warmup-bars cannot be negative")
	}
	return nil
}

func warmupStart(start int64, interval string, warmupBars int64) (int64, error) {
	if err := validateWarmupBars(warmupBars); err != nil {
		return 0, err
	}
	if warmupBars == 0 {
		return start, nil
	}
	intervalMillis, err := marketmodel.IntervalMillis(interval)
	if err != nil {
		return 0, err
	}
	return start - intervalMillis*warmupBars, nil
}

func effectiveWarmupRange(start int64, end int64, interval string, warmupBars int64) (warmupRange, error) {
	effectiveStart, err := warmupStart(start, interval, warmupBars)
	if err != nil {
		return warmupRange{}, err
	}
	return warmupRange{
		RequestedStart: start,
		EffectiveStart: effectiveStart,
		End:            end,
		WarmupBars:     warmupBars,
	}, nil
}

func timeRange(startValue string, endValue string, timezone string) (int64, int64, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, 0, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	start, err := parseMinuteTime(startValue, location)
	if err != nil {
		return 0, 0, fmt.Errorf("parse start: %w", err)
	}
	end, err := parseMinuteTime(endValue, location)
	if err != nil {
		return 0, 0, fmt.Errorf("parse end: %w", err)
	}
	if !end.After(start) {
		return 0, 0, fmt.Errorf("end must be greater than start")
	}
	return start.UnixMilli(), end.UnixMilli(), nil
}

func parseMinuteTime(value string, location *time.Location) (time.Time, error) {
	if len(value) != len("200601021504") {
		return time.Time{}, fmt.Errorf("time must use YYYYMMDDHHmm format")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return time.Time{}, fmt.Errorf("time must use YYYYMMDDHHmm format")
		}
	}
	return time.ParseInLocation("200601021504", value, location)
}

func parseList(raw string) []string {
	items := strings.Split(raw, ",")
	values := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func newRESTClient(exchange string) (restClient, error) {
	httpClient := httpclient.New()
	switch strings.ToLower(strings.TrimSpace(exchange)) {
	case "binance":
		return binance.NewRESTClient(config.BinanceRESTBase(), httpClient), nil
	case "gate":
		return gate.NewRESTClient(config.GateRESTBase(), config.GateSettle(), httpClient), nil
	case "bitget":
		return bitget.NewRESTClient(config.BitgetRESTBase(), config.BitgetProductType(), httpClient), nil
	case "bybit":
		return bybit.NewRESTClient(config.BybitRESTBase(), config.BybitCategory(), httpClient), nil
	default:
		return nil, fmt.Errorf("unsupported exchange %q", exchange)
	}
}
