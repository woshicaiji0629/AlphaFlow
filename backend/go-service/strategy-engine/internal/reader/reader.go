package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketkeys"
	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyframe"
	"github.com/redis/go-redis/v9"
)

type HashReader interface {
	HGetAll(ctx context.Context, key string) (map[string]string, error)
}

type StringReader interface {
	Get(ctx context.Context, key string) (string, error)
}

type RedisHashReader struct {
	client redis.Cmdable
}

func NewRedisHashReader(client redis.Cmdable) RedisHashReader {
	return RedisHashReader{client: client}
}

func (r RedisHashReader) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if r.client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	return r.client.HGetAll(ctx, key).Result()
}

func (r RedisHashReader) Get(ctx context.Context, key string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("redis client is required")
	}
	return r.client.Get(ctx, key).Result()
}

type Options struct {
	Hashes  HashReader
	Strings StringReader
	Now     func() int64
}

type Reader struct {
	hashes  HashReader
	strings StringReader
	now     func() int64
}

func New(options Options) (*Reader, error) {
	if options.Hashes == nil {
		return nil, fmt.Errorf("hash reader is required")
	}
	if options.Now == nil {
		options.Now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Reader{
		hashes:  options.Hashes,
		strings: options.Strings,
		now:     options.Now,
	}, nil
}

func (r *Reader) Read(ctx context.Context, target strategy.Target, intervals []string) (strategy.Context, error) {
	intervals = normalizeIntervals(target.Interval, intervals)
	snapshots := make(map[string]strategy.Snapshot, len(intervals))
	for _, interval := range intervals {
		snapshot, err := r.readSnapshot(ctx, target, interval, interval == target.Interval)
		if err != nil {
			return strategy.Context{}, err
		}
		snapshots[interval] = snapshot
	}
	asOf := snapshots[target.Interval].Window.CloseTime
	return strategyframe.BuildContext(target, snapshots, asOf, strategy.TriggerOnEntryClose)
}

func normalizeIntervals(entryInterval string, intervals []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(intervals)+1)
	if entryInterval != "" {
		normalized = append(normalized, entryInterval)
		seen[entryInterval] = true
	}
	for _, interval := range intervals {
		interval = strings.TrimSpace(interval)
		if interval == "" || seen[interval] {
			continue
		}
		normalized = append(normalized, interval)
		seen[interval] = true
	}
	return normalized
}

func (r *Reader) readSnapshot(
	ctx context.Context,
	target strategy.Target,
	interval string,
	requireRealtime bool,
) (strategy.Snapshot, error) {
	window, err := r.readWindow(ctx, target, interval)
	if err != nil {
		return strategy.Snapshot{}, err
	}
	health, err := r.readHealth(ctx, target, interval, window.OpenTime)
	if err != nil {
		return strategy.Snapshot{}, err
	}
	realtime := strategy.IndicatorView{}
	current := marketmodel.Kline{}
	if requireRealtime {
		realtime, current, err = r.readRealtime(ctx, target, interval)
		if err != nil {
			return strategy.Snapshot{}, err
		}
	}
	var realtimeView *strategy.RealtimeView
	if requireRealtime {
		realtimeView = &strategy.RealtimeView{
			Current:   current,
			Indicator: realtime,
			Price:     strategyframe.PriceView(realtime, current),
		}
	}
	return strategy.Snapshot{
		Target:    targetWithInterval(target, interval),
		Current:   current,
		Window:    window,
		Price:     strategyframe.PriceView(realtime, current),
		Health:    healthWithUpdatedAt(health, maxInt64(health.UpdatedAt, realtime.UpdatedAt, window.UpdatedAt)),
		Realtime:  realtimeView,
		AsOf:      window.CloseTime,
		Trigger:   strategy.TriggerOnEntryClose,
		UpdatedAt: maxInt64(realtime.UpdatedAt, window.UpdatedAt),
	}, nil
}

func targetWithInterval(target strategy.Target, interval string) strategy.Target {
	target.Interval = interval
	return target
}

func (r *Reader) readWindow(ctx context.Context, target strategy.Target, interval string) (strategy.IndicatorWindowView, error) {
	key := marketkeys.IndicatorWindowKey(target.Exchange, target.Market, target.Symbol, interval)
	fields, err := r.readHash(ctx, key)
	if err != nil {
		return strategy.IndicatorWindowView{}, err
	}
	if err := checkFreshness(fields, r.now()); err != nil {
		return strategy.IndicatorWindowView{}, fmt.Errorf("indicator window %s: %w", key, err)
	}
	window, err := strategyframe.WindowView(marketmodel.IndicatorWindowSnapshot{
		OpenTime:  intField(fields, "meta:open_time"),
		CloseTime: intField(fields, "meta:close_time"),
		Version:   fields["meta:version"],
		Values:    prefixedFields(fields, "value:"),
		Signals:   prefixedFields(fields, "signal:"),
		UpdatedAt: intField(fields, "meta:updated_at"),
	})
	if err != nil {
		return strategy.IndicatorWindowView{}, fmt.Errorf("indicator window %s: %w", key, err)
	}
	return window, nil
}

func (r *Reader) readRealtime(
	ctx context.Context,
	target strategy.Target,
	interval string,
) (strategy.IndicatorView, marketmodel.Kline, error) {
	key := marketkeys.IndicatorRealtimeKey(target.Exchange, target.Market, target.Symbol, interval)
	fields, err := r.readHash(ctx, key)
	if err != nil {
		return strategy.IndicatorView{}, marketmodel.Kline{}, err
	}
	if err := checkFreshness(fields, r.now()); err != nil {
		return strategy.IndicatorView{}, marketmodel.Kline{}, fmt.Errorf("indicator realtime %s: %w", key, err)
	}
	return strategy.IndicatorView{
		OpenTime:  intField(fields, "meta:open_time"),
		CloseTime: intField(fields, "meta:close_time"),
		Values:    prefixedFields(fields, "value:"),
		Signals:   prefixedFields(fields, "signal:"),
		UpdatedAt: intField(fields, "meta:updated_at"),
	}, klineFromFields(target, interval, fields), nil
}

type dataHealth struct {
	KlineStatus           string `json:"kline_status"`
	IndicatorStatus       string `json:"indicator_status"`
	LastKlineOpenTime     int64  `json:"last_kline_open_time,omitempty"`
	LastIndicatorOpenTime int64  `json:"last_indicator_open_time,omitempty"`
	Reason                string `json:"reason,omitempty"`
	UpdatedAt             int64  `json:"updated_at"`
}

func (r *Reader) readHealth(ctx context.Context, target strategy.Target, interval string, windowOpenTime int64) (strategy.HealthView, error) {
	if r.strings == nil {
		return strategy.HealthView{OK: true}, nil
	}
	key := marketkeys.DataHealthKey(target.Exchange, target.Market, target.Symbol, interval)
	raw, err := r.strings.Get(ctx, key)
	if err != nil {
		return strategy.HealthView{}, fmt.Errorf("read data health %s: %w", key, err)
	}
	var health dataHealth
	if err := json.Unmarshal([]byte(raw), &health); err != nil {
		return strategy.HealthView{}, fmt.Errorf("decode data health %s: %w", key, err)
	}
	if health.KlineStatus != "ok" || health.IndicatorStatus != "ok" {
		return strategy.HealthView{}, fmt.Errorf("data health %s not ok: kline=%s indicator=%s reason=%s",
			key,
			health.KlineStatus,
			health.IndicatorStatus,
			health.Reason,
		)
	}
	if windowOpenTime > 0 && health.LastIndicatorOpenTime > 0 && health.LastIndicatorOpenTime < windowOpenTime {
		return strategy.HealthView{}, fmt.Errorf("data health %s indicator cursor behind window: indicator_open_time=%d window_open_time=%d",
			key,
			health.LastIndicatorOpenTime,
			windowOpenTime,
		)
	}
	return strategy.HealthView{OK: true, Reason: health.Reason, UpdatedAt: health.UpdatedAt}, nil
}

func (r *Reader) readHash(ctx context.Context, key string) (map[string]string, error) {
	fields, err := r.hashes.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read redis hash %s: %w", key, err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("redis hash %s missing", key)
	}
	return fields, nil
}

func healthWithUpdatedAt(h strategy.HealthView, updatedAt int64) strategy.HealthView {
	h.UpdatedAt = updatedAt
	return h
}

func checkFreshness(fields map[string]string, now int64) error {
	if now <= 0 {
		return nil
	}
	updatedAt, err := requiredIntField(fields, "meta:updated_at")
	if err != nil {
		return err
	}
	ageLimit, err := requiredIntField(fields, "meta:age_limit_ms")
	if err != nil {
		return err
	}
	if updatedAt+ageLimit < now {
		return fmt.Errorf("snapshot stale: updated_at=%d age_limit_ms=%d now=%d", updatedAt, ageLimit, now)
	}
	return nil
}

func prefixedFields(fields map[string]string, prefix string) map[string]string {
	values := map[string]string{}
	for field, value := range fields {
		key, ok := strings.CutPrefix(field, prefix)
		if ok {
			values[key] = value
		}
	}
	return values
}

func klineFromFields(target strategy.Target, interval string, fields map[string]string) marketmodel.Kline {
	return marketmodel.Kline{
		Exchange:            target.Exchange,
		Market:              target.Market,
		Symbol:              target.Symbol,
		Interval:            interval,
		OpenTime:            intField(fields, "kline:open_time"),
		CloseTime:           intField(fields, "kline:close_time"),
		Open:                fields["kline:open"],
		High:                fields["kline:high"],
		Low:                 fields["kline:low"],
		Close:               fields["kline:close"],
		Volume:              fields["kline:volume"],
		QuoteVolume:         fields["kline:quote_volume"],
		TradeCount:          intField(fields, "kline:trade_count"),
		TakerBuyVolume:      fields["kline:taker_buy_volume"],
		TakerBuyQuoteVolume: fields["kline:taker_buy_quote_volume"],
		IsClosed:            boolField(fields, "kline:is_closed"),
	}
}

func requiredIntField(fields map[string]string, key string) (int64, error) {
	value, ok := fields[key]
	if !ok || value == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	parsed, err := parseInt(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func intField(fields map[string]string, key string) int64 {
	parsed, _ := parseInt(fields[key])
	return parsed
}

func boolField(fields map[string]string, key string) bool {
	parsed, _ := strconv.ParseBool(fields[key])
	return parsed
}

func parseInt(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
