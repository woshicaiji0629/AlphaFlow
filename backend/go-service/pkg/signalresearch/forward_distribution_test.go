package signalresearch

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
)

func TestBuildForwardDistributionSummarizesEveryEligibleBar(t *testing.T) {
	minute := int64(time.Minute / time.Millisecond)
	start := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	bars := make([]marketmodel.Kline, 0, 7)
	for index, closePrice := range []float64{100, 101, 102, 103, 104, 105, 106} {
		bar := forwardKline(start+int64(index)*3*minute, closePrice+1, closePrice-1, closePrice)
		bar.OpenTime = start + int64(index)*3*minute
		bar.CloseTime = bar.OpenTime + 3*minute - 1
		bars = append(bars, bar)
	}

	report, err := BuildForwardDistribution(bars, start, start+4*3*minute, 3*minute, []int{2, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.CandidateSamples != 4 || len(report.Horizons) != 2 {
		t.Fatalf("report=%#v", report)
	}
	for _, horizon := range report.Horizons {
		if horizon.ValidSamples != 4 || horizon.InvalidSamples != 0 || horizon.MonthlySamples["2024-08"] != 4 {
			t.Fatalf("horizon=%#v", horizon)
		}
		if horizon.Metrics["direction_return_bps"].Count != 4 {
			t.Fatalf("metrics=%#v", horizon.Metrics)
		}
		if horizon.MonthlyMetrics["2024-08"]["direction_return_bps"].Count != 4 {
			t.Fatalf("monthly metrics=%#v", horizon.MonthlyMetrics)
		}
		if horizon.Metrics["absolute_direction_return_bps"].Count != 4 || horizon.Metrics["total_excursion_bps"].Count != 4 {
			t.Fatalf("derived metrics=%#v", horizon.Metrics)
		}
		wantRate := float64(1)
		if horizon.HorizonBars == 1 {
			wantRate = 0
		}
		if horizon.Rates["midpoint_late_same_direction"].Rate != wantRate || horizon.MonthlyRates["2024-08"]["midpoint_late_same_direction"].Rate != wantRate {
			t.Fatalf("rates=%#v monthly=%#v", horizon.Rates, horizon.MonthlyRates)
		}
	}
	if report.PercentileMethod != "nearest_rank" {
		t.Fatalf("percentile method=%q", report.PercentileMethod)
	}
	if report.Horizons[0].HorizonBars != 1 || report.Horizons[1].HorizonBars != 2 {
		t.Fatalf("horizons=%#v", report.Horizons)
	}
}

func TestBuildForwardDistributionCountsInvalidFuturePaths(t *testing.T) {
	start := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	bars := []marketmodel.Kline{
		forwardKline(start, 101, 99, 100),
		forwardKline(start+1, 102, 100, 101),
		forwardKline(start+3, 103, 101, 102),
	}
	bars[0].OpenTime = start
	bars[1].OpenTime = start + 1
	bars[2].OpenTime = start + 3

	report, err := BuildForwardDistribution(bars, start, start+1, 1, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	if report.Horizons[0].ValidSamples != 0 || report.Horizons[0].InvalidSamples != 1 {
		t.Fatalf("horizon=%#v", report.Horizons[0])
	}
}

func TestBuildForwardDistributionRejectsInvalidConfiguration(t *testing.T) {
	if _, err := BuildForwardDistribution(nil, 2, 1, 1, []int{20}); err == nil {
		t.Fatal("expected invalid range error")
	}
	if _, err := BuildForwardDistribution(nil, 1, 2, 1, nil); err == nil {
		t.Fatal("expected missing horizon error")
	}
	if _, err := BuildForwardDistribution(nil, 1, 2, 0, []int{20}); err == nil {
		t.Fatal("expected invalid interval error")
	}
}
