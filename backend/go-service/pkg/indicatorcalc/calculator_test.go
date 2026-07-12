package indicatorcalc

import (
	"context"
	"errors"
	"testing"

	model "alphaflow/go-service/pkg/marketmodel"
)

func TestCalculateWindowsContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CalculateWindowsContext(ctx, []model.Kline{testKline(0, 100, true)}, 0, 1, DefaultOptions(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestCalculateCommonIndicators(t *testing.T) {
	klines := make([]model.Kline, 0, 120)
	for index := 0; index < 120; index++ {
		price := 100 + float64(index)
		klines = append(klines, testKline(int64(index), price, true))
	}

	result, err := Calculate(klines, DefaultOptions())
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	for _, key := range []string{
		"sma7",
		"ema25",
		"wma99",
		"hma21",
		"vwma20",
		"kama10",
		"alligator_jaw",
		"ma_group_spread_pct",
		"rsi14",
		"rsi_slope3",
		"macd",
		"macd_signal",
		"macd_hist",
		"macd_hist_delta",
		"macd_fast",
		"macd_fast_signal",
		"macd_fast_hist",
		"macd_fast_hist_delta",
		"kdj_k",
		"stoch_k",
		"stoch_rsi_k",
		"skdj_k",
		"cci20",
		"williams_r14",
		"roc12",
		"atr14",
		"natr14",
		"bb_upper",
		"volume_ma20",
		"volume_ratio5",
		"volume_ratio10",
		"volume_breakout_ratio",
		"volume_divergence_score",
		"obv",
		"adx14",
		"di_plus14",
		"donchian_high20",
		"donchian_mid20",
		"donchian_width_pct20",
		"donchian_position20",
		"keltner_upper20",
		"keltner_middle20",
		"keltner_lower20",
		"keltner_width_pct20",
		"keltner_position20",
		"qqe_line",
		"qqe_signal",
		"qqe_hist",
		"ut_stop",
		"ut_stop_distance_pct",
		"ssl_upper",
		"ssl_lower",
		"ssl_width_pct",
		"range_filter",
		"range_filter_upper",
		"range_filter_lower",
		"range_filter_distance_pct",
		"wvf",
		"wvf_upper_band",
		"wvf_range_high",
		"td_sell_setup_count",
		"nw_middle",
		"nw_upper",
		"nw_lower",
		"nw_width_pct",
		"nw_position",
		"vwap",
		"rolling_vwap20",
		"change_pct",
		"body_ratio",
		"volume_ratio20",
		"ha_close",
		"price_ema7_distance_pct",
		"script_dual_ma_out1",
		"script_dual_ma_out2",
		"script_dual_ma_out1_slope_pct",
		"script_dual_ma_out2_slope_pct",
		"script_ma_breakout_pct",
		"script_ma_mid_direction",
		"emd_avg",
		"emd_value",
		"emd_upper",
		"emd_lower",
		"supertrend",
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"supertrend_7_2",
		"supertrend_10_3",
		"supertrend_10_3_3",
		"supertrend_14_4",
		"alphatrend",
		"alphatrend_distance_pct",
		"alphatrend_slope_pct",
		"psar",
		"chandelier_long",
		"chandelier_short",
		"chandelier_stop_distance_pct",
		"mfi14",
		"cmf20",
		"ad_line",
		"squeeze_momentum",
		"bb_width_pct",
		"bb_percent_b",
		"bb_width_delta",
		"support_1",
		"resistance_1",
		"fib_382",
		"pivot_point",
		"ichimoku_tenkan",
		"order_block_high",
	} {
		if result.Values[key] == "" {
			t.Fatalf("missing indicator %s in %#v", key, result.Values)
		}
	}
	for _, key := range []string{
		"ema_alignment",
		"ma_state",
		"alligator_direction",
		"alligator_state",
		"ma_arrangement",
		"ma_cross",
		"ma_spread_state",
		"ma_compression",
		"ma_slope_state",
		"ma_breakout",
		"script_dual_ma_cross",
		"script_ma1_direction",
		"script_price_cross_ma1",
		"script_price_cross_ma2",
		"script_ma_signal",
		"emd_direction",
		"emd_cross",
		"rsi_state",
		"rsi_divergence",
		"stoch_rsi_state",
		"skdj_cross",
		"cci_state",
		"williams_state",
		"roc_state",
		"macd_divergence",
		"macd_fast_divergence",
		"trend_direction",
		"volatility_state",
		"adx_trend_strength",
		"di_direction",
		"candle_pattern",
		"ha_trend",
		"ha_strength",
		"sr_position",
		"fib_zone",
		"pivot_zone",
		"supertrend_direction",
		"supertrend_flip",
		"supertrend_7_2_direction",
		"supertrend_10_3_direction",
		"supertrend_10_3_3_direction",
		"supertrend_14_4_direction",
		"alphatrend_direction",
		"alphatrend_flip",
		"alphatrend_cross",
		"alphatrend_signal",
		"psar_direction",
		"chandelier_direction",
		"ichimoku_trend",
		"ichimoku_cloud",
		"ichimoku_cross",
		"cmf_state",
		"price_volume_action",
		"breakout_volume_confirm",
		"breakout_volume_strength",
		"volume_divergence",
		"volume_phase",
		"squeeze",
		"squeeze_state",
		"momentum_state",
		"bb_position",
		"bb_width_state",
		"bb_trend",
		"donchian_breakout",
		"keltner_breakout",
		"qqe_trend",
		"qqe_cross",
		"ut_direction",
		"ut_signal",
		"ssl_direction",
		"ssl_cross",
		"range_filter_direction",
		"wvf_state",
		"td_exhaustion",
		"nw_trend",
		"nw_position_state",
		"structure_event",
		"structure_bias",
	} {
		if result.Signals[key] == "" {
			t.Fatalf("missing signal %s in %#v", key, result.Signals)
		}
	}
	if result.OpenTime != 119000 {
		t.Fatalf("open time = %d, want 119000", result.OpenTime)
	}
	if result.Signals["data_quality"] != "ok" {
		t.Fatalf("data quality = %q, want ok", result.Signals["data_quality"])
	}
	if result.Values["sample_count"] != "120" {
		t.Fatalf("sample count = %q, want 120", result.Values["sample_count"])
	}
	if result.Values["required_count"] != "99" {
		t.Fatalf("required count = %q, want 99", result.Values["required_count"])
	}
}

func TestCalculateIgnoresOpenKline(t *testing.T) {
	klines := []model.Kline{
		testKline(1, 100, true),
		testKline(2, 101, true),
		testKline(3, 200, false),
	}

	result, err := Calculate(klines, Options{SMAPeriods: []int{2}})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.OpenTime != 2000 {
		t.Fatalf("open time = %d, want 2000", result.OpenTime)
	}
	if result.Values["sma2"] != "101.5" {
		t.Fatalf("sma2 = %q, want 101.5", result.Values["sma2"])
	}
}

func TestCalculateWindowNumericSkipsLegacyEncoding(t *testing.T) {
	klines := []model.Kline{
		testKline(1, 100, true),
		testKline(2, 101, true),
	}
	window := NewCalculationWindowFromKlines(klines, 0)
	result, err := CalculateWindowNumeric(window, Options{SMAPeriods: []int{2}})
	if err != nil {
		t.Fatalf("CalculateWindowNumeric: %v", err)
	}
	if result.Values != nil {
		t.Fatalf("legacy values were encoded: %#v", result.Values)
	}
	if result.NumericValues["sma2"] != 101.5 {
		t.Fatalf("numeric sma2 = %v, want 101.5", result.NumericValues["sma2"])
	}
}

func TestCalculateReportsInsufficientSamples(t *testing.T) {
	klines := []model.Kline{
		testKline(1, 100, true),
		testKline(2, 101, true),
	}

	result, err := Calculate(klines, Options{SMAPeriods: []int{3}})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.Signals["data_quality"] != "insufficient" {
		t.Fatalf("data quality = %q, want insufficient", result.Signals["data_quality"])
	}
	if result.Values["sample_count"] != "2" {
		t.Fatalf("sample count = %q, want 2", result.Values["sample_count"])
	}
	if result.Values["required_count"] != "3" {
		t.Fatalf("required count = %q, want 3", result.Values["required_count"])
	}
}

func TestCalculateReportsGap(t *testing.T) {
	klines := []model.Kline{
		testKline(1, 100, true),
		testKline(3, 101, true),
	}

	result, err := Calculate(klines, Options{SMAPeriods: []int{2}})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.Signals["data_quality"] != "gap" {
		t.Fatalf("data quality = %q, want gap", result.Signals["data_quality"])
	}
}

func TestCalculateReportsInvalidOHLC(t *testing.T) {
	klines := []model.Kline{
		testKline(1, 100, true),
	}
	klines[0].High = "99"

	result, err := Calculate(klines, Options{SMAPeriods: []int{1}})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.Signals["data_quality"] != "invalid_ohlc" {
		t.Fatalf("data quality = %q, want invalid_ohlc", result.Signals["data_quality"])
	}
	if result.Signals["data_quality_reason"] == "" {
		t.Fatal("expected data quality reason")
	}
}

func TestCalculationWindowAppendsAndTrims(t *testing.T) {
	klines := make([]model.Kline, 0, 10)
	for index := 0; index < 10; index++ {
		klines = append(klines, testKline(int64(index), 100+float64(index), true))
	}
	window := NewCalculationWindowFromKlines(klines[:5], 5)
	window.Append(klines[5:])

	if got := len(window.Klines()); got != 5 {
		t.Fatalf("window size = %d, want 5", got)
	}
	lastOpenTime, ok := window.LastOpenTime()
	if !ok {
		t.Fatal("missing last open time")
	}
	if want := klines[len(klines)-1].OpenTime; lastOpenTime != want {
		t.Fatalf("last open time = %d, want %d", lastOpenTime, want)
	}
	if got := window.Klines()[0].OpenTime; got != klines[5].OpenTime {
		t.Fatalf("first open time = %d, want %d", got, klines[5].OpenTime)
	}
}

func TestCalculateWindowMatchesCalculate(t *testing.T) {
	klines := make([]model.Kline, 0, 120)
	for index := 0; index < 120; index++ {
		klines = append(klines, testKline(int64(index), 100+float64(index), true))
	}
	window := NewCalculationWindowFromKlines(klines[:100], 120)
	window.Append(klines[100:])

	fromKlines, err := Calculate(klines, DefaultOptions())
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	fromWindow, err := CalculateWindow(window, DefaultOptions())
	if err != nil {
		t.Fatalf("CalculateWindow: %v", err)
	}
	for _, key := range []string{"ema7", "ema25", "rsi14", "macd", "bb_upper", "sample_count"} {
		if fromWindow.Values[key] != fromKlines.Values[key] {
			t.Fatalf("%s = %q, want %q", key, fromWindow.Values[key], fromKlines.Values[key])
		}
	}
	if fromWindow.Signals["data_quality"] != fromKlines.Signals["data_quality"] {
		t.Fatalf("data quality = %q, want %q", fromWindow.Signals["data_quality"], fromKlines.Signals["data_quality"])
	}
}

func TestCalculateWindowsMatchesSequentialCalculateWindow(t *testing.T) {
	klines := make([]model.Kline, 0, 310)
	for index := 0; index < 310; index++ {
		klines = append(klines, testKline(int64(index), 100+float64(index%50), true))
	}
	const start = 260
	const warmup = 250
	seedStart := start - (warmup - 1)
	sequentialWindow := NewCalculationWindowFromKlines(klines[seedStart:start], warmup)
	sequentialWindow.EnableBasicState()
	want := make([]Result, 0, len(klines)-start)
	for index := start; index < len(klines); index++ {
		sequentialWindow.Append([]model.Kline{klines[index]})
		result, err := CalculateWindow(sequentialWindow, DefaultOptions())
		if err != nil {
			t.Fatalf("CalculateWindow: %v", err)
		}
		want = append(want, result)
	}

	got, err := CalculateWindows(klines, start, warmup, DefaultOptions())
	if err != nil {
		t.Fatalf("CalculateWindows: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("results = %d, want %d", len(got), len(want))
	}
	for index := range got {
		assertResultEqual(t, index, got[index], want[index])
	}
}

func assertResultEqual(t *testing.T, index int, got Result, want Result) {
	t.Helper()
	if got.OpenTime != want.OpenTime || got.CloseTime != want.CloseTime {
		t.Fatalf("result[%d] time = %d/%d, want %d/%d", index, got.OpenTime, got.CloseTime, want.OpenTime, want.CloseTime)
	}
	assertStringMapEqual(t, index, "values", got.Values, want.Values)
	assertStringMapEqual(t, index, "signals", got.Signals, want.Signals)
}

func assertStringMapEqual(t *testing.T, index int, name string, got map[string]string, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("result[%d] %s len = %d, want %d", index, name, len(got), len(want))
	}
	for key, wantValue := range want {
		if gotValue := got[key]; gotValue != wantValue {
			t.Fatalf("result[%d] %s[%s] = %q, want %q", index, name, key, gotValue, wantValue)
		}
	}
}

func testKline(index int64, price float64, closed bool) model.Kline {
	return model.Kline{
		Exchange:    "binance",
		Market:      "um",
		Symbol:      "ETHUSDT",
		Interval:    "1m",
		OpenTime:    index * 1000,
		CloseTime:   index*1000 + 999,
		Open:        format(price),
		High:        format(price + 2),
		Low:         format(price - 2),
		Close:       format(price + 1),
		Volume:      format(10 + float64(index%5)),
		QuoteVolume: format((10 + float64(index%5)) * price),
		IsClosed:    closed,
	}
}
