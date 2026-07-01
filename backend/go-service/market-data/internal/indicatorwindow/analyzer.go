package indicatorwindow

import (
	"fmt"
	"sort"
	"strconv"

	"alphaflow/go-service/market-data/internal/model"
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
	if len(ordered) > defaultLookback {
		ordered = ordered[len(ordered)-defaultLookback:]
	}

	points := make([]point, 0, len(ordered))
	for _, snapshot := range ordered {
		points = append(points, pointFromSnapshot(snapshot))
	}
	last := ordered[len(ordered)-1]
	ctx := &analysisContext{
		values: map[string]string{
			"window_sample_count": strconv.Itoa(len(points)),
			"window_lookback":     strconv.Itoa(defaultLookback),
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
	addMoneyFlowWindowAnalysis(ctx)
	addStructureWindowAnalysis(ctx)
	addCandleWindowAnalysis(ctx)
	addPumpWindowAnalysis(ctx)
	addGenericWindowAnalysis(ctx)

	return Result{
		OpenTime:  last.OpenTime,
		CloseTime: last.CloseTime,
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
		series := numericSeries(ctx.points, key)
		if len(series) == 0 {
			continue
		}
		ctx.numericAnalyzed[key] = struct{}{}
		addNumericSeriesAnalysis(ctx.values, ctx.signals, key, series)
	}
}

func (ctx *analysisContext) addSignals(keys ...string) {
	for _, key := range keys {
		if _, ok := ctx.signalAnalyzed[key]; ok {
			continue
		}
		series := signalSeries(ctx.points, key)
		if len(series) == 0 {
			continue
		}
		ctx.signalAnalyzed[key] = struct{}{}
		addSignalSeriesAnalysis(ctx.values, ctx.signals, key, series)
	}
}
