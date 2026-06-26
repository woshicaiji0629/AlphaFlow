package store

import (
	"encoding/json"
	"testing"

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

func TestRedisStoreSkipsUnchangedLatestPayload(t *testing.T) {
	store := &RedisStore{latestPayloadCache: lcache.MustNew(10)}
	key := model.LastPriceKey("binance", "um", "ETHUSDT")

	if store.shouldSkipLatestWrite(key, []byte(`{"price":"100"}`)) {
		t.Fatal("first payload write was skipped")
	}
	if !store.shouldSkipLatestWrite(key, []byte(`{"price":"100"}`)) {
		t.Fatal("unchanged payload write was not skipped")
	}
	if store.shouldSkipLatestWrite(key, []byte(`{"price":"101"}`)) {
		t.Fatal("changed payload write was skipped")
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
