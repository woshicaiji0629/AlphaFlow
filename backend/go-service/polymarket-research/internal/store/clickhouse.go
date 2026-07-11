package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"alphaflow/go-service/polymarket-research/internal/model"
	_ "github.com/ClickHouse/clickhouse-go/v2"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Options struct {
	Addr, Database, Username, Password string
	DialTimeout, ReadTimeout           time.Duration
}
type ClickHouse struct{ db *sql.DB }

func NewClickHouse(ctx context.Context, options Options) (*ClickHouse, error) {
	if !identifierPattern.MatchString(options.Database) {
		return nil, fmt.Errorf("invalid clickhouse database %q", options.Database)
	}
	dsn := url.URL{Scheme: "clickhouse", Host: options.Addr, Path: "/" + options.Database}
	if options.Username != "" {
		dsn.User = url.UserPassword(options.Username, options.Password)
	}
	query := dsn.Query()
	query.Set("dial_timeout", options.DialTimeout.String())
	query.Set("read_timeout", options.ReadTimeout.String())
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("clickhouse", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	store := &ClickHouse{db: db}
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

func (s *ClickHouse) initSchema(ctx context.Context) error {
	statements := []string{`CREATE TABLE IF NOT EXISTS polymarket_markets (
		market_id String, condition_id String, event_id String, slug String, title String,
		symbol LowCardinality(String), duration LowCardinality(String), start_time_ms Int64, end_time_ms Int64,
		yes_token_id String, no_token_id String, resolution_source String, active Bool, closed Bool,
		accepting_orders Bool, resolved_outcome LowCardinality(String), price_to_beat String, final_price String, updated_at_ms Int64
	) ENGINE = ReplacingMergeTree(updated_at_ms)
	PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(start_time_ms))
	ORDER BY (symbol, duration, start_time_ms, market_id)`, `CREATE TABLE IF NOT EXISTS polymarket_book_ticks (
		event_time_ms Int64, received_at_ms Int64, market_id String, token_id String, outcome LowCardinality(String), best_bid String, best_ask String, spread String
	) ENGINE = MergeTree PARTITION BY toYYYYMMDD(fromUnixTimestamp64Milli(event_time_ms)) ORDER BY (market_id, token_id, event_time_ms, received_at_ms)`, `CREATE TABLE IF NOT EXISTS polymarket_trades (
		event_time_ms Int64, received_at_ms Int64, market_id String, token_id String, outcome LowCardinality(String), side LowCardinality(String), price String, size String, fee_rate_bps String
	) ENGINE = ReplacingMergeTree(received_at_ms) PARTITION BY toYYYYMMDD(fromUnixTimestamp64Milli(event_time_ms)) ORDER BY (market_id, token_id, event_time_ms, price, size, side)`, `CREATE TABLE IF NOT EXISTS polymarket_reference_prices (
		event_time_ms Int64, received_at_ms Int64, source LowCardinality(String), symbol LowCardinality(String), price String
	) ENGINE = MergeTree PARTITION BY toYYYYMMDD(fromUnixTimestamp64Milli(event_time_ms)) ORDER BY (symbol, source, event_time_ms, received_at_ms)`, `CREATE TABLE IF NOT EXISTS polymarket_resolutions (
		event_time_ms Int64, market_id String, winning_token_id String, winning_outcome LowCardinality(String)
	) ENGINE = ReplacingMergeTree(event_time_ms) ORDER BY (market_id)`}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize polymarket schema: %w", err)
		}
	}
	for _, column := range []string{"price_to_beat String", "final_price String"} {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE polymarket_markets ADD COLUMN IF NOT EXISTS `+column); err != nil {
			return fmt.Errorf("extend polymarket markets schema: %w", err)
		}
	}
	return nil
}

func (s *ClickHouse) UpsertMarkets(ctx context.Context, markets []model.Market) error {
	if len(markets) == 0 {
		return nil
	}
	rows := make([]string, 0, len(markets))
	args := make([]any, 0, len(markets)*19)
	for _, market := range markets {
		rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, market.MarketID, market.ConditionID, market.EventID, market.Slug, market.Title, market.Symbol, market.Duration,
			market.StartTimeMS, market.EndTimeMS, market.YesTokenID, market.NoTokenID, market.ResolutionSource, market.Active, market.Closed,
			market.AcceptingOrders, market.ResolvedOutcome, market.PriceToBeat, market.FinalPrice, market.UpdatedAtMS)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO polymarket_markets (
		market_id, condition_id, event_id, slug, title, symbol, duration, start_time_ms, end_time_ms,
		yes_token_id, no_token_id, resolution_source, active, closed, accepting_orders, resolved_outcome, price_to_beat, final_price, updated_at_ms
	) VALUES `+strings.Join(rows, ","), args...)
	if err != nil {
		return fmt.Errorf("insert polymarket markets: %w", err)
	}
	return nil
}
func (s *ClickHouse) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *ClickHouse) WriteBookTick(ctx context.Context, v model.BookTick) error {
	return s.WriteBookTicks(ctx, []model.BookTick{v})
}
func (s *ClickHouse) WriteBookTicks(ctx context.Context, values []model.BookTick) error {
	if len(values) == 0 {
		return nil
	}
	rows := make([]string, 0, len(values))
	args := make([]any, 0, len(values)*8)
	for _, v := range values {
		rows = append(rows, "(?,?,?,?,?,?,?,?)")
		args = append(args, v.EventTimeMS, v.ReceivedAtMS, v.MarketID, v.TokenID, v.Outcome, v.BestBid, v.BestAsk, v.Spread)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO polymarket_book_ticks VALUES `+strings.Join(rows, ","), args...)
	if err != nil {
		return fmt.Errorf("insert book tick: %w", err)
	}
	return nil
}
func (s *ClickHouse) WriteTrade(ctx context.Context, v model.Trade) error {
	return s.WriteTrades(ctx, []model.Trade{v})
}
func (s *ClickHouse) WriteTrades(ctx context.Context, values []model.Trade) error {
	if len(values) == 0 {
		return nil
	}
	rows := make([]string, 0, len(values))
	args := make([]any, 0, len(values)*9)
	for _, v := range values {
		rows = append(rows, "(?,?,?,?,?,?,?,?,?)")
		args = append(args, v.EventTimeMS, v.ReceivedAtMS, v.MarketID, v.TokenID, v.Outcome, v.Side, v.Price, v.Size, v.FeeRateBPS)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO polymarket_trades VALUES `+strings.Join(rows, ","), args...)
	if err != nil {
		return fmt.Errorf("insert trade: %w", err)
	}
	return nil
}
func (s *ClickHouse) WriteReferencePrice(ctx context.Context, v model.ReferencePrice) error {
	return s.WriteReferencePrices(ctx, []model.ReferencePrice{v})
}
func (s *ClickHouse) WriteReferencePrices(ctx context.Context, values []model.ReferencePrice) error {
	if len(values) == 0 {
		return nil
	}
	rows := make([]string, 0, len(values))
	args := make([]any, 0, len(values)*5)
	for _, v := range values {
		rows = append(rows, "(?,?,?,?,?)")
		args = append(args, v.EventTimeMS, v.ReceivedAtMS, v.Source, v.Symbol, v.Price)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO polymarket_reference_prices VALUES `+strings.Join(rows, ","), args...)
	if err != nil {
		return fmt.Errorf("insert reference price: %w", err)
	}
	return nil
}
func (s *ClickHouse) WriteResolution(ctx context.Context, v model.Resolution) error {
	return s.WriteResolutions(ctx, []model.Resolution{v})
}
func (s *ClickHouse) WriteResolutions(ctx context.Context, values []model.Resolution) error {
	if len(values) == 0 {
		return nil
	}
	rows := make([]string, 0, len(values))
	args := make([]any, 0, len(values)*4)
	for _, v := range values {
		rows = append(rows, "(?,?,?,?)")
		args = append(args, v.EventTimeMS, v.MarketID, v.WinningTokenID, v.WinningOutcome)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO polymarket_resolutions VALUES `+strings.Join(rows, ","), args...)
	if err != nil {
		return fmt.Errorf("update resolution: %w", err)
	}
	return nil
}
