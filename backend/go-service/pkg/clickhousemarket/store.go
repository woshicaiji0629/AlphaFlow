package clickhousemarket

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	_ "github.com/ClickHouse/clickhouse-go/v2"
)

var clickHouseIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Options struct {
	Addr        string
	Database    string
	Username    string
	Password    string
	DialTimeout time.Duration
	ReadTimeout time.Duration
}

type Store struct {
	db *sql.DB
}

func NewStore(ctx context.Context, options Options) (*Store, error) {
	if !clickHouseIdentifierPattern.MatchString(options.Database) {
		return nil, fmt.Errorf("invalid clickhouse database %q", options.Database)
	}
	if options.DialTimeout <= 0 {
		options.DialTimeout = 5 * time.Second
	}
	if options.ReadTimeout <= 0 {
		options.ReadTimeout = 30 * time.Second
	}

	db, err := sql.Open("clickhouse", clickHouseDSN(options, options.Database))
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection: %w", err)
	}
	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	if err := store.initSchema(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) WriteKline(ctx context.Context, kline marketmodel.Kline) error {
	return s.WriteKlines(ctx, []marketmodel.Kline{kline})
}

func (s *Store) WriteKlines(ctx context.Context, klines []marketmodel.Kline) error {
	if s == nil {
		return nil
	}
	if len(klines) == 0 {
		return nil
	}

	rows := make([]string, 0, len(klines))
	args := make([]any, 0, len(klines)*20)
	updatedAt := time.Now().UnixMilli()
	for _, kline := range klines {
		rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			kline.Exchange,
			kline.Market,
			kline.Symbol,
			kline.Interval,
			kline.OpenTime,
			kline.CloseTime,
			kline.Open,
			kline.High,
			kline.Low,
			kline.Close,
			kline.Volume,
			kline.QuoteVolume,
			kline.TradeCount,
			kline.TakerBuyVolume,
			kline.TakerBuyQuoteVolume,
			kline.EventTime,
			kline.FirstTradeID,
			kline.LastTradeID,
			kline.IsClosed,
			updatedAt,
		)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO market_klines (
			exchange, market, symbol, interval,
			open_time_ms, close_time_ms,
			open, high, low, close,
			volume, quote_volume, trade_count,
			taker_buy_volume, taker_buy_quote_volume,
			event_time_ms, first_trade_id, last_trade_id,
			is_closed, updated_at_ms
		) VALUES `+strings.Join(rows, ", "), args...)
	if err != nil {
		return fmt.Errorf("insert clickhouse kline: %w", err)
	}
	return nil
}

func (s *Store) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]marketmodel.Kline, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			exchange, market, symbol, interval,
			open_time_ms, close_time_ms,
			open, high, low, close,
			volume, quote_volume, trade_count,
			taker_buy_volume, taker_buy_quote_volume,
			event_time_ms, first_trade_id, last_trade_id,
			is_closed
		FROM market_klines FINAL
		WHERE exchange = ?
			AND market = ?
			AND symbol = ?
			AND interval = ?
			AND open_time_ms >= ?
			AND open_time_ms <= ?
		ORDER BY open_time_ms ASC
	`, exchange, market, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("query clickhouse klines: %w", err)
	}
	defer rows.Close()

	klines := []marketmodel.Kline{}
	for rows.Next() {
		var kline marketmodel.Kline
		if err := rows.Scan(
			&kline.Exchange,
			&kline.Market,
			&kline.Symbol,
			&kline.Interval,
			&kline.OpenTime,
			&kline.CloseTime,
			&kline.Open,
			&kline.High,
			&kline.Low,
			&kline.Close,
			&kline.Volume,
			&kline.QuoteVolume,
			&kline.TradeCount,
			&kline.TakerBuyVolume,
			&kline.TakerBuyQuoteVolume,
			&kline.EventTime,
			&kline.FirstTradeID,
			&kline.LastTradeID,
			&kline.IsClosed,
		); err != nil {
			return nil, fmt.Errorf("scan clickhouse kline: %w", err)
		}
		klines = append(klines, kline)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clickhouse klines: %w", err)
	}
	return klines, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS market_klines (
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			interval LowCardinality(String),
			open_time_ms Int64,
			close_time_ms Int64,
			open String,
			high String,
			low String,
			close String,
			volume String,
			quote_volume String,
			trade_count Int64,
			taker_buy_volume String,
			taker_buy_quote_volume String,
			event_time_ms Int64,
			first_trade_id Int64,
			last_trade_id Int64,
			is_closed Bool,
			updated_at_ms Int64
		)
		ENGINE = ReplacingMergeTree(updated_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(open_time_ms))
		ORDER BY (exchange, market, symbol, interval, open_time_ms)
	`); err != nil {
		return fmt.Errorf("create market_klines table: %w", err)
	}

	return nil
}

func clickHouseDSN(options Options, database string) string {
	dsn := url.URL{
		Scheme: "clickhouse",
		Host:   options.Addr,
	}
	if database != "" {
		dsn.Path = "/" + database
	}
	if options.Username != "" {
		dsn.User = url.UserPassword(options.Username, options.Password)
	}
	query := dsn.Query()
	query.Set("dial_timeout", options.DialTimeout.String())
	query.Set("read_timeout", options.ReadTimeout.String())
	dsn.RawQuery = query.Encode()
	return dsn.String()
}
