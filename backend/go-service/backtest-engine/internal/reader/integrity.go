package reader

import (
	"context"
	"fmt"
	"sort"

	"alphaflow/go-service/pkg/marketmodel"
)

type DatasetIntegrity struct {
	Complete bool              `json:"complete"`
	Series   []SeriesIntegrity `json:"series"`
}

type SeriesIntegrity struct {
	Symbol                  string  `json:"symbol"`
	Interval                string  `json:"interval"`
	EffectiveStart          int64   `json:"effective_start"`
	RequestedStart          int64   `json:"requested_start"`
	End                     int64   `json:"end"`
	Rows                    int     `json:"rows"`
	UniqueRows              int     `json:"unique_rows"`
	DuplicateOpenTimes      []int64 `json:"duplicate_open_times,omitempty"`
	MissingWarmupOpenTimes  []int64 `json:"missing_warmup_open_times,omitempty"`
	MissingTradingOpenTimes []int64 `json:"missing_trading_open_times,omitempty"`
	AvailableWarmupBars     int64   `json:"available_warmup_bars"`
	LongestRunBars          int     `json:"longest_run_bars"`
	LongestRunStart         int64   `json:"longest_run_start,omitempty"`
	LongestRunEnd           int64   `json:"longest_run_end,omitempty"`
}

func (r *Reader) CheckDataset(ctx context.Context, request DatasetRequest) (DatasetIntegrity, error) {
	if err := validateDatasetRequest(request); err != nil {
		return DatasetIntegrity{}, err
	}
	intervals := datasetIntervals(request.Interval, request.ConfirmIntervals)
	report := DatasetIntegrity{Complete: true, Series: make([]SeriesIntegrity, 0, len(request.Symbols)*len(intervals))}
	for _, symbol := range request.Symbols {
		for _, interval := range intervals {
			item, err := r.checkSeries(ctx, Request{
				Exchange: request.Exchange, Market: request.Market, Symbol: symbol, Interval: interval,
				Start: request.Start, End: request.End, WarmupBars: request.WarmupBars,
			})
			if err != nil {
				return DatasetIntegrity{}, fmt.Errorf("check dataset %s %s: %w", symbol, interval, err)
			}
			if len(item.DuplicateOpenTimes) > 0 || len(item.MissingWarmupOpenTimes) > 0 || len(item.MissingTradingOpenTimes) > 0 {
				report.Complete = false
			}
			report.Series = append(report.Series, item)
		}
	}
	return report, nil
}

func (r *Reader) checkSeries(ctx context.Context, request Request) (SeriesIntegrity, error) {
	intervalMillis, err := marketmodel.IntervalMillis(request.Interval)
	if err != nil {
		return SeriesIntegrity{}, err
	}
	effectiveStart := request.Start - request.WarmupBars*intervalMillis
	klines, err := r.store.RangeKlines(ctx, request.Exchange, request.Market, request.Symbol, request.Interval, effectiveStart, request.End)
	if err != nil {
		return SeriesIntegrity{}, err
	}
	counts := make(map[int64]int, len(klines))
	rowsInRange := 0
	for _, kline := range klines {
		if kline.OpenTime >= effectiveStart && kline.OpenTime < request.End {
			counts[kline.OpenTime]++
			rowsInRange++
		}
	}
	openTimes := make([]int64, 0, len(counts))
	duplicates := []int64{}
	for openTime, count := range counts {
		openTimes = append(openTimes, openTime)
		if count > 1 {
			duplicates = append(duplicates, openTime)
		}
	}
	sort.Slice(openTimes, func(i, j int) bool { return openTimes[i] < openTimes[j] })
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i] < duplicates[j] })
	existing := make(map[int64]struct{}, len(openTimes))
	for _, openTime := range openTimes {
		existing[openTime] = struct{}{}
	}
	_, missingWarmup := countRangeFromSet(existing, effectiveStart, request.Start, intervalMillis)
	_, missingTrading := countRangeFromSet(existing, request.Start, request.End, intervalMillis)
	availableWarmup := int64(0)
	for openTime := request.Start - intervalMillis; openTime >= effectiveStart; openTime -= intervalMillis {
		if _, ok := existing[openTime]; !ok {
			break
		}
		availableWarmup++
	}
	runBars, runStart, runEnd := longestRun(openTimes, intervalMillis)
	return SeriesIntegrity{
		Symbol: request.Symbol, Interval: request.Interval,
		EffectiveStart: effectiveStart, RequestedStart: request.Start, End: request.End,
		Rows: rowsInRange, UniqueRows: len(openTimes), DuplicateOpenTimes: duplicates,
		MissingWarmupOpenTimes: missingWarmup, MissingTradingOpenTimes: missingTrading,
		AvailableWarmupBars: availableWarmup,
		LongestRunBars:      runBars, LongestRunStart: runStart, LongestRunEnd: runEnd,
	}, nil
}

func countRangeFromSet(existing map[int64]struct{}, start int64, end int64, intervalMillis int64) (int, []int64) {
	count := 0
	missing := []int64{}
	for openTime := start; openTime < end; openTime += intervalMillis {
		if _, ok := existing[openTime]; ok {
			count++
		} else {
			missing = append(missing, openTime)
		}
	}
	return count, missing
}

func longestRun(openTimes []int64, intervalMillis int64) (int, int64, int64) {
	best, current := 0, 0
	var bestStart, bestEnd, currentStart int64
	for index, openTime := range openTimes {
		if index == 0 || openTime != openTimes[index-1]+intervalMillis {
			current = 1
			currentStart = openTime
		} else {
			current++
		}
		if current > best {
			best = current
			bestStart = currentStart
			bestEnd = openTime
		}
	}
	return best, bestStart, bestEnd
}
