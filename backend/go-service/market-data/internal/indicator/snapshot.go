package indicator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketbus"
)

const indicatorWindowLookback = 20

type calculatedIndicators struct {
	current model.IndicatorSnapshot
	recent  []model.IndicatorSnapshot
}

func (r *Runner) publishClosedSnapshot(
	ctx context.Context,
	snapshot model.IndicatorSnapshot,
	windowSnapshot model.IndicatorWindowSnapshot,
) error {
	if r.options.Publisher == nil {
		return nil
	}
	envelope := marketbus.NewClosedEnvelope(snapshot, windowSnapshot, r.now().UnixMilli(), r.options.PublishTTL)
	messageID, err := r.options.Publisher.PublishSnapshot(ctx, envelope)
	if err != nil {
		return err
	}
	slog.Debug(
		"published closed market snapshot",
		"message_id", messageID,
		"exchange", snapshot.Exchange,
		"market", snapshot.Market,
		"symbol", snapshot.Symbol,
		"interval", snapshot.Interval,
		"open_time", snapshot.OpenTime,
	)
	return nil
}

func (r *Runner) publishRealtimeSnapshot(
	ctx context.Context,
	snapshot model.IndicatorRealtimeSnapshot,
) error {
	if r.options.Publisher == nil {
		return nil
	}
	envelope := marketbus.NewRealtimeEnvelope(snapshot, r.now().UnixMilli(), r.options.PublishTTL)
	messageID, err := r.options.Publisher.PublishSnapshot(ctx, envelope)
	if err != nil {
		return err
	}
	slog.Debug(
		"published realtime market snapshot",
		"message_id", messageID,
		"exchange", snapshot.Exchange,
		"market", snapshot.Market,
		"symbol", snapshot.Symbol,
		"interval", snapshot.Interval,
		"open_time", snapshot.OpenTime,
	)
	return nil
}

func (r *Runner) indicatorWindowSnapshot(
	rule Rule,
	symbol string,
	interval string,
	snapshots []model.IndicatorSnapshot,
	updatedAt int64,
) (model.IndicatorWindowSnapshot, error) {
	result, err := indicatorwindow.AnalyzeOrdered(snapshots)
	if err != nil {
		return model.IndicatorWindowSnapshot{}, err
	}
	feature := featureMetadata(r.options.CalculateOptions)
	if len(snapshots) > 0 && snapshots[len(snapshots)-1].Feature.SchemaVersion != "" {
		feature = snapshots[len(snapshots)-1].Feature
	}
	return model.IndicatorWindowSnapshot{
		Exchange:  rule.Exchange,
		Market:    rule.Market,
		Symbol:    symbol,
		Interval:  interval,
		OpenTime:  result.OpenTime,
		CloseTime: result.CloseTime,
		Version:   result.Version,
		Values:    result.Values,
		Signals:   result.Signals,
		Feature:   feature,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *Runner) analyzeIndicatorWindow(
	rule Rule,
	symbol string,
	interval string,
	snapshots []model.IndicatorSnapshot,
	updatedAt int64,
	intervalMillis int64,
) (model.IndicatorWindowSnapshot, error) {
	if err := validateIndicatorSnapshotContinuity(snapshots, intervalMillis); err != nil {
		return model.IndicatorWindowSnapshot{}, err
	}
	return r.indicatorWindowSnapshot(rule, symbol, interval, snapshots, updatedAt)
}

func (r *Runner) calculateClosedIndicators(
	ctx context.Context,
	key string,
	rule Rule,
	symbol string,
	interval string,
	closed []model.Kline,
) (calculatedIndicators, error) {
	cached := r.cachedIndicatorSnapshotsForWindow(key, closed)
	if len(cached) == 0 {
		recent, err := r.store.RecentIndicators(ctx, rule.Exchange, rule.Market, symbol, interval, r.options.SnapshotCacheLimit)
		if err != nil {
			return calculatedIndicators{}, err
		}
		cached = alignedIndicatorSnapshotsInWindow(closed, recent, r.options.SnapshotCacheLimit)
	}
	snapshots, err := r.calculatedIndicatorSnapshotsForWindow(closed, cached)
	if err != nil {
		return calculatedIndicators{}, err
	}
	snapshots = r.rememberIndicatorSnapshots(key, snapshots)
	if len(snapshots) == 0 {
		return calculatedIndicators{}, fmt.Errorf("no calculated indicator snapshots")
	}
	return calculatedIndicators{
		current: snapshots[len(snapshots)-1],
		recent:  snapshots,
	}, nil
}

func (r *Runner) calculateRealtimeIndicators(
	key string,
	window *indicatorcalc.CalculationWindow,
	kline model.Kline,
) (calculatedIndicators, error) {
	result, err := r.calculateWindow(window)
	if err != nil {
		return calculatedIndicators{}, err
	}
	snapshot := indicatorSnapshotFromResult(kline, result, featureMetadata(r.options.CalculateOptions), r.now().UnixMilli())
	snapshots := r.cachedIndicatorSnapshotsWithCurrent(key, snapshot)
	if len(snapshots) == 0 {
		snapshots = []model.IndicatorSnapshot{snapshot}
	}
	return calculatedIndicators{
		current: snapshot,
		recent:  snapshots,
	}, nil
}

func indicatorSnapshotFromResult(kline model.Kline, result indicatorcalc.Result, feature model.FeatureMetadata, updatedAt int64) model.IndicatorSnapshot {
	return model.IndicatorSnapshot{
		Exchange:      kline.Exchange,
		Market:        kline.Market,
		Symbol:        kline.Symbol,
		Interval:      kline.Interval,
		OpenTime:      result.OpenTime,
		CloseTime:     result.CloseTime,
		Values:        result.Values,
		NumericValues: result.NumericValues,
		Signals:       result.Signals,
		Feature:       feature,
		UpdatedAt:     updatedAt,
	}
}

func (r *Runner) calculatedIndicatorSnapshotsForWindow(
	closed []model.Kline,
	cached []model.IndicatorSnapshot,
) ([]model.IndicatorSnapshot, error) {
	if len(closed) == 0 {
		return nil, fmt.Errorf("no closed klines")
	}
	cached = snapshotsMatchingFeature(cached, featureMetadata(r.options.CalculateOptions))
	lookback := r.options.WindowLookback
	if lookback > len(closed) {
		lookback = len(closed)
	}
	start := len(closed) - lookback
	if snapshotsCoverKlines(cached, closed[start:]) {
		return trimIndicatorSnapshots(cached, lookback), nil
	}
	cachedByOpenTime := map[int64]model.IndicatorSnapshot{}
	for _, snapshot := range cached {
		cachedByOpenTime[snapshot.OpenTime] = snapshot
	}
	snapshots := make([]model.IndicatorSnapshot, 0, lookback)
	firstMissing := len(closed)
	for index := start; index < len(closed); index++ {
		kline := closed[index]
		if cachedSnapshot, ok := cachedByOpenTime[kline.OpenTime]; ok {
			snapshots = append(snapshots, cachedSnapshot)
			continue
		}
		firstMissing = index
		break
	}
	if firstMissing == len(closed) {
		return snapshots, nil
	}
	warmup := int(r.options.WarmupPeriods)
	if warmup <= 0 || warmup > len(closed) {
		warmup = len(closed)
	}
	results, err := r.calculateWindows(closed, firstMissing, warmup)
	if err != nil {
		return nil, err
	}
	for offset, result := range results {
		index := firstMissing + offset
		kline := closed[index]
		if cachedSnapshot, ok := cachedByOpenTime[kline.OpenTime]; ok {
			snapshots = append(snapshots, cachedSnapshot)
			continue
		}
		snapshots = append(snapshots, model.IndicatorSnapshot{
			Exchange:      kline.Exchange,
			Market:        kline.Market,
			Symbol:        kline.Symbol,
			Interval:      kline.Interval,
			OpenTime:      result.OpenTime,
			CloseTime:     result.CloseTime,
			Values:        result.Values,
			NumericValues: result.NumericValues,
			Signals:       result.Signals,
			Feature:       featureMetadata(r.options.CalculateOptions),
			UpdatedAt:     r.now().UnixMilli(),
		})
	}
	return snapshots, nil
}

func snapshotsMatchingFeature(snapshots []model.IndicatorSnapshot, expected model.FeatureMetadata) []model.IndicatorSnapshot {
	matched := make([]model.IndicatorSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Feature == expected {
			matched = append(matched, snapshot)
		}
	}
	return matched
}

func featureMetadata(options indicatorcalc.Options) model.FeatureMetadata {
	metadata := indicatorcalc.Metadata(options)
	return model.FeatureMetadata{
		SchemaVersion:     metadata.SchemaVersion,
		CalculatorVersion: metadata.CalculatorVersion,
		ParameterHash:     metadata.ParameterHash,
	}
}

func (r *Runner) calculateWindows(
	klines []model.Kline,
	start int,
	warmup int,
) ([]indicatorcalc.Result, error) {
	results, err := indicatorcalc.CalculateWindows(klines, start, warmup, r.options.CalculateOptions)
	if r.options.OnCalculateWindow != nil {
		for range results {
			r.options.OnCalculateWindow()
		}
	}
	return results, err
}

func (r *Runner) calculateWindow(window *indicatorcalc.CalculationWindow) (indicatorcalc.Result, error) {
	if r.options.OnCalculateWindow != nil {
		r.options.OnCalculateWindow()
	}
	return indicatorcalc.CalculateWindow(window, r.options.CalculateOptions)
}

func (r *Runner) cachedIndicatorSnapshotsWithCurrent(
	key string,
	snapshot model.IndicatorSnapshot,
) []model.IndicatorSnapshot {
	cached := r.cachedIndicatorSnapshotsForKey(key)
	cached = snapshotsMatchingFeature(cached, snapshot.Feature)

	return appendIndicatorSnapshot(cached, snapshot, r.options.SnapshotCacheLimit)
}

func (r *Runner) cachedIndicatorSnapshotsForKey(key string) []model.IndicatorSnapshot {
	r.mu.Lock()
	cached := append([]model.IndicatorSnapshot(nil), r.indicatorSnapshots[key]...)
	r.mu.Unlock()
	return cached
}

func appendIndicatorSnapshot(
	cached []model.IndicatorSnapshot,
	snapshot model.IndicatorSnapshot,
	limit int,
) []model.IndicatorSnapshot {
	candidates := make([]model.IndicatorSnapshot, 0, len(cached)+1)
	for _, cachedSnapshot := range cached {
		if cachedSnapshot.OpenTime >= snapshot.OpenTime {
			continue
		}
		candidates = append(candidates, cachedSnapshot)
	}
	candidates = append(candidates, snapshot)
	return trimIndicatorSnapshots(candidates, limit)
}

func (r *Runner) cachedIndicatorSnapshotsForWindow(
	key string,
	closed []model.Kline,
) []model.IndicatorSnapshot {
	if len(closed) == 0 {
		return nil
	}

	r.mu.Lock()
	cached := append([]model.IndicatorSnapshot(nil), r.indicatorSnapshots[key]...)
	r.mu.Unlock()
	return alignedIndicatorSnapshotsInWindow(closed, cached, r.options.SnapshotCacheLimit)
}

func alignedIndicatorSnapshotsInWindow(
	closed []model.Kline,
	candidates []model.IndicatorSnapshot,
	limit int,
) []model.IndicatorSnapshot {
	if len(closed) == 0 || len(candidates) == 0 {
		return nil
	}
	windowIndexes := make(map[int64]int, len(closed))
	for index, kline := range closed {
		windowIndexes[kline.OpenTime] = index
	}
	for candidateEnd := len(candidates) - 1; candidateEnd >= 0; candidateEnd-- {
		windowIndex, ok := windowIndexes[candidates[candidateEnd].OpenTime]
		if !ok {
			continue
		}
		candidateIndex := candidateEnd
		for candidateIndex >= 0 && windowIndex >= 0 {
			if candidates[candidateIndex].OpenTime != closed[windowIndex].OpenTime {
				break
			}
			candidateIndex--
			windowIndex--
		}
		if candidateIndex < candidateEnd {
			return trimIndicatorSnapshots(candidates[candidateIndex+1:candidateEnd+1], limit)
		}
	}
	return nil
}

func snapshotsCoverKlines(snapshots []model.IndicatorSnapshot, klines []model.Kline) bool {
	if len(klines) == 0 || len(snapshots) < len(klines) {
		return false
	}
	offset := len(snapshots) - len(klines)
	for index, kline := range klines {
		if snapshots[offset+index].OpenTime != kline.OpenTime {
			return false
		}
	}
	return true
}

func validateIndicatorSnapshotContinuity(snapshots []model.IndicatorSnapshot, intervalMillis int64) error {
	if len(snapshots) <= 1 {
		return nil
	}
	ordered := append([]model.IndicatorSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].OpenTime < ordered[j].OpenTime
	})
	for index := 1; index < len(ordered); index++ {
		previous := ordered[index-1]
		current := ordered[index]
		expectedOpenTime := previous.OpenTime + intervalMillis
		if previous.CloseTime > 0 {
			expectedOpenTime = previous.CloseTime + 1
		}
		if current.OpenTime != expectedOpenTime {
			return fmt.Errorf(
				"indicator snapshot gap: previous_open_time=%d current_open_time=%d expected_open_time=%d",
				previous.OpenTime,
				current.OpenTime,
				expectedOpenTime,
			)
		}
	}
	return nil
}

func (r *Runner) cachedIndicatorSnapshots(
	key string,
	window *indicatorcalc.CalculationWindow,
	snapshot model.IndicatorSnapshot,
) []model.IndicatorSnapshot {
	if window == nil {
		return nil
	}
	closed := window.Klines()
	if len(closed) == 0 || closed[len(closed)-1].OpenTime != snapshot.OpenTime {
		return nil
	}

	r.mu.Lock()
	cached := append([]model.IndicatorSnapshot(nil), r.indicatorSnapshots[key]...)
	r.mu.Unlock()
	candidates := make([]model.IndicatorSnapshot, 0, len(cached)+1)
	for _, cachedSnapshot := range cached {
		if cachedSnapshot.OpenTime >= snapshot.OpenTime {
			continue
		}
		candidates = append(candidates, cachedSnapshot)
	}
	candidates = append(candidates, snapshot)
	return alignedIndicatorSnapshotSuffix(closed, candidates)
}

func alignedIndicatorSnapshotSuffix(
	closed []model.Kline,
	candidates []model.IndicatorSnapshot,
) []model.IndicatorSnapshot {
	if len(closed) == 0 || len(candidates) == 0 {
		return nil
	}
	windowIndex := len(closed) - 1
	candidateIndex := len(candidates) - 1
	for windowIndex >= 0 && candidateIndex >= 0 {
		if candidates[candidateIndex].OpenTime != closed[windowIndex].OpenTime {
			break
		}
		windowIndex--
		candidateIndex--
	}
	if candidateIndex == len(candidates)-1 {
		return nil
	}
	return trimIndicatorSnapshots(candidates[candidateIndex+1:], indicatorWindowLookback)
}

func (r *Runner) rememberIndicatorSnapshots(
	key string,
	snapshots []model.IndicatorSnapshot,
) []model.IndicatorSnapshot {
	trimmed := trimIndicatorSnapshots(snapshots, r.options.SnapshotCacheLimit)
	r.mu.Lock()
	r.indicatorSnapshots[key] = append([]model.IndicatorSnapshot(nil), trimmed...)
	r.mu.Unlock()
	return trimmed
}

func trimIndicatorSnapshots(snapshots []model.IndicatorSnapshot, limit int) []model.IndicatorSnapshot {
	if limit <= 0 {
		limit = indicatorWindowLookback
	}
	if len(snapshots) <= limit {
		return append([]model.IndicatorSnapshot(nil), snapshots...)
	}
	return append([]model.IndicatorSnapshot(nil), snapshots[len(snapshots)-limit:]...)
}
