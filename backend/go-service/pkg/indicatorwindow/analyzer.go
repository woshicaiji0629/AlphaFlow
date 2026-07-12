package indicatorwindow

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	model "alphaflow/go-service/pkg/marketmodel"
)

type analysisContext struct {
	values          map[string]string
	signals         map[string]string
	points          []point
	numericAnalyzed map[string]struct{}
	signalAnalyzed  map[string]struct{}
}

func Analyze(snapshots []model.IndicatorSnapshot) (Result, error) {
	if len(snapshots) == 0 {
		return Result{}, fmt.Errorf("no indicator snapshots")
	}
	ordered := append([]model.IndicatorSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].OpenTime < ordered[j].OpenTime
	})
	if len(ordered) > DefaultLookback {
		ordered = ordered[len(ordered)-DefaultLookback:]
	}
	return analyzeOrdered(ordered)
}

// AnalyzeOrdered analyzes snapshots already ordered by ascending OpenTime.
// Callers must not insert or update older snapshots out of order.
func AnalyzeOrdered(snapshots []model.IndicatorSnapshot) (Result, error) {
	if len(snapshots) == 0 {
		return Result{}, fmt.Errorf("no indicator snapshots")
	}
	if len(snapshots) > DefaultLookback {
		snapshots = snapshots[len(snapshots)-DefaultLookback:]
	}
	return analyzeOrdered(snapshots)
}

func analyzeOrdered(snapshots []model.IndicatorSnapshot) (Result, error) {
	points := make([]point, 0, len(snapshots))
	for _, snapshot := range snapshots {
		points = append(points, pointFromSnapshot(snapshot))
	}
	return analyzePoints(points)
}

// CalculateWindows analyzes every ordered suffix once while reusing the
// snapshot ordering and point conversion work across results.
func CalculateWindows(snapshots []model.IndicatorSnapshot) ([]Result, error) {
	return CalculateWindowsContext(context.Background(), snapshots, nil)
}

// CalculateWindowsContext analyzes every ordered suffix while allowing long
// backtest preparations to be cancelled and observed.
func CalculateWindowsContext(
	ctx context.Context,
	snapshots []model.IndicatorSnapshot,
	progress func(processed int, total int),
) ([]Result, error) {
	if len(snapshots) == 0 {
		return nil, nil
	}
	ordered := append([]model.IndicatorSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].OpenTime < ordered[j].OpenTime
	})
	points := make([]point, 0, len(ordered))
	for _, snapshot := range ordered {
		points = append(points, pointFromSnapshot(snapshot))
	}
	results := make([]Result, 0, len(points))
	for index := range points {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := index + 1 - DefaultLookback
		if start < 0 {
			start = 0
		}
		result, err := analyzePoints(points[start : index+1])
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		if progress != nil {
			progress(len(results), len(points))
		}
	}
	return results, nil
}

func analyzePoints(points []point) (Result, error) {
	if len(points) == 0 {
		return Result{}, fmt.Errorf("no indicator points")
	}
	last := points[len(points)-1]
	ctx := &analysisContext{
		values: map[string]string{
			"window_sample_count": strconv.Itoa(len(points)),
			"window_lookback":     strconv.Itoa(DefaultLookback),
		},
		signals: map[string]string{
			"window_version": Version,
		},
		points:          points,
		numericAnalyzed: map[string]struct{}{},
		signalAnalyzed:  map[string]struct{}{},
	}

	addMovingAverageWindowAnalysis(ctx)
	addMACDWindowAnalysis(ctx)
	addMomentumWindowAnalysis(ctx)
	addVolatilityWindowAnalysis(ctx)
	addTradingViewWindowAnalysis(ctx)
	addMoneyFlowWindowAnalysis(ctx)
	addStructureWindowAnalysis(ctx)
	addCandleWindowAnalysis(ctx)
	addPumpWindowAnalysis(ctx)
	addGenericWindowAnalysis(ctx)

	return Result{
		OpenTime:  last.openTime,
		CloseTime: last.closeTime,
		Version:   Version,
		Values:    ctx.values,
		Signals:   ctx.signals,
	}, nil
}

func (ctx *analysisContext) addNumeric(keys ...string) {
	for _, key := range keys {
		if _, ok := ctx.numericAnalyzed[key]; ok {
			continue
		}
		stats, ok := numericStatsFromPoints(ctx.points, key)
		if !ok {
			continue
		}
		ctx.numericAnalyzed[key] = struct{}{}
		addNumericStatsAnalysis(ctx.values, ctx.signals, key, stats)
	}
}

func (ctx *analysisContext) addSignals(keys ...string) {
	for _, key := range keys {
		if _, ok := ctx.signalAnalyzed[key]; ok {
			continue
		}
		stats, ok := signalStatsFromPoints(ctx.points, key)
		if !ok {
			continue
		}
		ctx.signalAnalyzed[key] = struct{}{}
		addSignalStatsAnalysis(ctx.values, ctx.signals, key, stats)
	}
}
