package admin

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/config"
	_ "github.com/ClickHouse/clickhouse-go/v2"
)

type adminStore struct {
	db *sql.DB
}

type inventoryRow struct {
	Exchange      string
	Market        string
	Symbol        string
	Interval      string
	KlineRows     uint64
	PhysicalRows  uint64
	FirstOpenTime int64
	LastOpenTime  int64
}

type inventoryRangeRow struct {
	Exchange      string
	Market        string
	Symbol        string
	Interval      string
	KlineRows     uint64
	PhysicalRows  uint64
	FirstOpenTime int64
	LastOpenTime  int64
}

type duplicateRow struct {
	Exchange string
	Market   string
	Symbol   string
	Interval string
	OpenTime int64
	Versions uint64
}

type duplicateSummaryRow struct {
	Exchange           string
	Market             string
	Symbol             string
	Interval           string
	LogicalRows        uint64
	PhysicalRows       uint64
	DuplicateRows      uint64
	DuplicateOpenTimes uint64
	MaxVersions        uint64
}

type tableCount struct {
	Table string
	Rows  uint64
}

type inventoryFilter struct {
	Exchange string
	Market   string
	Symbol   string
	Interval string
}

func newAdminStore(ctx context.Context, cfg config.Config) (*adminStore, error) {
	dialTimeout, err := config.ClickHouseDialTimeout(cfg)
	if err != nil {
		return nil, err
	}
	readTimeout, err := config.ClickHouseReadTimeout(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("clickhouse", clickHouseDSN(cfg, dialTimeout, readTimeout))
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection: %w", err)
	}
	store := &adminStore{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return store, nil
}

func (s *adminStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *adminStore) Inventory(ctx context.Context, filter inventoryFilter) ([]inventoryRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			exchange,
			market,
			symbol,
			interval,
			uniqExact(open_time_ms) AS kline_rows,
			count() AS physical_rows,
			min(open_time_ms) AS first_open_time,
			max(open_time_ms) AS last_open_time
		FROM market_klines
		WHERE (? = '' OR exchange = ?)
			AND (? = '' OR market = ?)
			AND (? = '' OR symbol = ?)
			AND (? = '' OR interval = ?)
		GROUP BY exchange, market, symbol, interval
		ORDER BY
			exchange,
			market,
			symbol,
			multiIf(
				interval = '1m', 1,
				interval = '3m', 2,
				interval = '5m', 3,
				interval = '10m', 4,
				interval = '15m', 5,
				interval = '30m', 6,
				interval = '1h', 7,
				interval = '2h', 8,
				interval = '4h', 9,
				100
			),
			interval
	`,
		filter.Exchange, filter.Exchange,
		filter.Market, filter.Market,
		filter.Symbol, filter.Symbol,
		filter.Interval, filter.Interval,
	)
	if err != nil {
		return nil, fmt.Errorf("query inventory: %w", err)
	}
	defer rows.Close()

	result := []inventoryRow{}
	for rows.Next() {
		var item inventoryRow
		if err := rows.Scan(
			&item.Exchange,
			&item.Market,
			&item.Symbol,
			&item.Interval,
			&item.KlineRows,
			&item.PhysicalRows,
			&item.FirstOpenTime,
			&item.LastOpenTime,
		); err != nil {
			return nil, fmt.Errorf("scan inventory: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inventory: %w", err)
	}
	return result, nil
}

func (s *adminStore) InventoryRange(
	ctx context.Context,
	filter inventoryFilter,
	start int64,
	endExclusive int64,
) ([]inventoryRangeRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			exchange,
			market,
			symbol,
			interval,
			uniqExact(open_time_ms) AS kline_rows,
			count() AS physical_rows,
			min(open_time_ms) AS first_open_time,
			max(open_time_ms) AS last_open_time
		FROM market_klines
		WHERE open_time_ms >= ?
			AND open_time_ms < ?
			AND (? = '' OR exchange = ?)
			AND (? = '' OR market = ?)
			AND (? = '' OR symbol = ?)
			AND (? = '' OR interval = ?)
		GROUP BY exchange, market, symbol, interval
		ORDER BY
			exchange,
			market,
			symbol,
			multiIf(
				interval = '1m', 1,
				interval = '3m', 2,
				interval = '5m', 3,
				interval = '10m', 4,
				interval = '15m', 5,
				interval = '30m', 6,
				interval = '1h', 7,
				interval = '2h', 8,
				interval = '4h', 9,
				100
			),
			interval
	`,
		start,
		endExclusive,
		filter.Exchange, filter.Exchange,
		filter.Market, filter.Market,
		filter.Symbol, filter.Symbol,
		filter.Interval, filter.Interval,
	)
	if err != nil {
		return nil, fmt.Errorf("query inventory range: %w", err)
	}
	defer rows.Close()

	result := []inventoryRangeRow{}
	for rows.Next() {
		var item inventoryRangeRow
		if err := rows.Scan(
			&item.Exchange,
			&item.Market,
			&item.Symbol,
			&item.Interval,
			&item.KlineRows,
			&item.PhysicalRows,
			&item.FirstOpenTime,
			&item.LastOpenTime,
		); err != nil {
			return nil, fmt.Errorf("scan inventory range: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inventory range: %w", err)
	}
	return result, nil
}

func (s *adminStore) DuplicateSummary(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) ([]duplicateSummaryRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			exchange,
			market,
			symbol,
			interval,
			count() AS logical_rows,
			sum(versions) AS physical_rows,
			sum(versions) - count() AS duplicate_rows,
			countIf(versions > 1) AS duplicate_open_times,
			max(versions) AS max_versions
		FROM (
			SELECT
				exchange,
				market,
				symbol,
				interval,
				open_time_ms,
				count() AS versions
			FROM market_klines
			WHERE exchange = ?
				AND market = ?
				AND symbol = ?
				AND interval = ?
				AND open_time_ms >= ?
				AND open_time_ms < ?
			GROUP BY exchange, market, symbol, interval, open_time_ms
		)
		GROUP BY exchange, market, symbol, interval
		ORDER BY duplicate_rows DESC, exchange, market, symbol, interval
	`, exchange, market, symbol, interval, start, endExclusive)
	if err != nil {
		return nil, fmt.Errorf("query duplicate summary: %w", err)
	}
	defer rows.Close()

	result := []duplicateSummaryRow{}
	for rows.Next() {
		var item duplicateSummaryRow
		if err := rows.Scan(
			&item.Exchange,
			&item.Market,
			&item.Symbol,
			&item.Interval,
			&item.LogicalRows,
			&item.PhysicalRows,
			&item.DuplicateRows,
			&item.DuplicateOpenTimes,
			&item.MaxVersions,
		); err != nil {
			return nil, fmt.Errorf("scan duplicate summary: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duplicate summary: %w", err)
	}
	return result, nil
}

func (s *adminStore) DuplicateRows(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
	limit int,
) ([]duplicateRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			exchange,
			market,
			symbol,
			interval,
			open_time_ms,
			count() AS versions
		FROM market_klines
		WHERE exchange = ?
			AND market = ?
			AND symbol = ?
			AND interval = ?
			AND open_time_ms >= ?
			AND open_time_ms < ?
		GROUP BY exchange, market, symbol, interval, open_time_ms
		HAVING versions > 1
		ORDER BY versions DESC, open_time_ms ASC
		LIMIT ?
	`, exchange, market, symbol, interval, start, endExclusive, limit)
	if err != nil {
		return nil, fmt.Errorf("query duplicate rows: %w", err)
	}
	defer rows.Close()

	result := []duplicateRow{}
	for rows.Next() {
		var item duplicateRow
		if err := rows.Scan(
			&item.Exchange,
			&item.Market,
			&item.Symbol,
			&item.Interval,
			&item.OpenTime,
			&item.Versions,
		); err != nil {
			return nil, fmt.Errorf("scan duplicate row: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duplicate rows: %w", err)
	}
	return result, nil
}

func (s *adminStore) ExistingOpenTimes(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) (map[int64]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT open_time_ms
		FROM market_klines FINAL
		WHERE exchange = ?
			AND market = ?
			AND symbol = ?
			AND interval = ?
			AND open_time_ms >= ?
			AND open_time_ms < ?
		ORDER BY open_time_ms ASC
	`, exchange, market, symbol, interval, start, endExclusive)
	if err != nil {
		return nil, fmt.Errorf("query existing open times: %w", err)
	}
	defer rows.Close()

	existing := map[int64]struct{}{}
	for rows.Next() {
		var openTime int64
		if err := rows.Scan(&openTime); err != nil {
			return nil, fmt.Errorf("scan existing open time: %w", err)
		}
		existing[openTime] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing open times: %w", err)
	}
	return existing, nil
}

func (s *adminStore) DeleteCounts(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) ([]tableCount, error) {
	counts := make([]tableCount, 0, len(deleteTables))
	for _, table := range deleteTables {
		count, err := s.countRows(ctx, table, exchange, market, symbol, interval, start, endExclusive)
		if err != nil {
			return nil, err
		}
		counts = append(counts, tableCount{Table: table, Rows: count})
	}
	return counts, nil
}

func (s *adminStore) DeleteRange(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) error {
	for _, table := range deleteTables {
		if err := s.deleteRows(ctx, table, exchange, market, symbol, interval, start, endExclusive); err != nil {
			return err
		}
	}
	return nil
}

func (s *adminStore) countRows(
	ctx context.Context,
	table string,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) (uint64, error) {
	if !validDeleteTable(table) {
		return 0, fmt.Errorf("invalid delete table %q", table)
	}
	var count uint64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT count()
		FROM %s FINAL
		WHERE exchange = ?
			AND market = ?
			AND symbol = ?
			AND interval = ?
			AND open_time_ms >= ?
			AND open_time_ms < ?
	`, table), exchange, market, symbol, interval, start, endExclusive).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count %s rows: %w", table, err)
	}
	return count, nil
}

func (s *adminStore) deleteRows(
	ctx context.Context,
	table string,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	endExclusive int64,
) error {
	if !validDeleteTable(table) {
		return fmt.Errorf("invalid delete table %q", table)
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		ALTER TABLE %s DELETE
		WHERE exchange = ?
			AND market = ?
			AND symbol = ?
			AND interval = ?
			AND open_time_ms >= ?
			AND open_time_ms < ?
	`, table), exchange, market, symbol, interval, start, endExclusive)
	if err != nil {
		return fmt.Errorf("delete %s rows: %w", table, err)
	}
	return nil
}

func clickHouseDSN(cfg config.Config, dialTimeout time.Duration, readTimeout time.Duration) string {
	dsn := url.URL{
		Scheme: "clickhouse",
		Host:   cfg.ClickHouse.Addr,
		Path:   "/" + cfg.ClickHouse.Database,
	}
	if cfg.ClickHouse.Username != "" {
		dsn.User = url.UserPassword(cfg.ClickHouse.Username, cfg.ClickHouse.Password)
	}
	query := dsn.Query()
	query.Set("dial_timeout", dialTimeout.String())
	query.Set("read_timeout", readTimeout.String())
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

var deleteTables = []string{"market_klines"}

func validDeleteTable(table string) bool {
	for _, allowed := range deleteTables {
		if strings.EqualFold(table, allowed) {
			return true
		}
	}
	return false
}
