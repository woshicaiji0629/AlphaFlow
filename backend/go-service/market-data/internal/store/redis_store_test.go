package store

import (
	"encoding/json"
	"testing"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/lcache"
)

func TestGroupKlineHashUpdatesKeepsLastOpenTimePayload(t *testing.T) {
	updates, err := groupKlineHashUpdates([]model.Kline{
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "100",
		},
		{
			Exchange: "binance",
			Market:   "um",
			Symbol:   "ETHUSDT",
			Interval: "1m",
			OpenTime: 1000,
			Close:    "101",
		},
	})
	if err != nil {
		t.Fatalf("groupKlineHashUpdates: %v", err)
	}

	update := updates[model.RedisKey("binance", "um", "ETHUSDT", "1m")]
	if len(update.fields) != 1 || update.fields[0] != "1000" {
		t.Fatalf("fields = %#v, want [1000]", update.fields)
	}
	if len(update.hashValues) != 2 || update.hashValues[0] != "1000" {
		t.Fatalf("hashValues = %#v, want field 1000", update.hashValues)
	}
	if len(update.indexMembers) != 1 || update.indexMembers[0].Member != "1000" {
		t.Fatalf("indexMembers = %#v, want member 1000", update.indexMembers)
	}
	if update.indexMembers[0].Score != 1000 {
		t.Fatalf("index score = %v, want 1000", update.indexMembers[0].Score)
	}

	payload, ok := update.hashValues[1].([]byte)
	if !ok {
		t.Fatalf("payload type = %T, want []byte", update.hashValues[1])
	}
	var kline model.Kline
	if err := json.Unmarshal(payload, &kline); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	if kline.Close != "101" {
		t.Fatalf("kline close = %q, want 101", kline.Close)
	}
}

func TestKlineHashKeys(t *testing.T) {
	baseKey := model.RedisKey("binance", "um", "ETHUSDT", "1m")

	if got, want := klineDataKey(baseKey), baseKey+":data"; got != want {
		t.Fatalf("data key = %q, want %q", got, want)
	}
	if got, want := klineIndexKey(baseKey), baseKey+":idx"; got != want {
		t.Fatalf("index key = %q, want %q", got, want)
	}
}

func TestKlineTrimStopRank(t *testing.T) {
	if got := klineTrimStopRank(200); got != -201 {
		t.Fatalf("trim stop rank = %d, want -201", got)
	}
}

func TestIndicatorWindowHashFieldsIncludeBarFreshness(t *testing.T) {
	fields, err := indicatorWindowHashFields(model.IndicatorWindowSnapshot{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "15m",
		OpenTime:  900000,
		CloseTime: 1799999,
		Version:   "v1",
		Values:    map[string]string{"pump_window_score": "80"},
		Signals:   map[string]string{"pump_window_signal": "true"},
		UpdatedAt: 1801000,
	}, 24*time.Hour)
	if err != nil {
		t.Fatalf("indicatorWindowHashFields: %v", err)
	}
	got := hashFieldsMap(fields)
	assertHashField(t, got, "meta:snapshot_type", "window")
	assertHashField(t, got, "meta:bar_open_time", "900000")
	assertHashField(t, got, "meta:bar_close_time", "1799999")
	assertHashField(t, got, "meta:bar_interval_ms", "900000")
	assertHashField(t, got, "meta:bar_seq", "1")
	assertHashField(t, got, "meta:age_limit_ms", "1800000")
	assertHashField(t, got, "meta:ttl_seconds", "86400")
	assertHashField(t, got, "value:pump_window_score", "80")
	assertHashField(t, got, "signal:pump_window_signal", "true")
}

func TestIndicatorRealtimeHashFieldsIncludeBarFreshness(t *testing.T) {
	fields, err := indicatorRealtimeHashFields(model.IndicatorRealtimeSnapshot{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "3m",
		OpenTime:  180000,
		CloseTime: 359999,
		Kline: model.Kline{
			OpenTime:  180000,
			CloseTime: 359999,
			Close:     "100",
			IsClosed:  false,
		},
		Values:    map[string]string{"rsi14": "55"},
		Signals:   map[string]string{"ema_alignment": "bull"},
		UpdatedAt: 181000,
	}, 24*time.Hour)
	if err != nil {
		t.Fatalf("indicatorRealtimeHashFields: %v", err)
	}
	got := hashFieldsMap(fields)
	assertHashField(t, got, "meta:snapshot_type", "realtime")
	assertHashField(t, got, "meta:bar_open_time", "180000")
	assertHashField(t, got, "meta:bar_close_time", "359999")
	assertHashField(t, got, "meta:bar_interval_ms", "180000")
	assertHashField(t, got, "meta:bar_seq", "1")
	assertHashField(t, got, "meta:age_limit_ms", "15000")
	assertHashField(t, got, "meta:ttl_seconds", "86400")
	assertHashField(t, got, "kline:is_closed", "false")
	assertHashField(t, got, "value:rsi14", "55")
	assertHashField(t, got, "signal:ema_alignment", "bull")
}

func TestRedisStoreSkipsUnchangedWebSocketStatusPayload(t *testing.T) {
	store := &RedisStore{webSocketStatusCache: lcache.MustNew(10)}
	key := model.WebSocketStatusKey("binance", "um")

	if store.shouldSkipWebSocketStatusWrite(key, []byte(`{"available":true}`)) {
		t.Fatal("first payload write was skipped")
	}
	if !store.shouldSkipWebSocketStatusWrite(key, []byte(`{"available":true}`)) {
		t.Fatal("unchanged payload write was not skipped")
	}
	if store.shouldSkipWebSocketStatusWrite(key, []byte(`{"available":false}`)) {
		t.Fatal("changed payload write was skipped")
	}
}

func hashFieldsMap(fields []interface{}) map[string]string {
	values := make(map[string]string, len(fields)/2)
	for index := 0; index+1 < len(fields); index += 2 {
		key, ok := fields[index].(string)
		if !ok {
			continue
		}
		value, ok := fields[index+1].(string)
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func assertHashField(t *testing.T, fields map[string]string, key string, want string) {
	t.Helper()
	if got := fields[key]; got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func TestRedisStoreMaintainIndicatorKeysDeduplicatesByKey(t *testing.T) {
	store := &RedisStore{indicatorMaintenance: lcache.MustNew(10)}
	callsByKey := map[string]int{}

	store.maintainIndicatorKeys([]string{"indicator:key", "indicator:last"}, func(key string) {
		callsByKey[key]++
	})
	store.maintainIndicatorKeys([]string{"indicator:key", "indicator:window"}, func(key string) {
		callsByKey[key]++
	})

	if got := callsByKey["indicator:key"]; got != 1 {
		t.Fatalf("indicator:key calls = %d, want 1", got)
	}
	if got := callsByKey["indicator:last"]; got != 1 {
		t.Fatalf("indicator:last calls = %d, want 1", got)
	}
	if got := callsByKey["indicator:window"]; got != 1 {
		t.Fatalf("indicator:window calls = %d, want 1", got)
	}
}

func TestRedisStoreMaintainLiquidationKeyUsesFreqCall(t *testing.T) {
	store := &RedisStore{liquidationMaintenance: lcache.MustNew(10)}
	calls := 0

	store.maintainLiquidationKey("liquidation:key", func() {
		calls++
	})
	store.maintainLiquidationKey("liquidation:key", func() {
		calls++
	})
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	store.maintainLiquidationKey("liquidation:other", func() {
		calls++
	})
	if calls != 2 {
		t.Fatalf("calls after different key = %d, want 2", calls)
	}
}
