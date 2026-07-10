package config

import (
	"fmt"
	"time"
)

func KlineLimit() int64 {
	return IndicatorKlineCacheLimit()
}

func KlineTTL() time.Duration {
	return 24 * time.Hour
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
	return IndicatorKlineCacheLimit()
}

func IndicatorWarmupKlines() int64 {
	return 250
}

func IndicatorWindowLookback() int64 {
	return 50
}

func IndicatorCacheBuffer() int64 {
	return 10
}

func IndicatorKlineCacheLimit() int64 {
	return IndicatorWarmupKlines() + IndicatorWindowLookback() + IndicatorCacheBuffer()
}

func IndicatorSnapshotCacheLimit() int {
	return int(IndicatorWindowLookback() + IndicatorCacheBuffer())
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

func IndicatorQueueAckWait(cfg Config) (time.Duration, error) {
	if cfg.Indicator.AckWait == "" {
		return 30 * time.Second, nil
	}
	wait, err := time.ParseDuration(cfg.Indicator.AckWait)
	if err != nil {
		return 0, fmt.Errorf("parse indicator_queue ack_wait: %w", err)
	}
	if wait <= 0 {
		return 0, fmt.Errorf("indicator_queue ack_wait must be positive")
	}
	return wait, nil
}

func IndicatorQueueWorkerMaxWait(cfg Config) (time.Duration, error) {
	if cfg.Indicator.WorkerMaxWait == "" {
		return time.Second, nil
	}
	wait, err := time.ParseDuration(cfg.Indicator.WorkerMaxWait)
	if err != nil {
		return 0, fmt.Errorf("parse indicator_queue worker_max_wait: %w", err)
	}
	if wait <= 0 {
		return 0, fmt.Errorf("indicator_queue worker_max_wait must be positive")
	}
	return wait, nil
}

func MarketBusDefaultTTL(cfg Config) (time.Duration, error) {
	if cfg.MarketBus.DefaultTTL == "" {
		return 30 * time.Second, nil
	}
	ttl, err := time.ParseDuration(cfg.MarketBus.DefaultTTL)
	if err != nil {
		return 0, fmt.Errorf("parse market_bus default_ttl: %w", err)
	}
	if ttl <= 0 {
		return 0, fmt.Errorf("market_bus default_ttl must be positive")
	}
	return ttl, nil
}
