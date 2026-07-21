package signalresearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketregime"

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
		`ALTER TABLE supertrend_signal_research ADD COLUMN IF NOT EXISTS trigger_metadata_json String AFTER trigger_sources`,
		`CREATE TABLE IF NOT EXISTS supertrend_signal_validation_observations (
			run_id String,
			signal_id String,
			observation_bars UInt8,
			observation_time_ms Int64,
			max_favorable_bps Float64,
			max_adverse_bps Float64,
			close_return_bps Float64,
			signal_structure_held Bool,
			confirmation_5m LowCardinality(String),
			created_at_ms Int64
		) ENGINE = ReplacingMergeTree(created_at_ms)
		ORDER BY (run_id, signal_id, observation_bars)`,
		`CREATE TABLE IF NOT EXISTS supertrend_chop_observations (
			run_id String,
			bar_close_time_ms Int64,
			state LowCardinality(String),
			efficiency_ratio Float64,
			supertrend_flips_10m UInt8,
			adx_10m Float64,
			normalized_slope_10m Float64,
			range_atr_10m Float64,
			evidence_votes UInt8,
			platform_high Float64,
			platform_low Float64,
			failed_breakouts UInt16
		) ENGINE = ReplacingMergeTree
		ORDER BY (run_id, bar_close_time_ms)`,
		`CREATE TABLE IF NOT EXISTS market_regime_observations (
			run_id String,
			bar_close_time_ms Int64,
			state LowCardinality(String),
			direction LowCardinality(String),
			allow_new_position Bool,
			dormant_intervals UInt8,
			lock_reason LowCardinality(String),
			efficiency_ratio Float64,
			platform_range_atr Float64,
			platform_high Float64,
			platform_low Float64,
			failed_breakouts UInt16,
			state_bars UInt16,
			evidence_json String
		) ENGINE = ReplacingMergeTree
		ORDER BY (run_id, bar_close_time_ms)`,
		`ALTER TABLE market_regime_observations ADD COLUMN IF NOT EXISTS lock_reason LowCardinality(String) AFTER dormant_intervals`,
		`CREATE TABLE IF NOT EXISTS market_swings (
			swing_id String,
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			interval LowCardinality(String),
			definition_version LowCardinality(String),
			minimum_move_points Float64,
			reversal_points Float64,
			start_time_ms Int64,
			end_time_ms Int64,
			side LowCardinality(String),
			start_price Float64,
			end_price Float64,
			move_points Float64,
			move_bucket LowCardinality(String),
			move_pct Float64,
			duration_minutes Float64,
			created_at_ms Int64
		) ENGINE = ReplacingMergeTree(created_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(start_time_ms))
		ORDER BY (exchange, market, symbol, interval, definition_version,
			minimum_move_points, reversal_points, start_time_ms, end_time_ms, side, swing_id)`,
		`CREATE TABLE IF NOT EXISTS market_analysis_observations (
			exchange LowCardinality(String),
			market LowCardinality(String),
			symbol LowCardinality(String),
			interval LowCardinality(String),
			analysis_name LowCardinality(String),
			analysis_version LowCardinality(String),
			config_fingerprint String,
			bar_close_time_ms Int64,
			state LowCardinality(String),
			direction LowCardinality(String),
			allow_long Bool,
			allow_short Bool,
			trendability_score Float64,
			direction_score Float64,
			confidence Float64,
			lock_reason LowCardinality(String),
			state_bars UInt16,
			reasons_json String,
			evidence_json String,
			created_at_ms Int64
		) ENGINE = ReplacingMergeTree(created_at_ms)
		PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(bar_close_time_ms))
		ORDER BY (exchange, market, symbol, interval, analysis_name,
			analysis_version, config_fingerprint, bar_close_time_ms)`,
	}
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("initialize signal research schema: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveMarketAnalysisObservations(ctx context.Context, exchange, market, symbol, interval, fingerprint string, items []marketregime.Result, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	createdAtMS := time.Now().UnixMilli()
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*20)
		for _, item := range items[start:end] {
			reasonsJSON, err := json.Marshal(item.Reasons)
			if err != nil {
				return fmt.Errorf("marshal market analysis reasons: %w", err)
			}
			evidenceJSON, err := json.Marshal(item.Evidence)
			if err != nil {
				return fmt.Errorf("marshal market analysis evidence: %w", err)
			}
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, exchange, market, symbol, interval, "market_regime", string(item.Version), fingerprint,
				item.BarCloseTimeMS, string(item.State), string(item.Direction), item.AllowLong, item.AllowShort,
				item.TrendabilityScore, item.DirectionScore, item.Confidence, item.LockReason, item.StateBars,
				string(reasonsJSON), string(evidenceJSON), createdAtMS)
		}
		query := `INSERT INTO market_analysis_observations (
			exchange, market, symbol, interval, analysis_name, analysis_version, config_fingerprint,
			bar_close_time_ms, state, direction, allow_long, allow_short,
			trendability_score, direction_score, confidence, lock_reason, state_bars,
			reasons_json, evidence_json, created_at_ms
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert market analysis observations: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveMarketSwings(ctx context.Context, items []MarketSwing, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 500
	}
	createdAtMS := time.Now().UnixMilli()
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*18)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.SwingID, item.Exchange, item.Market, item.Symbol, item.Interval,
				item.DefinitionVersion, item.MinimumMovePoints, item.ReversalPoints, item.StartTimeMS, item.EndTimeMS,
				string(item.Side), item.StartPrice, item.EndPrice, item.MovePoints, item.MoveBucket,
				item.MovePct, item.DurationMinutes, createdAtMS)
		}
		query := `INSERT INTO market_swings (
			swing_id, exchange, market, symbol, interval, definition_version,
			minimum_move_points, reversal_points, start_time_ms, end_time_ms, side,
			start_price, end_price, move_points, move_bucket, move_pct, duration_minutes, created_at_ms
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert market swings: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveMarketRegimeObservations(ctx context.Context, runID string, items []marketregime.Result, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*14)
		for _, item := range items[start:end] {
			evidenceJSON, err := json.Marshal(item.Evidence)
			if err != nil {
				return fmt.Errorf("marshal market regime evidence: %w", err)
			}
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, runID, item.BarCloseTimeMS, string(item.State), string(item.Direction),
				item.AllowNewPosition, item.DormantIntervals, item.LockReason, item.EfficiencyRatio, item.PlatformRangeATR,
				item.PlatformHigh, item.PlatformLow, item.FailedBreakouts, item.StateBars, string(evidenceJSON))
		}
		query := `INSERT INTO market_regime_observations (
			run_id, bar_close_time_ms, state, direction, allow_new_position, dormant_intervals, lock_reason,
			efficiency_ratio, platform_range_atr, platform_high, platform_low,
			failed_breakouts, state_bars, evidence_json
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert market regime observations: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveChopObservations(ctx context.Context, items []ChopObservation, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*12)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.RunID, item.BarCloseTimeMS, item.State, item.EfficiencyRatio,
				item.SupertrendFlips10M, item.ADX10M, item.NormalizedSlope10M, item.RangeATR10M,
				item.EvidenceVotes, item.PlatformHigh, item.PlatformLow, item.FailedBreakouts)
		}
		query := `INSERT INTO supertrend_chop_observations (
			run_id, bar_close_time_ms, state, efficiency_ratio, supertrend_flips_10m,
			adx_10m, normalized_slope_10m, range_atr_10m, evidence_votes,
			platform_high, platform_low, failed_breakouts
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert chop observations: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveValidationObservations(ctx context.Context, items []ValidationObservation, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*10)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.RunID, item.SignalID, item.ObservationBars, item.ObservationTimeMS,
				item.MaxFavorableBps, item.MaxAdverseBps, item.CloseReturnBps, item.SignalStructureHeld,
				item.Confirmation5M, item.CreatedAtMS)
		}
		query := `INSERT INTO supertrend_signal_validation_observations (
			run_id, signal_id, observation_bars, observation_time_ms, max_favorable_bps,
			max_adverse_bps, close_return_bps, signal_structure_held, confirmation_5m, created_at_ms
		) VALUES ` + strings.Join(rows, ", ")
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert signal validation observations: %w", err)
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
		args := make([]any, 0, (end-start)*17)
		for _, item := range items[start:end] {
			rows = append(rows, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, item.RunID, item.SignalID, item.Exchange, item.Market, item.Symbol, item.Interval,
				string(item.Side), item.TriggerSources, item.TriggerMetadataJSON, item.SignalTimeMS, item.SignalBarOpenMS, item.EntryPrice,
				item.ATR, item.HorizonMinutes, item.FeatureVersion, item.FeatureSnapshotJSON, item.CreatedAtMS)
		}
		query := `INSERT INTO supertrend_signal_research (
			run_id, signal_id, exchange, market, symbol, interval, side, trigger_sources, trigger_metadata_json,
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
