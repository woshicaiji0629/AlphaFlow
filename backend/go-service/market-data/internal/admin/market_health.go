package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"alphaflow/go-service/market-data/internal/config"
	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/constants"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/redisclient"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
)

type marketHealthOptions struct {
	exchange       string
	market         string
	symbol         string
	intervals      []string
	windowOpenTime int64
}

type marketHealthRow struct {
	Exchange              string
	Market                string
	Symbol                string
	Interval              string
	KlineStatus           string
	IndicatorStatus       string
	LastKlineOpenTime     int64
	LastIndicatorOpenTime int64
	Reason                string
	UpdatedAt             int64
	Ready                 bool
	Status                string
}

func newMarketHealthCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	opts := marketHealthOptions{}
	var rawIntervals string
	cmd := &cobra.Command{
		Use:   "market-health",
		Short: "Print Redis market data health and NATS queue lag",
		Example: "market-data-admin --config configs/market-data.local.toml market-health " +
			"--exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.intervals = parseList(rawIntervals)
			normalizeMarketHealthOptions(&opts)
			return runMarketHealth(ctx, root.configPath, opts)
		},
	}
	cmd.Flags().StringVar(&opts.exchange, "exchange", "", "exchange: binance, gate, bitget, bybit")
	cmd.Flags().StringVar(&opts.market, "market", "", "market, for example um, usdt, linear")
	cmd.Flags().StringVar(&opts.symbol, "symbol", "", "symbol, for example ETHUSDT or ETH_USDT")
	cmd.Flags().StringVar(&rawIntervals, "intervals", "", "comma-separated intervals, for example 1m,3m,5m")
	cmd.Flags().Int64Var(&opts.windowOpenTime, "window-open-time", 0, "optional strategy window open_time in milliseconds")
	return cmd
}

func runMarketHealth(ctx context.Context, configPath string, opts marketHealthOptions) error {
	if err := validateMarketHealthOptions(opts); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	redisCfg := config.RedisConfigs()[constants.RedisDefaultInstance]
	redisClient, err := redisclient.New(ctx, redisCfg)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer redisclient.Close(redisClient)

	rows, err := readMarketHealthRows(ctx, redisClient, opts)
	if err != nil {
		return err
	}
	decisionReady := marketHealthReady(rows)
	if err := printMarketHealthRows(rows, decisionReady); err != nil {
		return err
	}

	conn, err := connectNATS(cfg.NATS.URL)
	if err != nil {
		return err
	}
	defer conn.Close()
	queueRows, err := buildQueueStatusRows(ctx, conn.JetStreamContext, queueStatusTargets)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout)
	return printQueueStatusRows(queueRows)
}

func normalizeMarketHealthOptions(opts *marketHealthOptions) {
	opts.exchange = strings.ToLower(strings.TrimSpace(opts.exchange))
	opts.market = strings.ToLower(strings.TrimSpace(opts.market))
	opts.symbol = strings.ToUpper(strings.TrimSpace(opts.symbol))
}

func validateMarketHealthOptions(opts marketHealthOptions) error {
	if opts.exchange == "" {
		return fmt.Errorf("exchange is required")
	}
	if opts.market == "" {
		return fmt.Errorf("market is required")
	}
	if opts.symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if len(opts.intervals) == 0 {
		return fmt.Errorf("intervals is required")
	}
	for _, interval := range opts.intervals {
		if _, err := marketmodel.IntervalMillis(interval); err != nil {
			return err
		}
	}
	if opts.windowOpenTime < 0 {
		return fmt.Errorf("window-open-time cannot be negative")
	}
	return nil
}

type redisStringGetter interface {
	Get(ctx context.Context, key string) *redis.StringCmd
}

func readMarketHealthRows(
	ctx context.Context,
	client redisStringGetter,
	opts marketHealthOptions,
) ([]marketHealthRow, error) {
	rows := make([]marketHealthRow, 0, len(opts.intervals))
	for _, interval := range opts.intervals {
		key := model.DataHealthKey(opts.exchange, opts.market, opts.symbol, interval)
		raw, err := client.Get(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			rows = append(rows, marketHealthRow{
				Exchange: opts.exchange,
				Market:   opts.market,
				Symbol:   opts.symbol,
				Interval: interval,
				Ready:    false,
				Status:   "missing",
				Reason:   "health key missing",
			})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read data health %s: %w", key, err)
		}
		var health model.DataHealth
		if err := json.Unmarshal([]byte(raw), &health); err != nil {
			return nil, fmt.Errorf("decode data health %s: %w", key, err)
		}
		row := marketHealthRow{
			Exchange:              health.Exchange,
			Market:                health.Market,
			Symbol:                health.Symbol,
			Interval:              health.Interval,
			KlineStatus:           health.KlineStatus,
			IndicatorStatus:       health.IndicatorStatus,
			LastKlineOpenTime:     health.LastKlineOpenTime,
			LastIndicatorOpenTime: health.LastIndicatorOpenTime,
			Reason:                health.Reason,
			UpdatedAt:             health.UpdatedAt,
			Status:                "ok",
		}
		row.Ready = rowMarketHealthReady(row, opts.windowOpenTime)
		if !row.Ready && row.Reason == "" {
			row.Reason = marketHealthNotReadyReason(row, opts.windowOpenTime)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func rowMarketHealthReady(row marketHealthRow, windowOpenTime int64) bool {
	if row.KlineStatus != model.HealthStatusOK || row.IndicatorStatus != model.HealthStatusOK {
		return false
	}
	return windowOpenTime <= 0 || row.LastIndicatorOpenTime <= 0 || row.LastIndicatorOpenTime >= windowOpenTime
}

func marketHealthNotReadyReason(row marketHealthRow, windowOpenTime int64) string {
	if row.KlineStatus != model.HealthStatusOK || row.IndicatorStatus != model.HealthStatusOK {
		return fmt.Sprintf("kline=%s indicator=%s", row.KlineStatus, row.IndicatorStatus)
	}
	if windowOpenTime > 0 && row.LastIndicatorOpenTime > 0 && row.LastIndicatorOpenTime < windowOpenTime {
		return fmt.Sprintf("indicator cursor behind window: indicator_open_time=%d window_open_time=%d",
			row.LastIndicatorOpenTime,
			windowOpenTime,
		)
	}
	return ""
}

func marketHealthReady(rows []marketHealthRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !row.Ready {
			return false
		}
	}
	return true
}

func printMarketHealthRows(rows []marketHealthRow, decisionReady bool) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "EXCHANGE\tMARKET\tSYMBOL\tINTERVAL\tKLINE\tINDICATOR\tLAST_KLINE\tLAST_INDICATOR\tUPDATED_AT\tREADY\tSTATUS\tREASON")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%t\t%s\t%s\n",
			row.Exchange,
			row.Market,
			row.Symbol,
			row.Interval,
			row.KlineStatus,
			row.IndicatorStatus,
			row.LastKlineOpenTime,
			row.LastIndicatorOpenTime,
			row.UpdatedAt,
			row.Ready,
			row.Status,
			row.Reason,
		)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "DECISION_READY=%t\n", decisionReady)
	return nil
}
