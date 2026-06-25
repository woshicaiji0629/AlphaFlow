package config

import (
	"fmt"
	"time"
)

func KlineLimit() int64 {
	return 500
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

func IndicatorLookbackPeriods() int64 {
	return 200
}

func ReconnectDelay(cfg Config) (time.Duration, error) {
	if cfg.WebSocket.ReconnectDelay == "" {
		return ReconnectDelayFallback(), nil
	}
	delay, err := time.ParseDuration(cfg.WebSocket.ReconnectDelay)
	if err != nil {
		return 0, fmt.Errorf("parse reconnect_delay: %w", err)
	}
	if delay <= 0 {
		return 0, fmt.Errorf("reconnect_delay must be positive")
	}
	return delay, nil
}

func ReconnectDelayFallback() time.Duration {
	return 5 * time.Second
}
