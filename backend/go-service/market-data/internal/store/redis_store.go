package store

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/lcache"
	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client                 *redis.Client
	retention              Retention
	ops                    chan struct{}
	klineMaintenance       *lcache.Cache
	indicatorMaintenance   *lcache.Cache
	liquidationMaintenance *lcache.Cache
	webSocketStatusCache   *lcache.Cache
}

type klineHashUpdate struct {
	fields       []string
	hashValues   []interface{}
	indexMembers []redis.Z
}

const (
	klineMaintenanceInterval = time.Minute
	klineMaintenanceMaxKeys  = 20000

	indicatorMaintenanceInterval = time.Minute
	indicatorMaintenanceMaxKeys  = 20000

	liquidationMaintenanceInterval = time.Minute
	liquidationMaintenanceMaxKeys  = 20000
	webSocketStatusCacheMaxKeys    = 5000
	webSocketStatusCacheTTL        = 30 * time.Second
)

const trimKlineHashScript = `
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

type Retention struct {
	KlineLimit     int64
	KlineTTL       time.Duration
	LiquidationTTL time.Duration
	LatestTTL      time.Duration
	PollingTTL     time.Duration
}

func NewRedisStore(client *redis.Client, retention Retention) *RedisStore {
	return &RedisStore{
		client:                 client,
		retention:              retention,
		ops:                    make(chan struct{}, redisOperationLimit()),
		klineMaintenance:       lcache.MustNew(klineMaintenanceMaxKeys),
		indicatorMaintenance:   lcache.MustNew(indicatorMaintenanceMaxKeys),
		liquidationMaintenance: lcache.MustNew(liquidationMaintenanceMaxKeys),
		webSocketStatusCache:   lcache.MustNew(webSocketStatusCacheMaxKeys),
	}
}

func redisOperationLimit() int {
	limit := runtime.NumCPU() * 4
	if limit < 32 {
		return 32
	}
	if limit > 96 {
		return 96
	}
	return limit
}

func (s *RedisStore) acquire(ctx context.Context) (func(), error) {
	if s.ops == nil {
		return func() {}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.ops <- struct{}{}:
		return func() { <-s.ops }, nil
	}
}

func (s *RedisStore) LastOpenTime(
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

	indexKey := klineIndexKey(model.RedisKey(exchange, market, symbol, interval))
	values, err := s.client.ZRevRangeWithScores(ctx, indexKey, 0, 0).Result()
	if err != nil {
		return 0, false, fmt.Errorf("read latest kline: %w", err)
	}
	if len(values) == 0 {
		return 0, false, nil
	}
	return int64(values[0].Score), true, nil
}

func (s *RedisStore) RangeKlines(
	ctx context.Context,
	exchange string,
	market string,
	symbol string,
	interval string,
	start int64,
	end int64,
) ([]model.Kline, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	baseKey := model.RedisKey(exchange, market, symbol, interval)
	indexKey := klineIndexKey(baseKey)
	fields, err := s.client.ZRangeByScore(ctx, indexKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(start, 10),
		Max: strconv.FormatInt(end, 10),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read kline index: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}

	values, err := s.client.HMGet(ctx, klineDataKey(baseKey), fields...).Result()
	if err != nil {
		return nil, fmt.Errorf("read kline data: %w", err)
	}

	klines := make([]model.Kline, 0, len(values))
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
			return nil, fmt.Errorf("decode kline: unexpected payload type %T", value)
		}
		var kline model.Kline
		if err := json.Unmarshal(payload, &kline); err != nil {
			return nil, fmt.Errorf("decode kline: %w", err)
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

func (s *RedisStore) UpsertKline(ctx context.Context, kline model.Kline) error {
	return s.UpsertKlines(ctx, []model.Kline{kline})
}

func groupKlineHashUpdates(klines []model.Kline) (map[string]klineHashUpdate, error) {
	grouped := make(map[string]map[int64][]byte)
	for _, kline := range klines {
		key := model.RedisKey(kline.Exchange, kline.Market, kline.Symbol, kline.Interval)
		payload, err := json.Marshal(kline)
		if err != nil {
			return nil, fmt.Errorf("marshal kline: %w", err)
		}
		if grouped[key] == nil {
			grouped[key] = make(map[int64][]byte)
		}
		grouped[key][kline.OpenTime] = payload
	}

	updates := make(map[string]klineHashUpdate, len(grouped))
	for key, payloadByOpenTime := range grouped {
		update := klineHashUpdate{
			fields:       make([]string, 0, len(payloadByOpenTime)),
			hashValues:   make([]interface{}, 0, len(payloadByOpenTime)*2),
			indexMembers: make([]redis.Z, 0, len(payloadByOpenTime)),
		}
		for openTime, payload := range payloadByOpenTime {
			field := strconv.FormatInt(openTime, 10)
			update.fields = append(update.fields, field)
			update.hashValues = append(update.hashValues, field, payload)
			update.indexMembers = append(update.indexMembers, redis.Z{
				Score:  float64(openTime),
				Member: field,
			})
		}
		updates[key] = update
	}
	return updates, nil
}

func (s *RedisStore) UpsertKlines(ctx context.Context, klines []model.Kline) error {
	if len(klines) == 0 {
		return nil
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	grouped, err := groupKlineHashUpdates(klines)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	for key, update := range grouped {
		dataKey := klineDataKey(key)
		indexKey := klineIndexKey(key)
		pipe.HSet(ctx, dataKey, update.hashValues...)
		pipe.ZAdd(ctx, indexKey, update.indexMembers...)
		s.maintainKlineKey(key, func() {
			pipe.Eval(ctx, trimKlineHashScript, []string{indexKey, dataKey}, klineTrimStopRank(s.retention.KlineLimit), s.retention.KlineTTL.Milliseconds())
		})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("upsert klines: %w", err)
	}
	return nil
}

func (s *RedisStore) SetLastPrice(ctx context.Context, price model.LastPrice) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal last price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set last price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetMarkPrice(ctx context.Context, price model.MarkPrice) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
	payload, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("marshal mark price: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set mark price: %w", err)
	}
	return nil
}

func (s *RedisStore) SetBookTicker(ctx context.Context, ticker model.BookTicker) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
	payload, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("marshal book ticker: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set book ticker: %w", err)
	}
	return nil
}

func (s *RedisStore) SetLatestBatch(
	ctx context.Context,
	lastPrices []model.LastPrice,
	markPrices []model.MarkPrice,
	bookTickers []model.BookTicker,
	openInterests []model.OpenInterest,
	openKlines []model.Kline,
	indicators []model.IndicatorSnapshot,
	indicatorWins []model.IndicatorWindowSnapshot,
	indicatorRTs []model.IndicatorRealtimeSnapshot,
) error {
	if len(lastPrices) == 0 &&
		len(markPrices) == 0 &&
		len(bookTickers) == 0 &&
		len(openInterests) == 0 &&
		len(openKlines) == 0 &&
		len(indicators) == 0 &&
		len(indicatorWins) == 0 &&
		len(indicatorRTs) == 0 {
		return nil
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	pipe := s.client.Pipeline()
	hasWrites := false
	for _, price := range lastPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal last price: %w", err)
		}
		key := model.LastPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, price := range markPrices {
		payload, err := json.Marshal(price)
		if err != nil {
			return fmt.Errorf("marshal mark price: %w", err)
		}
		key := model.MarkPriceKey(price.Exchange, price.Market, price.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, ticker := range bookTickers {
		payload, err := json.Marshal(ticker)
		if err != nil {
			return fmt.Errorf("marshal book ticker: %w", err)
		}
		key := model.BookTickerKey(ticker.Exchange, ticker.Market, ticker.Symbol)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		hasWrites = true
	}
	for _, interest := range openInterests {
		payload, err := json.Marshal(interest)
		if err != nil {
			return fmt.Errorf("marshal open interest: %w", err)
		}
		key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
		pipe.Set(ctx, key, payload, s.retention.PollingTTL)
		hasWrites = true
	}
	groupedKlines, err := groupKlineHashUpdates(openKlines)
	if err != nil {
		return err
	}
	for key, update := range groupedKlines {
		dataKey := klineDataKey(key)
		indexKey := klineIndexKey(key)
		pipe.HSet(ctx, dataKey, update.hashValues...)
		pipe.ZAdd(ctx, indexKey, update.indexMembers...)
		hasWrites = true
		s.maintainKlineKey(key, func() {
			pipe.Eval(ctx, trimKlineHashScript, []string{indexKey, dataKey}, klineTrimStopRank(s.retention.KlineLimit), s.retention.KlineTTL.Milliseconds())
			hasWrites = true
		})
	}
	for _, snapshot := range indicators {
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal indicator: %w", err)
		}
		key := model.IndicatorKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.Set(ctx, key, payload, s.retention.LatestTTL)
		s.maintainIndicatorKeys([]string{key}, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	for _, snapshot := range indicatorWins {
		fields, err := indicatorWindowHashFields(snapshot, s.retention.LatestTTL)
		if err != nil {
			return err
		}
		key := model.IndicatorWindowLatestKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.HSet(ctx, key, fields...)
		s.maintainIndicatorKeys([]string{key}, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	for _, snapshot := range indicatorRTs {
		fields, err := indicatorRealtimeHashFields(snapshot, s.retention.LatestTTL)
		if err != nil {
			return err
		}
		key := model.IndicatorRealtimeKey(snapshot.Exchange, snapshot.Market, snapshot.Symbol, snapshot.Interval)
		pipe.HSet(ctx, key, fields...)
		s.maintainIndicatorKeys([]string{key}, func() {
			pipe.Expire(ctx, key, s.retention.LatestTTL)
			hasWrites = true
		})
		hasWrites = true
	}
	if !hasWrites {
		return nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set latest batch: %w", err)
	}
	return nil
}

func (s *RedisStore) maintainKlineKey(key string, fn func()) {
	if s.klineMaintenance == nil {
		fn()
		return
	}
	s.klineMaintenance.FreqCall(key, klineMaintenanceInterval, fn)
}

func (s *RedisStore) maintainIndicatorKeys(keys []string, fn func()) {
	if len(keys) == 0 {
		return
	}
	if s.indicatorMaintenance == nil {
		fn()
		return
	}
	cacheKey := strings.Join(keys, "\x00")
	s.indicatorMaintenance.FreqCall(cacheKey, indicatorMaintenanceInterval, fn)
}

func klineDataKey(baseKey string) string {
	return baseKey + ":data"
}

func klineIndexKey(baseKey string) string {
	return baseKey + ":idx"
}

func klineTrimStopRank(limit int64) int64 {
	return -(limit + 1)
}

func (s *RedisStore) SetOpenInterest(ctx context.Context, interest model.OpenInterest) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.OpenInterestKey(interest.Exchange, interest.Market, interest.Symbol)
	payload, err := json.Marshal(interest)
	if err != nil {
		return fmt.Errorf("marshal open interest: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.PollingTTL).Err(); err != nil {
		return fmt.Errorf("set open interest: %w", err)
	}
	return nil
}

func (s *RedisStore) AddLiquidation(
	ctx context.Context,
	liquidation model.Liquidation,
	limit int64,
) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.LiquidationKey(liquidation.Exchange, liquidation.Market, liquidation.Symbol)
	payload, err := json.Marshal(liquidation)
	if err != nil {
		return fmt.Errorf("marshal liquidation: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(liquidation.TradeTime),
		Member: payload,
	})
	s.maintainLiquidationKey(key, func() {
		pipe.ZRemRangeByRank(ctx, key, 0, -(limit + 1))
		pipe.Expire(ctx, key, s.retention.LiquidationTTL)
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("add liquidation: %w", err)
	}
	return nil
}

func (s *RedisStore) SetMarketStatus(ctx context.Context, status model.MarketStatus) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.MarketStatusKey(status.Exchange, status.Market)
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal market status: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, 0).Err(); err != nil {
		return fmt.Errorf("set market status: %w", err)
	}
	return nil
}

func (s *RedisStore) SetWebSocketStatus(ctx context.Context, status model.WebSocketStatus) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.WebSocketStatusKey(status.Exchange, status.Market)
	if status.Shard != "" {
		key = model.WebSocketShardStatusKey(status.Exchange, status.Market, status.Shard)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal websocket status: %w", err)
	}
	if s.shouldSkipWebSocketStatusWrite(key, payload) {
		return nil
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set websocket status: %w", err)
	}
	return nil
}

func (s *RedisStore) IsMarketAvailable(ctx context.Context, exchange string, market string) (bool, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	key := model.MarketStatusKey(exchange, market)
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read market status: %w", err)
	}

	var status model.MarketStatus
	if err := json.Unmarshal([]byte(value), &status); err != nil {
		return false, fmt.Errorf("decode market status: %w", err)
	}
	return status.Available, nil
}

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
	pipe := s.client.Pipeline()
	pipe.Set(ctx, indicatorKey, payload, s.retention.LatestTTL)
	pipe.Set(ctx, lastKey, strconv.FormatInt(snapshot.OpenTime, 10), s.retention.LatestTTL)
	s.maintainIndicatorKeys([]string{indicatorKey, lastKey}, func() {
		pipe.Expire(ctx, indicatorKey, s.retention.LatestTTL)
		pipe.Expire(ctx, lastKey, s.retention.LatestTTL)
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
	s.maintainIndicatorKeys([]string{windowKey, lastKey}, func() {
		pipe.Expire(ctx, windowKey, s.retention.LatestTTL)
		pipe.Expire(ctx, lastKey, s.retention.LatestTTL)
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
	windowKey := model.IndicatorWindowKey(windowSnapshot.Exchange, windowSnapshot.Market, windowSnapshot.Symbol, windowSnapshot.Interval)
	windowLastKey := model.IndicatorWindowLastKey(windowSnapshot.Exchange, windowSnapshot.Market, windowSnapshot.Symbol, windowSnapshot.Interval)

	pipe := s.client.Pipeline()
	pipe.Set(ctx, indicatorKey, payload, s.retention.LatestTTL)
	pipe.Set(ctx, indicatorLastKey, strconv.FormatInt(snapshot.OpenTime, 10), s.retention.LatestTTL)
	pipe.HSet(ctx, windowKey, fields...)
	pipe.Set(ctx, windowLastKey, strconv.FormatInt(windowSnapshot.OpenTime, 10), s.retention.LatestTTL)
	s.maintainIndicatorKeys([]string{indicatorKey, indicatorLastKey, windowKey, windowLastKey}, func() {
		pipe.Expire(ctx, indicatorKey, s.retention.LatestTTL)
		pipe.Expire(ctx, indicatorLastKey, s.retention.LatestTTL)
		pipe.Expire(ctx, windowKey, s.retention.LatestTTL)
		pipe.Expire(ctx, windowLastKey, s.retention.LatestTTL)
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
	s.maintainIndicatorKeys([]string{key}, func() {
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

func (s *RedisStore) SetDataHealth(ctx context.Context, health model.DataHealth) error {
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	key := model.DataHealthKey(health.Exchange, health.Market, health.Symbol, health.Interval)
	payload, err := json.Marshal(health)
	if err != nil {
		return fmt.Errorf("marshal data health: %w", err)
	}
	if err := s.client.Set(ctx, key, payload, s.retention.LatestTTL).Err(); err != nil {
		return fmt.Errorf("set data health: %w", err)
	}
	return nil
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

func (s *RedisStore) maintainLiquidationKey(key string, fn func()) {
	if s.liquidationMaintenance == nil {
		fn()
		return
	}
	s.liquidationMaintenance.FreqCall(key, liquidationMaintenanceInterval, fn)
}

func (s *RedisStore) shouldSkipWebSocketStatusWrite(key string, payload []byte) bool {
	return shouldSkipCachedPayloadWrite(s.webSocketStatusCache, key, payload, webSocketStatusCacheTTL)
}

func shouldSkipCachedPayloadWrite(cache *lcache.Cache, key string, payload []byte, exp time.Duration) bool {
	if cache == nil {
		return false
	}
	if cached, ok := cache.Get(key); ok {
		if cachedPayload, ok := cached.(string); ok && cachedPayload == string(payload) {
			return true
		}
	}
	cache.SetEx(key, string(payload), exp)
	return false
}
