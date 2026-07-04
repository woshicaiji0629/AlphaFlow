package reader

import (
	"context"
	"fmt"
	"sort"

	"alphaflow/go-service/pkg/marketmodel"
)

type Request struct {
	Exchange   string
	Market     string
	Symbol     string
	Interval   string
	Start      int64
	End        int64
	WarmupBars int64
}

type Result struct {
	Klines         []marketmodel.Kline
	RequestedStart int64
	EffectiveStart int64
	End            int64
	WarmupBars     int64
	WarmupCount    int
	TradingCount   int
}

type KlineStore interface {
	RangeKlines(
		ctx context.Context,
		exchange string,
		market string,
		symbol string,
		interval string,
		start int64,
		end int64,
	) ([]marketmodel.Kline, error)
}

type Reader struct {
	store KlineStore
}

func New(store KlineStore) (*Reader, error) {
	if store == nil {
		return nil, fmt.Errorf("kline store is required")
	}
	return &Reader{store: store}, nil
}

func (r *Reader) ReadKlines(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	intervalMillis, err := marketmodel.IntervalMillis(request.Interval)
	if err != nil {
		return Result{}, err
	}
	effectiveStart := request.Start - request.WarmupBars*intervalMillis
	klines, err := r.store.RangeKlines(
		ctx,
		request.Exchange,
		request.Market,
		request.Symbol,
		request.Interval,
		effectiveStart,
		request.End,
	)
	if err != nil {
		return Result{}, fmt.Errorf("read historical klines: %w", err)
	}
	klines = normalizeKlines(klines, effectiveStart, request.End)
	warmupCount, warmupMissing := countRange(klines, effectiveStart, request.Start, intervalMillis)
	tradingCount, tradingMissing := countRange(klines, request.Start, request.End, intervalMillis)
	if len(warmupMissing) > 0 || len(tradingMissing) > 0 {
		return Result{}, fmt.Errorf(
			"historical klines incomplete: warmup missing %d, trading missing %d",
			len(warmupMissing),
			len(tradingMissing),
		)
	}
	return Result{
		Klines:         klines,
		RequestedStart: request.Start,
		EffectiveStart: effectiveStart,
		End:            request.End,
		WarmupBars:     request.WarmupBars,
		WarmupCount:    warmupCount,
		TradingCount:   tradingCount,
	}, nil
}

func validateRequest(request Request) error {
	if request.Exchange == "" {
		return fmt.Errorf("exchange cannot be empty")
	}
	if request.Market == "" {
		return fmt.Errorf("market cannot be empty")
	}
	if request.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}
	if request.Interval == "" {
		return fmt.Errorf("interval cannot be empty")
	}
	if _, err := marketmodel.IntervalMillis(request.Interval); err != nil {
		return err
	}
	if request.End < request.Start {
		return fmt.Errorf("end must be greater than or equal to start")
	}
	if request.WarmupBars < 0 {
		return fmt.Errorf("warmup bars cannot be negative")
	}
	return nil
}

func normalizeKlines(klines []marketmodel.Kline, start int64, end int64) []marketmodel.Kline {
	filtered := make([]marketmodel.Kline, 0, len(klines))
	for _, kline := range klines {
		if kline.OpenTime < start || kline.OpenTime >= end {
			continue
		}
		filtered = append(filtered, kline)
	}
	sort.Slice(filtered, func(i int, j int) bool {
		return filtered[i].OpenTime < filtered[j].OpenTime
	})
	return filtered
}

func countRange(klines []marketmodel.Kline, start int64, end int64, intervalMillis int64) (int, []int64) {
	existing := make(map[int64]struct{}, len(klines))
	for _, kline := range klines {
		existing[kline.OpenTime] = struct{}{}
	}
	count := 0
	missing := []int64{}
	for openTime := start; openTime < end; openTime += intervalMillis {
		if _, ok := existing[openTime]; ok {
			count++
			continue
		}
		missing = append(missing, openTime)
	}
	return count, missing
}
