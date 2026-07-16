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
	result, _ := benchmarkWindowResults()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := WindowViewFromResult(result, result.CloseTime); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWindowViewFromTypedResult(b *testing.B) {
	_, result := benchmarkWindowResults()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := WindowViewFromResult(result, result.CloseTime); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWindowViewBuilderFromTypedResult(b *testing.B) {
	_, result := benchmarkWindowResults()
	builder := NewWindowViewBuilder()
	if _, err := builder.FromResult(result, result.CloseTime); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := builder.FromResult(result, result.CloseTime); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWindowViewBuilderFromGroupedResult(b *testing.B) {
	result := benchmarkGroupedWindowResult()
	builder := NewWindowViewBuilder()
	if _, err := builder.FromResult(result, result.CloseTime); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := builder.FromResult(result, result.CloseTime); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGroupedWindowResult() indicatorwindow.Result {
	result := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		NumericValues: map[string]float64{"window_sample_count": 20},
		Signals:       map[string]string{"window_version": "v1"},
	}
	for index := 0; index < 25; index++ {
		result.NumericWindows = append(result.NumericWindows, indicatorwindow.NumericWindow{
			Name: fmt.Sprintf("indicator_%d", index), Count: 20,
			Latest: 123.456, Previous: 122.345, Change: 1.111, ChangePct: 0.908,
			Slope: 0.058, Direction: "rising", RisingCount: 3,
			Minimum: 118.2, Maximum: 124.1, RangePositionPct: 89.08,
		})
		result.SignalWindows = append(result.SignalWindows, indicatorwindow.SignalWindow{
			Name: fmt.Sprintf("signal_%d", index), Count: 20,
			Latest: "bullish", Previous: "neutral", StableCount: 4, LastChangedAgo: 4,
		})
	}
	return result
}

func TestWindowViewBuilderGroupedResultMatchesTypedResult(t *testing.T) {
	typedAnalyzer := indicatorwindow.NewOrderedAnalyzer()
	denseAnalyzer := indicatorwindow.NewOrderedAnalyzer()
	typed := indicatorwindow.Result{}
	dense := indicatorwindow.Result{}
	for index := 0; index < 30; index++ {
		snapshot := marketmodel.IndicatorSnapshot{
			OpenTime: int64(index + 1), CloseTime: int64(index + 2),
			NumericValues: map[string]float64{
				"ema7": float64(100 + index), "rsi14": float64(40 + index%20),
			},
			Signals: map[string]string{"ema_alignment": []string{"bull", "bear"}[index%2]},
		}
		if index >= 10 {
			snapshot.NumericValues["dynamic_numeric"] = float64(index)
			snapshot.Signals["dynamic_signal"] = "ready"
		}
		if err := typedAnalyzer.AppendTypedInto(snapshot, &typed); err != nil {
			t.Fatal(err)
		}
		if err := denseAnalyzer.AppendDenseInto(snapshot, &dense); err != nil {
			t.Fatal(err)
		}
	}
	legacyView, err := WindowViewFromResult(typed, typed.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	builtView, err := NewWindowViewBuilder().FromResult(dense, dense.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	if builtView.SampleCount != legacyView.SampleCount {
		t.Fatalf("sample count = %d, want %d", builtView.SampleCount, legacyView.SampleCount)
	}
	for key, want := range legacyView.Values {
		got, ok := builtView.Numeric(key)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("grouped numeric %s = %#v/%v, want %#v", key, got, ok, want)
		}
	}
	for key, want := range legacyView.Signals {
		got, ok := builtView.Signal(key)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("grouped signal %s = %#v/%v, want %#v", key, got, ok, want)
		}
	}
}

func benchmarkWindowResults() (indicatorwindow.Result, indicatorwindow.Result) {
	values := make(map[string]string, 280)
	numericValues := make(map[string]float64, 280)
	signals := make(map[string]string, 150)
	values["window_sample_count"] = "20"
	numericValues["window_sample_count"] = 20
	for index := 0; index < 25; index++ {
		prefix := fmt.Sprintf("indicator_%d_win_", index)
		values[prefix+"latest"] = "123.456"
		numericValues[prefix+"latest"] = 123.456
		values[prefix+"previous"] = "122.345"
		numericValues[prefix+"previous"] = 122.345
		values[prefix+"change"] = "1.111"
		numericValues[prefix+"change"] = 1.111
		values[prefix+"change_pct"] = "0.908"
		numericValues[prefix+"change_pct"] = 0.908
		values[prefix+"slope"] = "0.058"
		numericValues[prefix+"slope"] = 0.058
		values[prefix+"rising_count"] = "3"
		numericValues[prefix+"rising_count"] = 3
		values[prefix+"falling_count"] = "0"
		numericValues[prefix+"falling_count"] = 0
		values[prefix+"stable_count"] = "0"
		numericValues[prefix+"stable_count"] = 0
		values[prefix+"min"] = "118.2"
		numericValues[prefix+"min"] = 118.2
		values[prefix+"max"] = "124.1"
		numericValues[prefix+"max"] = 124.1
		values[prefix+"range_pos_pct"] = "89.08"
		numericValues[prefix+"range_pos_pct"] = 89.08
		signals[prefix+"direction"] = "rising"
	}
	for index := 0; index < 25; index++ {
		prefix := fmt.Sprintf("signal_%d_win_", index)
		signals[prefix+"latest"] = "bullish"
		signals[prefix+"previous"] = "neutral"
		signals[prefix+"changed"] = "true"
		values[prefix+"stable_count"] = "4"
		numericValues[prefix+"stable_count"] = 4
		values[prefix+"last_changed_ago"] = "4"
		numericValues[prefix+"last_changed_ago"] = 4
	}
	legacy := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		Values: values, Signals: signals,
	}
	typed := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		NumericValues: numericValues, Signals: signals,
	}
	return legacy, typed
}

func TestWindowViewFromTypedResultMatchesLegacyResult(t *testing.T) {
	legacy, typed := benchmarkWindowResults()
	legacyView, err := WindowViewFromResult(legacy, legacy.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	typedView, err := WindowViewFromResult(typed, typed.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(typedView, legacyView) {
		t.Fatalf("typed result path differs from legacy: typed=%#v legacy=%#v", typedView, legacyView)
	}
	builder := NewWindowViewBuilder()
	builtView, err := builder.FromResult(typed, typed.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(builtView.Values) != 0 || len(builtView.Signals) != 0 {
		t.Fatalf("cached builder materialized compatibility maps: %#v", builtView)
	}
	for key, want := range legacyView.Values {
		got, ok := builtView.Numeric(key)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("cached numeric %s = %#v, want %#v", key, got, want)
		}
	}
	for key, want := range legacyView.Signals {
		got, ok := builtView.Signal(key)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("cached signal %s = %#v, want %#v", key, got, want)
		}
	}
	previousView := builtView
	typed.NumericValues["future_indicator"] = 42
	typed.Signals["future_signal"] = "ready"
	builtView, err = builder.FromResult(typed, typed.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	futureNumeric, numericOK := builtView.Numeric("future_indicator")
	futureSignal, signalOK := builtView.Signal("future_signal")
	if !numericOK || futureNumeric.Latest != 42 || !signalOK || futureSignal.Latest != "ready" {
		t.Fatalf("builder did not discover new fields: %#v", builtView)
	}
	if _, ok := previousView.Numeric("future_indicator"); ok {
		t.Fatalf("previous dense view unexpectedly exposes future numeric field: %#v", previousView)
	}
	if _, ok := previousView.Signal("future_signal"); ok {
		t.Fatalf("previous dense view unexpectedly exposes future signal field: %#v", previousView)
	}
}

func TestWindowViewBuilderCompactStoragePreservesAllFields(t *testing.T) {
	result := indicatorwindow.Result{
		OpenTime: 1000, CloseTime: 2000, Version: "v1",
		Values: map[string]string{
			"window_sample_count":             "19",
			"complete_win_latest":             "123.5",
			"complete_win_previous":           "120.25",
			"complete_win_change":             "3.25",
			"complete_win_change_pct":         "2.7027",
			"complete_win_slope":              "0.75",
			"complete_win_direction":          "rising",
			"complete_win_rising_count":       "17",
			"complete_win_falling_count":      "2",
			"complete_win_min":                "110",
			"complete_win_max":                "130",
			"complete_win_range_position_pct": "67.5",
		},
		Signals: map[string]string{
			"state_win_latest":           "bullish",
			"state_win_previous":         "neutral",
			"state_win_changed":          "true",
			"state_win_stable_count":     "9",
			"state_win_last_changed_ago": "4",
		},
	}

	wantView, err := WindowViewFromResult(result, result.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	gotView, err := NewWindowViewBuilder().FromResult(result, result.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	if gotView.SampleCount != wantView.SampleCount {
		t.Fatalf("sample count = %d, want %d", gotView.SampleCount, wantView.SampleCount)
	}
	gotNumeric, numericOK := gotView.Numeric("complete")
	if want := wantView.Values["complete"]; !numericOK || !reflect.DeepEqual(gotNumeric, want) {
		t.Fatalf("compact numeric = %#v/%v, want %#v", gotNumeric, numericOK, want)
	}
	gotSignal, signalOK := gotView.Signal("state")
	if want := wantView.Signals["state"]; !signalOK || !reflect.DeepEqual(gotSignal, want) {
		t.Fatalf("compact signal = %#v/%v, want %#v", gotSignal, signalOK, want)
	}
	if len(gotView.DenseDirections) == 0 {
		t.Fatal("numeric direction side storage was not retained")
	}
}

func TestWindowViewBuilderDiscoversSameCountReplacement(t *testing.T) {
	_, result := benchmarkWindowResults()
	builder := NewWindowViewBuilder()
	if _, err := builder.FromResult(result, result.CloseTime); err != nil {
		t.Fatal(err)
	}
	delete(result.NumericValues, "indicator_0_win_latest")
	result.NumericValues["replacement_win_latest"] = 77
	delete(result.Signals, "signal_0_win_latest")
	result.Signals["replacement_signal_win_latest"] = "ready"

	view, err := builder.FromResult(result, result.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	numeric, numericOK := view.Numeric("replacement")
	signal, signalOK := view.Signal("replacement_signal")
	if !numericOK || numeric.Latest != 77 || !signalOK || signal.Latest != "ready" {
		t.Fatalf("same-count replacement not discovered: %#v", view)
	}
}

func TestWindowViewBuilderDensePresenceDistinguishesZeroAndMissing(t *testing.T) {
	_, result := benchmarkWindowResults()
	result.NumericValues["zero_value_win_latest"] = 0
	result.Signals["empty_signal_win_latest"] = ""
	builder := NewWindowViewBuilder()

	view, err := builder.FromResult(result, result.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	numeric, numericOK := view.Numeric("zero_value")
	signal, signalOK := view.Signal("empty_signal")
	if !numericOK || numeric.Latest != 0 || !signalOK || signal.Latest != "" {
		t.Fatalf("valid zero values missing: numeric=%#v/%v signal=%#v/%v", numeric, numericOK, signal, signalOK)
	}

	delete(result.NumericValues, "zero_value_win_latest")
	delete(result.Signals, "empty_signal_win_latest")
	view, err = builder.FromResult(result, result.CloseTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Numeric("zero_value"); ok {
		t.Fatal("missing numeric field marked present")
	}
	if _, ok := view.Signal("empty_signal"); ok {
		t.Fatal("missing signal field marked present")
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
