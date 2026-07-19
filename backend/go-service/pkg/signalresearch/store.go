package signalresearch

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type StoreOptions struct {
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

func NewStore(ctx context.Context, options StoreOptions) (*Store, error) {
	if !databaseNamePattern.MatchString(options.Database) {
		return nil, fmt.Errorf("invalid clickhouse database %q", options.Database)
	}
	if options.DialTimeout <= 0 {
		options.DialTimeout = 5 * time.Second
	}
	if options.ReadTimeout <= 0 {
		options.ReadTimeout = 30 * time.Second
	}
	db, err := sql.Open("clickhouse", researchDSN(options))
	if err != nil {
		return nil, fmt.Errorf("open signal research clickhouse: %w", err)
	}
	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ping signal research clickhouse: %w", err)
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

func (s *Store) initSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS supertrend_signal_research (
			run_id String,
			signal_id String,
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			interval LowCardinality(String),
			side LowCardinality(String),
			trigger_sources String,
			signal_time_ms Int64,
			signal_bar_open_time_ms Int64,
			entry_price Float64,
			atr Float64,
			horizon_minutes UInt16,
			feature_version LowCardinality(String),
			feature_snapshot_json String,
			created_at_ms Int64
		) ENGINE = ReplacingMergeTree(created_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(signal_time_ms))
		ORDER BY (run_id, symbol, interval, signal_time_ms, signal_id)`,
		`CREATE TABLE IF NOT EXISTS supertrend_signal_outcomes (
			run_id String,
			signal_id String,
			stop_kind LowCardinality(String),
			stop_value Float64,
			stop_distance_bps Float64,
			take_profit_margin_pct Float64,
			take_profit_bps Float64,
			result LowCardinality(String),
			exit_time_ms Int64,
			observed_bars UInt16,
			max_favorable_bps Float64,
			max_adverse_bps Float64,
			highest_take_profit_margin_pct Float64,
			expiry_return_bps Float64,
			created_at_ms Int64
		) ENGINE = ReplacingMergeTree(created_at_ms)
		ORDER BY (run_id, signal_id, stop_kind, stop_value, take_profit_margin_pct)`,
	}
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("initialize signal research schema: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveSignals(ctx context.Context, items []Signal, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 500
	}
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*16)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.RunID, item.SignalID, item.Exchange, item.Market, item.Symbol, item.Interval,
				string(item.Side), item.TriggerSources, item.SignalTimeMS, item.SignalBarOpenMS, item.EntryPrice,
				item.ATR, item.HorizonMinutes, item.FeatureVersion, item.FeatureSnapshotJSON, item.CreatedAtMS)
		}
		query := `INSERT INTO supertrend_signal_research (
			run_id, signal_id, exchange, market, symbol, interval, side, trigger_sources,
			signal_time_ms, signal_bar_open_time_ms, entry_price, atr, horizon_minutes,
			feature_version, feature_snapshot_json, created_at_ms
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert signal research rows: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveOutcomes(ctx context.Context, items []Outcome, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*15)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.RunID, item.SignalID, string(item.StopKind), item.StopValue,
				item.StopDistanceBps, item.TakeProfitMarginPct, item.TakeProfitBps, item.Result,
				item.ExitTimeMS, item.ObservedBars, item.MaxFavorableBps, item.MaxAdverseBps,
				item.HighestTakeProfitMarginPct, item.ExpiryReturnBps, item.CreatedAtMS)
		}
		query := `INSERT INTO supertrend_signal_outcomes (
			run_id, signal_id, stop_kind, stop_value, stop_distance_bps,
			take_profit_margin_pct, take_profit_bps, result, exit_time_ms, observed_bars,
			max_favorable_bps, max_adverse_bps, highest_take_profit_margin_pct,
			expiry_return_bps, created_at_ms
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert signal outcome rows: %w", err)
		}
	}
	return nil
}

func researchDSN(options StoreOptions) string {
	dsn := url.URL{Scheme: "clickhouse", Host: options.Addr, Path: "/" + options.Database}
	if options.Username != "" {
		dsn.User = url.UserPassword(options.Username, options.Password)
	}
	query := dsn.Query()
	query.Set("dial_timeout", options.DialTimeout.String())
	query.Set("read_timeout", options.ReadTimeout.String())
	dsn.RawQuery = query.Encode()
	return dsn.String()
}
