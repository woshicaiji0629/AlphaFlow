package position

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"alphaflow/go-service/pkg/strategy"
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

func (s *ClickHouseStore) AppendEvent(ctx context.Context, event strategy.StrategyEvent) error {
	return s.AppendEvents(ctx, []strategy.StrategyEvent{event})
}

func (s *ClickHouseStore) AppendEvents(ctx context.Context, events []strategy.StrategyEvent) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	query, args, err := buildStrategyEventsInsert(events)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert strategy events: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) SaveBacktestRunSummary(ctx context.Context, summary strategy.BacktestRunSummary) error {
	if s == nil || s.db == nil {
		return nil
	}
	query, args, err := buildBacktestRunSummaryInsert(summary)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert backtest run summary: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) SaveBacktestTrades(ctx context.Context, trades []strategy.BacktestTrade) error {
	if s == nil || s.db == nil || len(trades) == 0 {
		return nil
	}
	query, args, err := buildBacktestTradesInsert(trades)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert backtest trades: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS strategy_events (
			event_id String,
			scope LowCardinality(String),
			run_id String,
			account String,
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			strategy_name LowCardinality(String),
			event_type LowCardinality(String),
			event_time_ms Int64,
			bar_open_time_ms Int64,
			side LowCardinality(String),
			position_side LowCardinality(String),
			position_mode LowCardinality(String),
			size Float64,
			price String,
			notional String,
			fee String,
			pnl String,
			reason String,
			score Float64,
			confidence Float64,
			order_id String,
			intent_id String,
			exchange_order_id String,
			metadata_json String,
			created_at_ms Int64
		)
		ENGINE = ReplacingMergeTree(created_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(event_time_ms))
		ORDER BY (scope, exchange, market, symbol, strategy_name, event_time_ms, event_id)
	`); err != nil {
		return fmt.Errorf("create strategy_events table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS backtest_trades (
			trade_id String,
			run_id String,
			account String,
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			strategy_name LowCardinality(String),
			position_side LowCardinality(String),
			entry_time_ms Int64,
			entry_bar_open_time_ms Int64,
			entry_price String,
			entry_size Float64,
			entry_reason String,
			exit_time_ms Int64,
			exit_bar_open_time_ms Int64,
			exit_price String,
			exit_size Float64,
			exit_reason String,
			pnl String,
			fee String,
			return_pct String,
			return_on_margin_pct String,
			entry_event_id String,
			exit_event_id String,
			entry_exchange_order_id String,
			exit_exchange_order_id String,
			metadata_json String,
			created_at_ms Int64
		)
		ENGINE = ReplacingMergeTree(created_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(exit_time_ms))
		ORDER BY (run_id, exchange, market, symbol, strategy_name, exit_time_ms, trade_id)
	`); err != nil {
		return fmt.Errorf("create backtest_trades table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS backtest_run_summary (
			run_id String,
			status LowCardinality(String),
			strategy_set String,
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbols_json String,
			start_time_ms Int64,
			end_time_ms Int64,
			total_trades Int64,
			win_rate Float64,
			net_pnl String,
			max_drawdown String,
			profit_factor Float64,
			sharpe Float64,
			failure_reason String,
			metadata_json String,
			created_at_ms Int64,
			updated_at_ms Int64
		)
		ENGINE = ReplacingMergeTree(updated_at_ms)
		ORDER BY run_id
	`); err != nil {
		return fmt.Errorf("create backtest_run_summary table: %w", err)
	}
	return nil
}

func buildStrategyEventsInsert(events []strategy.StrategyEvent) (string, []any, error) {
	if len(events) == 0 {
		return "", nil, nil
	}
	rows := make([]string, 0, len(events))
	args := make([]any, 0, len(events)*27)
	for _, event := range events {
		metadata, err := encodeStringMap(event.Metadata)
		if err != nil {
			return "", nil, fmt.Errorf("encode event metadata: %w", err)
		}
		rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			event.EventID,
			string(event.Scope),
			event.RunID,
			event.Account,
			event.Exchange,
			event.Market,
			event.Symbol,
			event.StrategyName,
			string(event.EventType),
			event.EventTime,
			event.BarOpenTime,
			string(event.Side),
			string(event.PositionSide),
			string(event.PositionMode),
			event.Size,
			event.Price,
			event.Notional,
			event.Fee,
			event.PnL,
			event.Reason,
			event.Score,
			event.Confidence,
			event.OrderID,
			event.IntentID,
			event.ExchangeOrderID,
			metadata,
			event.CreatedAt,
		)
	}
	return `
		INSERT INTO strategy_events (
			event_id, scope, run_id, account,
			exchange, market, symbol, strategy_name,
			event_type, event_time_ms, bar_open_time_ms,
			side, position_side, position_mode,
			size, price, notional, fee, pnl,
			reason, score, confidence,
			order_id, intent_id, exchange_order_id,
			metadata_json, created_at_ms
		) VALUES ` + strings.Join(rows, ", "), args, nil
}

func buildBacktestRunSummaryInsert(summary strategy.BacktestRunSummary) (string, []any, error) {
	symbols, err := encodeStringSlice(summary.Symbols)
	if err != nil {
		return "", nil, fmt.Errorf("encode summary symbols: %w", err)
	}
	metadata, err := encodeStringMap(summary.Metadata)
	if err != nil {
		return "", nil, fmt.Errorf("encode summary metadata: %w", err)
	}
	return `
		INSERT INTO backtest_run_summary (
			run_id, status, strategy_set, exchange, market,
			symbols_json, start_time_ms, end_time_ms,
			total_trades, win_rate, net_pnl, max_drawdown,
			profit_factor, sharpe, failure_reason,
			metadata_json, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, []any{
			summary.RunID,
			string(summary.Status),
			summary.StrategySet,
			summary.Exchange,
			summary.Market,
			symbols,
			summary.StartTime,
			summary.EndTime,
			summary.TotalTrades,
			summary.WinRate,
			summary.NetPnL,
			summary.MaxDrawdown,
			summary.ProfitFactor,
			summary.Sharpe,
			summary.FailureReason,
			metadata,
			summary.CreatedAt,
			summary.UpdatedAt,
		}, nil
}

func buildBacktestTradesInsert(trades []strategy.BacktestTrade) (string, []any, error) {
	if len(trades) == 0 {
		return "", nil, nil
	}
	rows := make([]string, 0, len(trades))
	args := make([]any, 0, len(trades)*28)
	for _, trade := range trades {
		metadata, err := encodeStringMap(trade.Metadata)
		if err != nil {
			return "", nil, fmt.Errorf("encode trade metadata: %w", err)
		}
		rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			trade.TradeID,
			trade.RunID,
			trade.Account,
			trade.Exchange,
			trade.Market,
			trade.Symbol,
			trade.StrategyName,
			string(trade.PositionSide),
			trade.EntryTime,
			trade.EntryBarOpenTime,
			trade.EntryPrice,
			trade.EntrySize,
			trade.EntryReason,
			trade.ExitTime,
			trade.ExitBarOpenTime,
			trade.ExitPrice,
			trade.ExitSize,
			trade.ExitReason,
			trade.PnL,
			trade.Fee,
			trade.ReturnPct,
			trade.ReturnOnMarginPct,
			trade.EntryEventID,
			trade.ExitEventID,
			trade.EntryExchangeOrderID,
			trade.ExitExchangeOrderID,
			metadata,
			trade.CreatedAt,
		)
	}
	return `
		INSERT INTO backtest_trades (
			trade_id, run_id, account,
			exchange, market, symbol, strategy_name, position_side,
			entry_time_ms, entry_bar_open_time_ms, entry_price, entry_size, entry_reason,
			exit_time_ms, exit_bar_open_time_ms, exit_price, exit_size, exit_reason,
			pnl, fee, return_pct, return_on_margin_pct,
			entry_event_id, exit_event_id,
			entry_exchange_order_id, exit_exchange_order_id,
			metadata_json, created_at_ms
		) VALUES ` + strings.Join(rows, ", "), args, nil
}

func encodeStringMap(items map[string]string) (string, error) {
	if items == nil {
		return "{}", nil
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func encodeStringSlice(items []string) (string, error) {
	if items == nil {
		return "[]", nil
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(payload), nil
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
