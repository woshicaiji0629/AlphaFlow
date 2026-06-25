package model

import "testing"

func TestIntervalMillis(t *testing.T) {
	tests := map[string]int64{
		"1m":  60000,
		"3m":  180000,
		"5m":  300000,
		"15m": 900000,
		"30m": 1800000,
		"1h":  3600000,
		"2h":  7200000,
		"4h":  14400000,
	}

	for interval, want := range tests {
		got, err := IntervalMillis(interval)
		if err != nil {
			t.Fatalf("IntervalMillis(%q): %v", interval, err)
		}
		if got != want {
			t.Fatalf("IntervalMillis(%q) = %d, want %d", interval, got, want)
		}
	}
}

func TestIntervalMillisRejectsUnsupportedInterval(t *testing.T) {
	if _, err := IntervalMillis("10m"); err == nil {
		t.Fatal("expected unsupported 10m interval")
	}
}

func TestRedisKey(t *testing.T) {
	got := RedisKey("binance", "um", "ETHUSDT", "3m")
	want := "bn:um:k:ETHUSDT:3m"
	if got != want {
		t.Fatalf("RedisKey() = %q, want %q", got, want)
	}
}
