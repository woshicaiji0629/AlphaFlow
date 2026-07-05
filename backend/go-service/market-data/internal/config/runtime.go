package config

import (
	"fmt"
	"time"
)

func KlineLimit() int64 {
	return 300
}

func KlineTTL() time.Duration {
	return 7 * 24 * time.Hour
}

func LiquidationLimit() int64 {
	return 200
}

func LiquidationTTL() time.Duration {
	return 24 * time.Hour
}

func LatestTTL() time.Duration {
	return 24 * time.Hour
}

func PollingTTL() time.Duration {
	return 24 * time.Hour
}

func AggregationScanInterval() time.Duration {
	return 10 * time.Second
}

func IndicatorScanInterval() time.Duration {
	return 10 * time.Second
}

func HealthScanInterval() time.Duration {
	return 10 * time.Second
}

func HealthGapLookback() int64 {
	return 5
}

func IndicatorLookbackPeriods() int64 {
	return KlineLimit()
}

func ClickHouseDialTimeout(cfg Config) (time.Duration, error) {
	if cfg.ClickHouse.DialTimeout == "" {
		return 5 * time.Second, nil
	}
	timeout, err := time.ParseDuration(cfg.ClickHouse.DialTimeout)
	if err != nil {
		return 0, fmt.Errorf("parse clickhouse dial_timeout: %w", err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("clickhouse dial_timeout must be positive")
	}
	return timeout, nil
}

func ClickHouseReadTimeout(cfg Config) (time.Duration, error) {
	if cfg.ClickHouse.ReadTimeout == "" {
		return 30 * time.Second, nil
	}
	timeout, err := time.ParseDuration(cfg.ClickHouse.ReadTimeout)
	if err != nil {
		return 0, fmt.Errorf("parse clickhouse read_timeout: %w", err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("clickhouse read_timeout must be positive")
	}
	return timeout, nil
}

func ClickHouseRetryInterval(cfg Config) (time.Duration, error) {
	if cfg.ClickHouse.RetryInterval == "" {
		return 10 * time.Second, nil
	}
	interval, err := time.ParseDuration(cfg.ClickHouse.RetryInterval)
	if err != nil {
		return 0, fmt.Errorf("parse clickhouse retry_interval: %w", err)
	}
	if interval <= 0 {
		return 0, fmt.Errorf("clickhouse retry_interval must be positive")
	}
	return interval, nil
}

func ClickHousePendingAckWait(cfg Config) (time.Duration, error) {
	if cfg.ClickHouse.PendingAckWait == "" {
		return 30 * time.Second, nil
	}
	wait, err := time.ParseDuration(cfg.ClickHouse.PendingAckWait)
	if err != nil {
		return 0, fmt.Errorf("parse clickhouse pending_ack_wait: %w", err)
	}
	if wait <= 0 {
		return 0, fmt.Errorf("clickhouse pending_ack_wait must be positive")
	}
	return wait, nil
}

func BackfillAckWait(cfg Config) (time.Duration, error) {
	if cfg.Backfill.AckWait == "" {
		return 30 * time.Minute, nil
	}
	wait, err := time.ParseDuration(cfg.Backfill.AckWait)
	if err != nil {
		return 0, fmt.Errorf("parse backfill_queue ack_wait: %w", err)
	}
	if wait <= 0 {
		return 0, fmt.Errorf("backfill_queue ack_wait must be positive")
	}
	return wait, nil
}

func BackfillWorkerMaxWait(cfg Config) (time.Duration, error) {
	if cfg.Backfill.WorkerMaxWait == "" {
		return time.Second, nil
	}
	wait, err := time.ParseDuration(cfg.Backfill.WorkerMaxWait)
	if err != nil {
		return 0, fmt.Errorf("parse backfill_queue worker_max_wait: %w", err)
	}
	if wait <= 0 {
		return 0, fmt.Errorf("backfill_queue worker_max_wait must be positive")
	}
	return wait, nil
}
