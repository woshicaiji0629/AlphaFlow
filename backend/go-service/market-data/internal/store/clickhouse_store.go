package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	_ "github.com/ClickHouse/clickhouse-go/v2"
)

var clickHouseIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type ClickHouseOptions struct {
	Addr        string
	Database    string
	Username    string
	Password    string
	DialTimeout time.Duration
	ReadTimeout time.Duration
}

type ClickHouseStore struct {
	db *sql.DB
}

func NewClickHouseStore(ctx context.Context, options ClickHouseOptions) (*ClickHouseStore, error) {
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
	store := &ClickHouseStore{db: db}
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

func (s *ClickHouseStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *ClickHouseStore) WriteKline(ctx context.Context, kline model.Kline) error {
	if s == nil {
		return nil
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
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
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
		time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert clickhouse kline: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) WriteIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	if s == nil {
		return nil
	}
	valuesJSON, err := json.Marshal(snapshot.Values)
	if err != nil {
		return fmt.Errorf("marshal indicator values: %w", err)
	}
	signalsJSON, err := json.Marshal(snapshot.Signals)
	if err != nil {
		return fmt.Errorf("marshal indicator signals: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO indicator_snapshots (
			exchange, market, symbol, interval,
			open_time_ms, close_time_ms,
			values_json, signals_json, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		snapshot.Exchange,
		snapshot.Market,
		snapshot.Symbol,
		snapshot.Interval,
		snapshot.OpenTime,
		snapshot.CloseTime,
		string(valuesJSON),
		string(signalsJSON),
		snapshot.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert clickhouse indicator: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) initSchema(ctx context.Context) error {
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

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indicator_snapshots (
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			interval LowCardinality(String),
			open_time_ms Int64,
			close_time_ms Int64,
			values_json String,
			signals_json String,
			updated_at_ms Int64
		)
		ENGINE = ReplacingMergeTree(updated_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(open_time_ms))
		ORDER BY (exchange, market, symbol, interval, open_time_ms)
	`); err != nil {
		return fmt.Errorf("create indicator_snapshots table: %w", err)
	}
	return nil
}

func clickHouseDSN(options ClickHouseOptions, database string) string {
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
