package signalresearch

import (
	"math"
	"strconv"
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
)

func TestCalculateForwardMetricsMeasuresPathWithoutIntrabarOrdering(t *testing.T) {
	bars := []marketmodel.Kline{
		forwardKline(1, 104, 99, 103),
		forwardKline(2, 106, 102, 105),
		forwardKline(3, 103, 97, 98),
		forwardKline(4, 101, 96, 100),
	}

	metrics, err := CalculateForwardMetrics(100, bars, 4)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Version != ForwardLabelVersion || metrics.ObservedBars != 4 {
		t.Fatalf("identity=%#v", metrics)
	}
	assertClose(t, "return", metrics.DirectionReturnBps, 0)
	assertClose(t, "midpoint return", metrics.MidpointReturnBps, 500)
	assertClose(t, "upside", metrics.MaxUpsideBps, 600)
	assertClose(t, "downside", metrics.MaxDownsideBps, 400)
	assertClose(t, "path efficiency", metrics.PathEfficiency, 0)
	assertClose(t, "close drawdown", metrics.MaxCloseDrawdownBps, (105.0-98.0)/105.0*10000)
	assertClose(t, "close recovery", metrics.MaxCloseRecoveryBps, 500)
	if !metrics.DominantExcursionIsUpward || metrics.DominantExcursionBar != 2 {
		t.Fatalf("dominant excursion=%#v", metrics)
	}
	assertClose(t, "dominant giveback", metrics.DominantGivebackBps, 600)
	assertClose(t, "dominant giveback ratio", metrics.DominantGivebackRatio, 1)
	assertClose(t, "dominant position", metrics.DominantExcursionPosition, 0.5)
	assertClose(t, "directional advantage", metrics.DirectionalAdvantage, 0.2)
	assertClose(t, "dominant retention", metrics.DominantRetention, 0)
	assertClose(t, "late return", metrics.LateReturnBps, -500)
	assertClose(t, "phase expansion", metrics.PhaseExpansion, 1)
	if metrics.RealizedVolatilityBps <= 0 {
		t.Fatalf("realized volatility=%v", metrics.RealizedVolatilityBps)
	}
}

func TestCalculateForwardMetricsRejectsIncompleteOrInvalidPaths(t *testing.T) {
	valid := forwardKline(1, 101, 99, 100)
	tests := []struct {
		name    string
		entry   float64
		bars    []marketmodel.Kline
		horizon int
	}{
		{name: "entry", entry: 0, bars: []marketmodel.Kline{valid}, horizon: 1},
		{name: "horizon", entry: 100, bars: []marketmodel.Kline{valid}, horizon: 0},
		{name: "short path", entry: 100, bars: []marketmodel.Kline{valid}, horizon: 2},
		{name: "open bar", entry: 100, bars: []marketmodel.Kline{{High: "101", Low: "99", Close: "100"}}, horizon: 1},
		{name: "bad range", entry: 100, bars: []marketmodel.Kline{forwardKline(1, 99, 101, 100)}, horizon: 1},
		{name: "close outside range", entry: 100, bars: []marketmodel.Kline{forwardKline(1, 101, 99, 102)}, horizon: 1},
		{name: "time reversal", entry: 100, bars: []marketmodel.Kline{valid, forwardKline(1, 101, 99, 100)}, horizon: 2},
		{name: "time gap", entry: 100, bars: []marketmodel.Kline{valid, forwardKline(2, 101, 99, 100), forwardKline(4, 101, 99, 100)}, horizon: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CalculateForwardMetrics(test.entry, test.bars, test.horizon); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDefaultForwardHorizonsAreFrozen(t *testing.T) {
	want := [3]int{20, 40, 80}
	if DefaultForwardHorizons != want {
		t.Fatalf("horizons=%v, want %v", DefaultForwardHorizons, want)
	}
}

func forwardKline(closeTime int64, high float64, low float64, closePrice float64) marketmodel.Kline {
	return marketmodel.Kline{
		OpenTime: closeTime - 1, CloseTime: closeTime,
		High: strconv.FormatFloat(high, 'f', -1, 64), Low: strconv.FormatFloat(low, 'f', -1, 64),
		Close: strconv.FormatFloat(closePrice, 'f', -1, 64), IsClosed: true,
	}
}

func assertClose(t *testing.T, name string, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s=%v, want %v", name, got, want)
	}
}
