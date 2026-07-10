package strategyframe

import (
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
