package indicator

import (
	"testing"

	"alphaflow/go-service/market-data/internal/model"
)

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
		"vwap",
		"rolling_vwap20",
		"change_pct",
		"body_ratio",
		"volume_ratio20",
		"ha_close",
		"price_ema7_distance_pct",
		"supertrend",
		"supertrend_distance_pct",
		"supertrend_stop_distance_pct",
		"supertrend_7_2",
		"supertrend_10_3",
		"supertrend_14_4",
		"alphatrend",
		"alphatrend_distance_pct",
		"alphatrend_slope_pct",
		"psar",
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
		"rsi_state",
		"rsi_divergence",
		"stoch_rsi_state",
		"skdj_cross",
		"cci_state",
		"williams_state",
		"roc_state",
		"macd_divergence",
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
		"supertrend_14_4_direction",
		"alphatrend_direction",
		"alphatrend_flip",
		"psar_direction",
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
		"momentum_state",
		"bb_position",
		"bb_width_state",
		"bb_trend",
	} {
		if result.Signals[key] == "" {
			t.Fatalf("missing signal %s in %#v", key, result.Signals)
		}
	}
	if result.OpenTime != 119000 {
		t.Fatalf("open time = %d, want 119000", result.OpenTime)
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
