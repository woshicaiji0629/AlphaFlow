package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"github.com/redis/go-redis/v9"
)

const trimIndicatorHistoryScript = `
local old = redis.call("ZRANGE", KEYS[1], 0, ARGV[1])
if #old > 0 then
	redis.call("HDEL", KEYS[2], unpack(old))
end
redis.call("ZREMRANGEBYRANK", KEYS[1], 0, ARGV[1])
local ttl = tonumber(ARGV[2])
if ttl > 0 then
	redis.call("PEXPIRE", KEYS[1], ttl)
	redis.call("PEXPIRE", KEYS[2], ttl)
end
return #old
`

func (s *RedisStore) SetIndicator(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal indicator: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set indicator: %w", err)
	}
	return nil
}

func (s *RedisStore) SetIndicatorWithOpenTime(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal indicator: %w", err)
	}
	indicatorKey := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	lastKey := model.IndicatorLastKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	historyIndexKey := indicatorHistoryIndexKey(snapshot)
	historyDataKey := indicatorHistoryDataKey(snapshot)
	historyField := strconv.FormatInt(snapshot.OpenTime, 10)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, indicatorKey, payload, s.retention.LatestTTL)
	pipe.Set(ctx, lastKey, strconv.FormatInt(snapshot.OpenTime, 10), s.retention.LatestTTL)
	pipe.HSet(ctx, historyDataKey, historyField, payload)
	pipe.ZAdd(ctx, historyIndexKey, redis.Z{Score: float64(snapshot.OpenTime), Member: historyField})
	s.maintainIndicatorKeys([]string{indicatorKey, lastKey, historyIndexKey, historyDataKey}, func(key string) {
		pipe.Expire(ctx, key, s.retention.LatestTTL)
	})
	s.maintainIndicatorKey(historyIndexKey+":trim", func() {
		pipe.Eval(ctx, trimIndicatorHistoryScript, []string{historyIndexKey, historyDataKey}, indicatorHistoryTrimStopRank(s.retention.IndicatorLimit), s.retention.LatestTTL.Milliseconds())
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set indicator: %w", err)
	}
	return nil
}

func (s *RedisStore) SetIndicatorWindowWithOpenTime(
	ctx context.Context,
	snapshot model.IndicatorWindowSnapshot,
) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	windowKey := model.IndicatorWindowKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	lastKey := model.IndicatorWindowLastKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	fields, err := indicatorWindowHashFields(snapshot, s.retention.LatestTTL)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, windowKey, fields...)
	pipe.Set(ctx, lastKey, strconv.FormatInt(snapshot.OpenTime, 10), s.retention.LatestTTL)
	s.maintainIndicatorKeys([]string{windowKey, lastKey}, func(key string) {
		pipe.Expire(ctx, key, s.retention.LatestTTL)
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set indicator window: %w", err)
	}
	return nil
}

func (s *RedisStore) SetClosedIndicator(
	ctx context.Context,
	snapshot model.IndicatorSnapshot,
	windowSnapshot model.IndicatorWindowSnapshot,
) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal indicator: %w", err)
	}
	fields, err := indicatorWindowHashFields(windowSnapshot, s.retention.LatestTTL)
	if err != nil {
		return err
	}

	indicatorKey := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	indicatorLastKey := model.IndicatorLastKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	historyIndexKey := indicatorHistoryIndexKey(snapshot)
	historyDataKey := indicatorHistoryDataKey(snapshot)
	historyField := strconv.FormatInt(snapshot.OpenTime, 10)
	windowKey := model.IndicatorWindowKey(windowSnapshot.Exchange, windowSnapshot.Market, windowSnapshot.Symbol, windowSnapshot.Interval)
	windowLastKey := model.IndicatorWindowLastKey(windowSnapshot.Exchange, windowSnapshot.Market, windowSnapshot.Symbol, windowSnapshot.Interval)

	pipe := s.client.Pipeline()
	pipe.Set(ctx, indicatorKey, payload, s.retention.LatestTTL)
	pipe.Set(ctx, indicatorLastKey, strconv.FormatInt(snapshot.OpenTime, 10), s.retention.LatestTTL)
	pipe.HSet(ctx, historyDataKey, historyField, payload)
	pipe.ZAdd(ctx, historyIndexKey, redis.Z{Score: float64(snapshot.OpenTime), Member: historyField})
	pipe.HSet(ctx, windowKey, fields...)
	pipe.Set(ctx, windowLastKey, strconv.FormatInt(windowSnapshot.OpenTime, 10), s.retention.LatestTTL)
	s.maintainIndicatorKeys([]string{indicatorKey, indicatorLastKey, historyIndexKey, historyDataKey, windowKey, windowLastKey}, func(key string) {
		pipe.Expire(ctx, key, s.retention.LatestTTL)
	})
	s.maintainIndicatorKey(historyIndexKey+":trim", func() {
		pipe.Eval(ctx, trimIndicatorHistoryScript, []string{historyIndexKey, historyDataKey}, indicatorHistoryTrimStopRank(s.retention.IndicatorLimit), s.retention.LatestTTL.Milliseconds())
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set closed indicator: %w", err)
	}
	return nil
}

func (s *RedisStore) SetIndicatorRealtime(
	ctx context.Context,
	snapshot model.IndicatorRealtimeSnapshot,
) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.IndicatorRealtimeKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	fields, err := indicatorRealtimeHashFields(snapshot, s.retention.LatestTTL)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, fields...)
	s.maintainIndicatorKey(key, func() {
		pipe.Expire(ctx, key, s.retention.LatestTTL)
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set indicator realtime: %w", err)
	}
	return nil
}

func indicatorWindowHashFields(
	snapshot model.IndicatorWindowSnapshot,
	ttl time.Duration,
) ([]interface{}, error) {
	intervalMillis, err := model.IntervalMillis(snapshot.Interval)
	if err != nil {
		return nil, err
	}
	fields := []interface{}{
		"meta:snapshot_type", "window",
		"meta:exchange", snapshot.Exchange,
		"meta:market", snapshot.Market,
		"meta:symbol", snapshot.Symbol,
		"meta:interval", snapshot.Interval,
		"meta:open_time", strconv.FormatInt(snapshot.OpenTime, 10),
		"meta:close_time", strconv.FormatInt(snapshot.CloseTime, 10),
		"meta:bar_open_time", strconv.FormatInt(snapshot.OpenTime, 10),
		"meta:bar_close_time", strconv.FormatInt(snapshot.CloseTime, 10),
		"meta:bar_interval_ms", strconv.FormatInt(intervalMillis, 10),
		"meta:bar_seq", strconv.FormatInt(barSeq(snapshot.OpenTime, intervalMillis), 10),
		"meta:age_limit_ms", strconv.FormatInt(windowAgeLimitMillis(intervalMillis), 10),
		"meta:ttl_seconds", strconv.FormatInt(int64(ttl/time.Second), 10),
		"meta:version", snapshot.Version,
		"meta:updated_at", strconv.FormatInt(snapshot.UpdatedAt, 10),
	}
	appendPrefixedFields(&fields, "value:", snapshot.Values)
	appendPrefixedFields(&fields, "signal:", snapshot.Signals)
	return fields, nil
}

func indicatorRealtimeHashFields(
	snapshot model.IndicatorRealtimeSnapshot,
	ttl time.Duration,
) ([]interface{}, error) {
	intervalMillis, err := model.IntervalMillis(snapshot.Interval)
	if err != nil {
		return nil, err
	}
	fields := []interface{}{
		"meta:snapshot_type", "realtime",
		"meta:exchange", snapshot.Exchange,
		"meta:market", snapshot.Market,
		"meta:symbol", snapshot.Symbol,
		"meta:interval", snapshot.Interval,
		"meta:open_time", strconv.FormatInt(snapshot.OpenTime, 10),
		"meta:close_time", strconv.FormatInt(snapshot.CloseTime, 10),
		"meta:bar_open_time", strconv.FormatInt(snapshot.Kline.OpenTime, 10),
		"meta:bar_close_time", strconv.FormatInt(snapshot.Kline.CloseTime, 10),
		"meta:bar_interval_ms", strconv.FormatInt(intervalMillis, 10),
		"meta:bar_seq", strconv.FormatInt(barSeq(snapshot.Kline.OpenTime, intervalMillis), 10),
		"meta:age_limit_ms", strconv.FormatInt(realtimeAgeLimitMillis(intervalMillis), 10),
		"meta:ttl_seconds", strconv.FormatInt(int64(ttl/time.Second), 10),
		"meta:updated_at", strconv.FormatInt(snapshot.UpdatedAt, 10),
		"kline:open_time", strconv.FormatInt(snapshot.Kline.OpenTime, 10),
		"kline:close_time", strconv.FormatInt(snapshot.Kline.CloseTime, 10),
		"kline:open", snapshot.Kline.Open,
		"kline:high", snapshot.Kline.High,
		"kline:low", snapshot.Kline.Low,
		"kline:close", snapshot.Kline.Close,
		"kline:volume", snapshot.Kline.Volume,
		"kline:quote_volume", snapshot.Kline.QuoteVolume,
		"kline:trade_count", strconv.FormatInt(snapshot.Kline.TradeCount, 10),
		"kline:taker_buy_volume", snapshot.Kline.TakerBuyVolume,
		"kline:taker_buy_quote_volume", snapshot.Kline.TakerBuyQuoteVolume,
		"kline:is_closed", strconv.FormatBool(snapshot.Kline.IsClosed),
	}
	appendPrefixedFields(&fields, "value:", snapshot.Values)
	appendPrefixedFields(&fields, "signal:", snapshot.Signals)
	return fields, nil
}

func barSeq(openTime int64, intervalMillis int64) int64 {
	if intervalMillis <= 0 {
		return 0
	}
	return openTime / intervalMillis
}

func windowAgeLimitMillis(intervalMillis int64) int64 {
	return intervalMillis * 2
}

func realtimeAgeLimitMillis(intervalMillis int64) int64 {
	switch {
	case intervalMillis <= 5*60*1000:
		return 15 * 1000
	case intervalMillis <= 30*60*1000:
		return 30 * 1000
	default:
		return 60 * 1000
	}
}

func appendPrefixedFields(fields *[]interface{}, prefix string, values map[string]string) {
	for key, value := range values {
		*fields = append(*fields, prefix+key, value)
	}
}

func (s *RedisStore) LastIndicatorOpenTime(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
) (int64, bool, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return 0, false, err
	}
	defer release()

	key := model.IndicatorLastKey(exchange, market, symbol, interval)
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read last indicator open time: %w", err)
	}
	openTime, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse last indicator open time: %w", err)
	}
	return openTime, true, nil
}

func (s *RedisStore) RecentIndicators(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	limit int,
) ([]model.IndicatorSnapshot, error) {
	if limit <= 0 {
		return nil, nil
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	baseKey := model.IndicatorHistoryKey(exchange, market, symbol, interval)
	indexKey := indicatorHistoryIndexKeyFromBase(baseKey)
	dataKey := indicatorHistoryDataKeyFromBase(baseKey)
	fields, err := s.client.ZRevRange(ctx, indexKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("read indicator history index: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	reverseStrings(fields)
	values, err := s.client.HMGet(ctx, dataKey, fields...).Result()
	if err != nil {
		return nil, fmt.Errorf("read indicator history data: %w", err)
	}
	snapshots := make([]model.IndicatorSnapshot, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		var payload []byte
		switch typed := value.(type) {
		case string:
			payload = []byte(typed)
		case []byte:
			payload = typed
		default:
			return nil, fmt.Errorf("decode indicator history: unexpected payload type %T", value)
		}
		var snapshot model.IndicatorSnapshot
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return nil, fmt.Errorf("decode indicator history: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (s *RedisStore) MarkIndicatorOpenTime(ctx context.Context, snapshot model.IndicatorSnapshot) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.IndicatorLastKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
	value := strconv.FormatInt(snapshot.OpenTime, 10)
	if err := s.client.Set(ctx, key, value, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set last indicator open time: %w", err)
	}
	return nil
}

func (s *RedisStore) maintainIndicatorKey(key string, fn func()) {
	if s.indicatorMaintenance == nil {
		fn()
		return
	}
	s.indicatorMaintenance.FreqCall(key, indicatorMaintenanceInterval, fn)
}

func (s *RedisStore) maintainIndicatorKeys(keys []string, fn func(string)) {
	for _, key := range keys {
		key := key
		s.maintainIndicatorKey(key, func() {
			fn(key)
		})
	}
}

func indicatorHistoryIndexKey(snapshot model.IndicatorSnapshot) string {
	return indicatorHistoryIndexKeyFromBase(model.IndicatorHistoryKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval))
}

func indicatorHistoryDataKey(snapshot model.IndicatorSnapshot) string {
	return indicatorHistoryDataKeyFromBase(model.IndicatorHistoryKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval))
}

func indicatorHistoryIndexKeyFromBase(baseKey string) string {
	return baseKey + ":idx"
}

func indicatorHistoryDataKeyFromBase(baseKey string) string {
	return baseKey + ":data"
}

func indicatorHistoryTrimStopRank(limit int64) int64 {
	return -(limit + 1)
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
