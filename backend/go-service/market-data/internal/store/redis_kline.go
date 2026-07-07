package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"alphaflow/go-service/market-data/internal/model"
	"github.com/redis/go-redis/v9"
)

type klineHashUpdate struct {
	fields       []string
	hashValues   []interface{}
	indexMembers []redis.Z
}

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

func (s *RedisStore) maintainKlineKey(key string, fn func()) {
	if s.klineMaintenance == nil {
		fn()
		return
	}
	s.klineMaintenance.FreqCall(key, klineMaintenanceInterval, fn)
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
