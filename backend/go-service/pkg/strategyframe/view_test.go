package strategyframe

import (
	"fmt"
	"reflect"
	"testing"

	"alphaflow/go-service/pkg/indicatorwindow"
	"alphaflow/go-service/pkg/marketmodel"
)

func TestWindowViewAcceptsOnlineAndAnalyzerSampleCountKeys(t *testing.T) {
	for _, key := range []string{"sample_count", "window_sample_count"} {
		view, err := WindowView(marketmodel.IndicatorWindowSnapshot{
			Values: map[string]string{
				key:                 "20",
				"rsi_win_latest":    "55",
				"rsi_win_direction": "up",
			},
			Signals: map[string]string{"trend_win_latest": "bullish"},
		})
		if err != nil {
			t.Fatalf("WindowView(%s) error = %v", key, err)
		}
		if view.SampleCount != 20 || view.Values["rsi"].Latest != 55 || view.Values["rsi"].Direction != "up" {
			t.Fatalf("WindowView(%s) = %#v", key, view)
		}
	}
}

func BenchmarkWindowViewFromResult(b *testing.B) {
	values := make(map[string]string, 280)
	signals := make(map[string]string, 150)
	values["window_sample_count"] = "20"
	for index := 0; index < 25; index++ {
		prefix := fmt.Sprintf("indicator_%d_win_", index)
		values[prefix+"latest"] = "123.456"
		values[prefix+"previous"] = "122.345"
		values[prefix+"change"] = "1.111"
		values[prefix+"change_pct"] = "0.908"
		values[prefix+"slope"] = "0.058"
		values[prefix+"rising_count"] = "3"
		values[prefix+"falling_count"] = "0"
		values[prefix+"min"] = "118.2"
		values[prefix+"max"] = "124.1"
		values[prefix+"range_position_pct"] = "89.08"
		signals[prefix+"direction"] = "rising"
	}
	for index := 0; index < 25; index++ {
		prefix := fmt.Sprintf("signal_%d_win_", index)
		signals[prefix+"latest"] = "bullish"
		signals[prefix+"previous"] = "neutral"
		signals[prefix+"changed"] = "true"
		values[prefix+"stable_count"] = "4"
		values[prefix+"last_changed_ago"] = "4"
	}
	result := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		Values: values, Signals: signals,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := WindowViewFromResult(result, result.CloseTime); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWindowViewFromResultMatchesSnapshotPath(t *testing.T) {
	result := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		Values:  map[string]string{"window_sample_count": "2", "rsi_win_latest": "55"},
		Signals: map[string]string{"trend_win_latest": "bullish"},
	}
	fromResult, err := WindowViewFromResult(result, 2000)
	if err != nil {
		t.Fatal(err)
	}
	fromSnapshot, err := WindowView(marketmodel.IndicatorWindowSnapshot{
		OpenTime: result.OpenTime, CloseTime: result.CloseTime, Version: result.Version,
		Values: result.Values, Signals: result.Signals, UpdatedAt: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromResult, fromSnapshot) {
		t.Fatalf("result path = %#v snapshot path = %#v", fromResult, fromSnapshot)
	}
}
