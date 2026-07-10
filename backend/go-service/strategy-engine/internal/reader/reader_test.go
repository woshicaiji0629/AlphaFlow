package reader

import (
	"context"
	"strings"
	"testing"

	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/strategy"
)

type fakeHashReader struct {
	hashes map[string]map[string]string
}

type fakeStringReader struct {
	values map[string]string
}

func (r fakeHashReader) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.hashes[key], nil
}

func (r fakeStringReader) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, ok := r.values[key]
	if !ok {
		return "", redisNil{}
	}
	return value, nil
}

type redisNil struct{}

func (redisNil) Error() string {
	return "redis: nil"
}

func TestReaderReadBuildsStrategyContext(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":            "1000",
				"meta:close_time":           "2000",
				"meta:age_limit_ms":         "6000",
				"meta:version":              "v1",
				"meta:updated_at":           "3000",
				"value:window_sample_count": "20",
				"value:macd_win_latest":     "1.5",
				"value:macd_win_previous":   "1.2",
				"value:macd_win_slope":      "0.3",
				"signal:trend_valid":        "true",
				"signal:ma_win_latest":      "bull",
				"signal:ma_win_previous":    "neutral",
			},
			marketkeys.IndicatorRealtimeKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":                   "2000",
				"meta:close_time":                  "3000",
				"meta:age_limit_ms":                "6000",
				"meta:updated_at":                  "3500",
				"kline:open_time":                  "2000",
				"kline:close_time":                 "3000",
				"kline:open":                       "100",
				"kline:high":                       "110",
				"kline:low":                        "95",
				"kline:close":                      "105",
				"kline:volume":                     "10",
				"kline:quote_volume":               "1000",
				"kline:trade_count":                "7",
				"kline:taker_buy_volume":           "4",
				"kline:taker_buy_quote_volume":     "400",
				"kline:is_closed":                  "false",
				"value:last_price":                 "106",
				"value:mark_price":                 "105.5",
				"signal:current_supertrend_signal": "up",
			},
		}},
		Now: func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := reader.Read(context.Background(), target, nil)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	snapshot := got.Snapshots["3m"]
	if snapshot.Current.Close != "105" {
		t.Fatalf("current close = %q, want 105", snapshot.Current.Close)
	}
	if snapshot.Price.LastPrice != "106" {
		t.Fatalf("last price = %q, want 106", snapshot.Price.LastPrice)
	}
	if snapshot.Window.SampleCount != 20 {
		t.Fatalf("sample count = %d, want 20", snapshot.Window.SampleCount)
	}
	if snapshot.Window.Values["macd"].Latest != 1.5 {
		t.Fatalf("macd latest = %v, want 1.5", snapshot.Window.Values["macd"].Latest)
	}
	if snapshot.Window.Signals["trend_valid"].Latest != "true" {
		t.Fatalf("trend_valid = %q, want true", snapshot.Window.Signals["trend_valid"].Latest)
	}
	if snapshot.Window.Signals["ma"].Previous != "neutral" {
		t.Fatalf("ma previous = %q, want neutral", snapshot.Window.Signals["ma"].Previous)
	}
	if snapshot.Realtime == nil || snapshot.Realtime.Indicator.Values["mark_price"] != "105.5" {
		t.Fatalf("realtime indicator = %#v, want mark price 105.5", snapshot.Realtime)
	}
	if len(snapshot.Timeframes) != 1 {
		t.Fatalf("len(timeframes) = %d, want 1", len(snapshot.Timeframes))
	}
	if snapshot.Timeframes["3m"].Window.Signals["trend_valid"].Latest != "true" {
		t.Fatalf("timeframe trend_valid = %q, want true", snapshot.Timeframes["3m"].Window.Signals["trend_valid"].Latest)
	}
}

func TestReaderReadAllowsConfirmIntervalWithoutRealtime(t *testing.T) {
	target := strategy.Target{
		Scope:    strategy.PositionScopePaper,
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "1000",
				"meta:close_time":   "2000",
				"meta:age_limit_ms": "6000",
				"meta:updated_at":   "3000",
			},
			marketkeys.IndicatorRealtimeKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "2000",
				"meta:close_time":   "3000",
				"meta:age_limit_ms": "6000",
				"meta:updated_at":   "3500",
				"kline:close":       "105",
			},
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "5m"): {
				"meta:open_time":        "1000",
				"meta:close_time":       "2000",
				"meta:age_limit_ms":     "10000",
				"meta:updated_at":       "3000",
				"signal:trend_valid":    "true",
				"signal:ma_window_bias": "bull",
			},
		}},
		Now: func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := reader.Read(context.Background(), target, []string{"5m"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Snapshots["5m"].Current.Close != "" {
		t.Fatalf("confirm current close = %q, want empty", got.Snapshots["5m"].Current.Close)
	}
	if got.Snapshots["3m"].Current.Close != "105" {
		t.Fatalf("entry current close = %q, want 105", got.Snapshots["3m"].Current.Close)
	}
	if got.Snapshots["3m"].Timeframes["5m"].Window.Signals["ma_window_bias"].Latest != "bull" {
		t.Fatalf("5m timeframe ma bias not populated")
	}
}

func TestReaderReadRequiresEntryRealtime(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "1000",
				"meta:close_time":   "2000",
				"meta:age_limit_ms": "6000",
				"meta:updated_at":   "3000",
			},
		}},
		Now: func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reader.Read(context.Background(), target, nil)
	if err == nil || !strings.Contains(err.Error(), "indrt") {
		t.Fatalf("Read() error = %v, want entry realtime missing error", err)
	}
}

func TestReaderReadReturnsMissingHashError(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{}},
		Now:    func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reader.Read(context.Background(), target, nil)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Read() error = %v, want missing hash error", err)
	}
}

func TestReaderReadReturnsStaleSnapshotError(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "1000",
				"meta:close_time":   "2000",
				"meta:age_limit_ms": "1000",
				"meta:updated_at":   "2000",
			},
		}},
		Now: func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reader.Read(context.Background(), target, nil)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Read() error = %v, want stale snapshot error", err)
	}
}

func TestReaderReadReturnsUnhealthyIndicatorError(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "3000",
				"meta:close_time":   "4000",
				"meta:age_limit_ms": "6000",
				"meta:updated_at":   "3500",
			},
			marketkeys.IndicatorRealtimeKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":    "4000",
				"meta:close_time":   "5000",
				"meta:age_limit_ms": "6000",
				"meta:updated_at":   "4500",
			},
		}},
		Strings: fakeStringReader{values: map[string]string{
			marketkeys.DataHealthKey("binance", "um", "ETHUSDT", "3m"): `{"kline_status":"ok","indicator_status":"stale","last_kline_open_time":4000,"last_indicator_open_time":2000,"reason":"indicator stale","updated_at":4500}`,
		}},
		Now: func() int64 { return 5000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reader.Read(context.Background(), target, nil)
	if err == nil || !strings.Contains(err.Error(), "not ok") {
		t.Fatalf("Read() error = %v, want health not ok error", err)
	}
}

func TestReaderReadReturnsInvalidNumericError(t *testing.T) {
	target := strategy.Target{
		Exchange: "binance",
		Market:   "um",
		Symbol:   "ETHUSDT",
		Interval: "3m",
	}
	reader, err := New(Options{
		Hashes: fakeHashReader{hashes: map[string]map[string]string{
			marketkeys.IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"): {
				"meta:open_time":        "1000",
				"meta:close_time":       "2000",
				"meta:age_limit_ms":     "6000",
				"meta:updated_at":       "3000",
				"value:macd_win_latest": "bad",
			},
		}},
		Now: func() int64 { return 4000 },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reader.Read(context.Background(), target, nil)
	if err == nil || !strings.Contains(err.Error(), "macd_win_latest") {
		t.Fatalf("Read() error = %v, want invalid numeric error", err)
	}
}
