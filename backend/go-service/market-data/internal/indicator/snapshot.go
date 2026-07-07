package indicator

import (
	"context"
	"fmt"
	"log/slog"

	"alphaflow/go-service/market-data/internal/model"
	"alphaflow/go-service/pkg/indicatorcalc"
	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketbus"
)

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
	window *indicatorcalc.CalculationWindow,
	snapshot model.IndicatorSnapshot,
) (model.IndicatorWindowSnapshot, error) {
	key := windowKey(rule.Exchange, rule.Market, symbol, interval)
	cached := r.cachedIndicatorSnapshots(key, window, snapshot)
	snapshots, err := r.indicatorSnapshotsForWindow(window, cached)
	if err != nil {
		return model.IndicatorWindowSnapshot{}, err
	}
	snapshots = r.rememberIndicatorSnapshots(key, snapshots)
	result, err := indicatorwindow.Analyze(snapshots)
	if err != nil {
		return model.IndicatorWindowSnapshot{}, err
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
		UpdatedAt: snapshot.UpdatedAt,
	}, nil
}

func (r *Runner) indicatorSnapshotsForWindow(
	window *indicatorcalc.CalculationWindow,
	cached []model.IndicatorSnapshot,
) ([]model.IndicatorSnapshot, error) {
	if window == nil {
		return nil, fmt.Errorf("nil calculation window")
	}
	closed := window.Klines()
	if len(closed) == 0 {
		return nil, fmt.Errorf("no closed klines")
	}
	lookback := 20
	if lookback > len(closed) {
		lookback = len(closed)
	}
	start := len(closed) - lookback
	cachedByOpenTime := map[int64]model.IndicatorSnapshot{}
	for _, snapshot := range cached {
		cachedByOpenTime[snapshot.OpenTime] = snapshot
	}
	snapshots := make([]model.IndicatorSnapshot, 0, lookback)
	calcWindow := newCalculationWindowFromKlines(closed[:start], len(closed))
	for index := start; index < len(closed); index++ {
		calcWindow.Append([]model.Kline{closed[index]})
		kline := closed[index]
		if cachedSnapshot, ok := cachedByOpenTime[kline.OpenTime]; ok {
			snapshots = append(snapshots, cachedSnapshot)
			continue
		}
		result, err := indicatorcalc.CalculateWindow(calcWindow, r.options.CalculateOptions)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, model.IndicatorSnapshot{
			Exchange:  kline.Exchange,
			Market:    kline.Market,
			Symbol:    kline.Symbol,
			Interval:  kline.Interval,
			OpenTime:  result.OpenTime,
			CloseTime: result.CloseTime,
			Values:    result.Values,
			Signals:   result.Signals,
			UpdatedAt: r.now().UnixMilli(),
		})
	}
	return snapshots, nil
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
	return trimIndicatorSnapshots(candidates[candidateIndex+1:])
}

func (r *Runner) rememberIndicatorSnapshots(
	key string,
	snapshots []model.IndicatorSnapshot,
) []model.IndicatorSnapshot {
	trimmed := trimIndicatorSnapshots(snapshots)
	r.mu.Lock()
	r.indicatorSnapshots[key] = append([]model.IndicatorSnapshot(nil), trimmed...)
	r.mu.Unlock()
	return trimmed
}

func trimIndicatorSnapshots(snapshots []model.IndicatorSnapshot) []model.IndicatorSnapshot {
	if len(snapshots) <= 20 {
		return append([]model.IndicatorSnapshot(nil), snapshots...)
	}
	return append([]model.IndicatorSnapshot(nil), snapshots[len(snapshots)-20:]...)
}
