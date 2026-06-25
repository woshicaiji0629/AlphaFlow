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
		"rsi14",
		"rsi_slope3",
		"macd",
		"macd_signal",
		"macd_hist",
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
		"obv",
		"adx14",
		"di_plus14",
		"donchian_high20",
		"vwap",
		"change_pct",
		"body_ratio",
		"volume_ratio20",
		"price_ema7_distance_pct",
		"supertrend",
		"alphatrend",
		"mfi14",
		"squeeze_momentum",
		"bb_width_pct",
		"bb_percent_b",
		"support_1",
		"resistance_1",
		"order_block_high",
	} {
		if result.Values[key] == "" {
			t.Fatalf("missing indicator %s in %#v", key, result.Values)
		}
	}
	for _, key := range []string{
		"ema_alignment",
		"ma_state",
		"rsi_state",
		"stoch_rsi_state",
		"skdj_cross",
		"cci_state",
		"williams_state",
		"roc_state",
		"trend_direction",
		"volatility_state",
		"adx_trend_strength",
		"di_direction",
		"candle_pattern",
		"sr_position",
		"supertrend_direction",
		"alphatrend_direction",
		"squeeze",
		"momentum_state",
		"bb_position",
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
