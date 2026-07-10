package indicatorwindow

import (
	"fmt"
	"reflect"
	"testing"

	model "alphaflow/go-service/pkg/marketmodel"
)

func TestCalculateWindowsMatchesAnalyzePrefixes(t *testing.T) {
	snapshots := benchmarkSnapshots(40)
	results, err := CalculateWindows(snapshots)
	if err != nil {
		t.Fatalf("CalculateWindows: %v", err)
	}
	if len(results) != len(snapshots) {
		t.Fatalf("results = %d, want %d", len(results), len(snapshots))
	}
	for index := range snapshots {
		want, err := Analyze(snapshots[:index+1])
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(results[index], want) {
			t.Fatalf("result[%d] does not match Analyze prefix", index)
		}
	}
}

func BenchmarkCalculateWindows300(b *testing.B) {
	snapshots := benchmarkSnapshots(300)
	b.ReportAllocs()
	for range b.N {
		if _, err := CalculateWindows(snapshots); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnalyzePrefixes300(b *testing.B) {
	snapshots := benchmarkSnapshots(300)
	b.ReportAllocs()
	for range b.N {
		for index := range snapshots {
			if _, err := Analyze(snapshots[:index+1]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func benchmarkSnapshots(count int) []model.IndicatorSnapshot {
	snapshots := make([]model.IndicatorSnapshot, 0, count)
	for index := 0; index < count; index++ {
		snapshots = append(snapshots, testSnapshot(int64(index+1), map[string]string{
			"ema7": fmt.Sprintf("%d", 100+index), "macd_hist": fmt.Sprintf("%d", index%7),
			"rsi14": fmt.Sprintf("%d", 40+index%30), "volume_ratio20": "1.2",
		}, map[string]string{
			"ema_alignment": "bull", "supertrend_direction": "up",
		}))
	}
	return snapshots
}

func TestAnalyzeBuildsWindowSnapshotFromIndicatorSequence(t *testing.T) {
	snapshots := []model.IndicatorSnapshot{
		testSnapshot(1, map[string]string{
			"ema7":                            "100",
			"ema_spread_pct":                  "0.1",
			"ema25_slope5_pct":                "0.03",
			"ez_ema_fast":                     "100",
			"ez_ema_group_spread_pct":         "0.1",
			"macd_hist":                       "-0.2",
			"macd_zero_distance":              "-0.2",
			"rsi14":                           "45",
			"volume_ratio20":                  "1",
			"cmf20":                           "0.01",
			"volume_profile_poc_distance_pct": "0.1",
			"volume_profile_vah_distance_pct": "3",
			"volume_profile_val_distance_pct": "2",
			"supertrend_distance_pct":         "0.2",
			"support_distance_pct":            "2",
			"body_ratio":                      "0.4",
			"close":                           "100",
			"custom_x":                        "1",
		}, map[string]string{
			"ema_alignment":                   "bear",
			"ma_cross":                        "none",
			"ez_ema_cross":                    "none",
			"ez_ema_stack":                    "bear",
			"ez_ema_compression":              "normal",
			"volume_profile_position":         "inside_value_area",
			"volume_profile_poc_side":         "at",
			"volume_profile_value_area_state": "balanced",
			"supertrend_direction":            "down",
			"alphatrend_direction":            "up",
			"candle_bias":                     "bear",
			"internal_structure_event":        "none",
			"internal_structure_bias":         "bear",
			"fvg_position":                    "none",
			"premium_discount_zone":           "discount",
		}),
		testSnapshot(2, map[string]string{
			"ema7":                            "101",
			"ema_spread_pct":                  "0.2",
			"ema25_slope5_pct":                "0.12",
			"ez_ema_fast":                     "101",
			"ez_ema_group_spread_pct":         "0.2",
			"macd_hist":                       "0.1",
			"macd_zero_distance":              "0.1",
			"rsi14":                           "55",
			"volume_ratio20":                  "1.8",
			"cmf20":                           "0.07",
			"volume_profile_poc_distance_pct": "1.2",
			"volume_profile_vah_distance_pct": "0.2",
			"volume_profile_val_distance_pct": "3",
			"supertrend_distance_pct":         "0.5",
			"support_distance_pct":            "1",
			"body_ratio":                      "0.5",
			"close":                           "104",
			"custom_x":                        "4",
		}, map[string]string{
			"ema_alignment":                   "bull",
			"ma_cross":                        "golden_cross",
			"ez_ema_cross":                    "golden",
			"ez_ema_stack":                    "bull",
			"ez_ema_compression":              "normal",
			"ez_price_cross_ema_pair":         "up",
			"supertrend_direction":            "up",
			"alphatrend_direction":            "up",
			"breakout_volume_confirm":         "confirmed",
			"volume_profile_position":         "above_value_area",
			"volume_profile_poc_side":         "above",
			"volume_profile_value_area_state": "upper_breakout",
			"price_volume_confirmation":       "confirmed",
			"structure_bias":                  "bull",
			"structure_event":                 "breakout",
			"internal_structure_event":        "bos_up",
			"internal_structure_bias":         "bull",
			"smart_money":                     "none",
			"fvg_position":                    "above",
			"premium_discount_zone":           "equilibrium",
			"candle_bias":                     "bull",
			"dynamic_swing_vwap_position":     "above",
		}),
		testSnapshot(3, map[string]string{
			"ema7":                            "103",
			"ema_spread_pct":                  "0.4",
			"ema25_slope5_pct":                "0.24",
			"ez_ema_fast":                     "103",
			"ez_ema_group_spread_pct":         "0.4",
			"macd_hist":                       "0.4",
			"macd_zero_distance":              "0.4",
			"rsi14":                           "65",
			"volume_ratio20":                  "2",
			"cmf20":                           "0.12",
			"volume_profile_poc_distance_pct": "1.8",
			"volume_profile_vah_distance_pct": "0.1",
			"volume_profile_val_distance_pct": "4",
			"supertrend_distance_pct":         "0.9",
			"support_distance_pct":            "0.8",
			"resistance_distance_pct":         "3",
			"body_ratio":                      "0.8",
			"close":                           "108",
			"order_block_high":                "105",
			"order_block_low":                 "101",
			"custom_x":                        "9",
		}, map[string]string{
			"ema_alignment":                   "bull",
			"ma_cross":                        "none",
			"ez_ema_cross":                    "none",
			"ez_ema_stack":                    "bull",
			"ez_ema_compression":              "normal",
			"ez_price_cross_ema_pair":         "none",
			"supertrend_direction":            "up",
			"alphatrend_direction":            "up",
			"breakout_volume_confirm":         "confirmed",
			"volume_profile_position":         "above_value_area",
			"volume_profile_poc_side":         "above",
			"volume_profile_value_area_state": "upper_breakout",
			"price_volume_confirmation":       "confirmed",
			"structure_bias":                  "bull",
			"structure_event":                 "breakout",
			"internal_structure_event":        "bos_up",
			"internal_structure_bias":         "bull",
			"smart_money":                     "none",
			"fvg_position":                    "above",
			"premium_discount_zone":           "premium",
			"candle_bias":                     "bull",
			"custom_signal":                   "on",
		}),
	}

	result, err := Analyze(snapshots)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if result.Version != Version {
		t.Fatalf("version = %q, want %q", result.Version, Version)
	}
	if result.Values["window_sample_count"] != "3" {
		t.Fatalf("window_sample_count = %q, want 3", result.Values["window_sample_count"])
	}
	if result.Signals["ema7_win_direction"] != "rising" {
		t.Fatalf("ema7 direction = %q, want rising", result.Signals["ema7_win_direction"])
	}
	if result.Values["ema7_win_rising_count"] != "2" {
		t.Fatalf("ema7 rising count = %q, want 2", result.Values["ema7_win_rising_count"])
	}
	if result.Signals["macd_hist_win_direction"] != "rising" {
		t.Fatalf("macd direction = %q, want rising", result.Signals["macd_hist_win_direction"])
	}
	if result.Signals["ema_alignment_win_latest"] != "bull" {
		t.Fatalf("ema alignment latest = %q, want bull", result.Signals["ema_alignment_win_latest"])
	}
	if result.Values["supertrend_direction_win_stable_count"] != "2" {
		t.Fatalf(
			"supertrend stable count = %q, want 2",
			result.Values["supertrend_direction_win_stable_count"],
		)
	}
	if result.Signals["custom_x_win_direction"] != "rising" {
		t.Fatalf("custom numeric fallback missing: %#v", result.Signals)
	}
	if result.Signals["custom_signal_win_latest"] != "on" {
		t.Fatalf("custom signal fallback missing: %#v", result.Signals)
	}
	if result.Signals["ma_window_cross_event"] != "golden_cross" {
		t.Fatalf("ma cross event = %q, want golden_cross", result.Signals["ma_window_cross_event"])
	}
	if result.Signals["ma_window_phase"] != "early_cross" {
		t.Fatalf("ma phase = %q, want early_cross", result.Signals["ma_window_phase"])
	}
	if result.Signals["ma_window_slope_level"] != "steep_up" {
		t.Fatalf("ma slope level = %q, want steep_up", result.Signals["ma_window_slope_level"])
	}
	if result.Signals["ma_ribbon_state"] != "bullish_fan" {
		t.Fatalf("ma ribbon state = %q, want bullish_fan", result.Signals["ma_ribbon_state"])
	}
	if result.Signals["ma_ribbon_phase"] != "early_expand" {
		t.Fatalf("ma ribbon phase = %q, want early_expand", result.Signals["ma_ribbon_phase"])
	}
	if result.Signals["ma_ribbon_pullback"] != "above" {
		t.Fatalf("ma ribbon pullback = %q, want above", result.Signals["ma_ribbon_pullback"])
	}
	if result.Signals["ez_ema_window_cross_event"] != "golden" {
		t.Fatalf("ez ema cross event = %q, want golden", result.Signals["ez_ema_window_cross_event"])
	}
	if result.Signals["ez_ema_window_phase"] != "early_cross" {
		t.Fatalf("ez ema phase = %q, want early_cross", result.Signals["ez_ema_window_phase"])
	}
	if result.Signals["ez_ema_window_tangled"] != "false" {
		t.Fatalf("ez ema tangled = %q, want false", result.Signals["ez_ema_window_tangled"])
	}
	if result.Signals["ez_ema_ribbon_state"] != "bullish_fan" {
		t.Fatalf("ez ema ribbon state = %q, want bullish_fan", result.Signals["ez_ema_ribbon_state"])
	}
	if result.Signals["ez_ema_ribbon_phase"] != "early_expand" {
		t.Fatalf("ez ema ribbon phase = %q, want early_expand", result.Signals["ez_ema_ribbon_phase"])
	}
	if result.Signals["macd_window_confirmed"] != "true" {
		t.Fatalf("macd confirmed = %q, want true", result.Signals["macd_window_confirmed"])
	}
	if result.Signals["macd_window_zero_side"] != "above" {
		t.Fatalf("macd zero side = %q, want above", result.Signals["macd_window_zero_side"])
	}
	if result.Signals["macd_window_quality"] != "strong" {
		t.Fatalf("macd quality = %q, want strong", result.Signals["macd_window_quality"])
	}
	if result.Signals["trend_window_continuation"] != "true" {
		t.Fatalf("trend continuation = %q, want true", result.Signals["trend_window_continuation"])
	}
	if result.Signals["trend_signal_event"] != "buy" {
		t.Fatalf("trend signal event = %q, want buy", result.Signals["trend_signal_event"])
	}
	if result.Values["trend_signal_age"] != "1" {
		t.Fatalf("trend signal age = %q, want 1", result.Values["trend_signal_age"])
	}
	if result.Signals["trend_price_progress"] != "advancing" {
		t.Fatalf("trend price progress = %q, want advancing", result.Signals["trend_price_progress"])
	}
	if result.Signals["trend_quality"] != "strong" {
		t.Fatalf("trend quality = %q, want strong", result.Signals["trend_quality"])
	}
	if result.Signals["trend_valid"] != "true" {
		t.Fatalf("trend valid = %q, want true", result.Signals["trend_valid"])
	}
	if result.Signals["trend_fake_risk"] != "low" {
		t.Fatalf("trend fake risk = %q, want low", result.Signals["trend_fake_risk"])
	}
	if result.Signals["volume_window_state"] != "expansion" {
		t.Fatalf("volume state = %q, want expansion", result.Signals["volume_window_state"])
	}
	if result.Signals["money_flow_window_bias"] != "bull" {
		t.Fatalf("money flow bias = %q, want bull", result.Signals["money_flow_window_bias"])
	}
	if result.Signals["volume_profile_window_bias"] != "bull" {
		t.Fatalf("volume profile bias = %q, want bull", result.Signals["volume_profile_window_bias"])
	}
	if result.Signals["volume_profile_window_breakout_quality"] != "confirmed" {
		t.Fatalf("volume profile breakout quality = %q, want confirmed", result.Signals["volume_profile_window_breakout_quality"])
	}
	if result.Signals["volume_profile_window_near_poc"] != "false" {
		t.Fatalf("volume profile near poc = %q, want false", result.Signals["volume_profile_window_near_poc"])
	}
	if result.Signals["volume_profile_window_near_value_edge"] != "true" {
		t.Fatalf("volume profile near value edge = %q, want true", result.Signals["volume_profile_window_near_value_edge"])
	}
	if result.Signals["structure_window_bias"] != "bull" {
		t.Fatalf("structure bias = %q, want bull", result.Signals["structure_window_bias"])
	}
	if result.Signals["smc_window_bias"] != "bull" {
		t.Fatalf("smc bias = %q, want bull", result.Signals["smc_window_bias"])
	}
	if result.Signals["smc_window_bos_recent"] != "true" {
		t.Fatalf("smc bos recent = %q, want true", result.Signals["smc_window_bos_recent"])
	}
	if result.Values["smc_window_event_age"] != "0" {
		t.Fatalf("smc event age = %q, want 0", result.Values["smc_window_event_age"])
	}
	if result.Values["smc_window_bos_age"] != "0" {
		t.Fatalf("smc bos age = %q, want 0", result.Values["smc_window_bos_age"])
	}
	if result.Values["smc_window_choch_age"] != "-1" {
		t.Fatalf("smc choch age = %q, want -1", result.Values["smc_window_choch_age"])
	}
	if result.Signals["smc_window_order_block_position"] != "above" {
		t.Fatalf("smc order block position = %q, want above", result.Signals["smc_window_order_block_position"])
	}
	if result.Signals["smc_window_reversal_risk"] != "true" {
		t.Fatalf("smc reversal risk = %q, want true", result.Signals["smc_window_reversal_risk"])
	}
	if result.Signals["candle_window_strength"] != "strong" {
		t.Fatalf("candle strength = %q, want strong", result.Signals["candle_window_strength"])
	}
	if result.Signals["pump_window_signal"] != "true" {
		t.Fatalf("pump signal = %q, want true", result.Signals["pump_window_signal"])
	}
	if result.Signals["pump_window_stage"] != "accelerating" {
		t.Fatalf("pump stage = %q, want accelerating", result.Signals["pump_window_stage"])
	}
	if result.Signals["pump_window_quality"] != "strong" {
		t.Fatalf("pump quality = %q, want strong", result.Signals["pump_window_quality"])
	}
	if result.Signals["pump_window_fake_risk"] != "low" {
		t.Fatalf("pump fake risk = %q, want low", result.Signals["pump_window_fake_risk"])
	}
	if result.Signals["pump_window_reason"] != "volume_trend_macd_breakout" {
		t.Fatalf("pump reason = %q, want volume_trend_macd_breakout", result.Signals["pump_window_reason"])
	}
	if result.Values["pump_window_score"] != "100" {
		t.Fatalf("pump score = %q, want 100", result.Values["pump_window_score"])
	}
	if result.Signals["dump_window_signal"] != "false" {
		t.Fatalf("dump signal = %q, want false", result.Signals["dump_window_signal"])
	}
	if result.Signals["dump_window_stage"] != "none" {
		t.Fatalf("dump stage = %q, want none", result.Signals["dump_window_stage"])
	}
	if result.Signals["dump_window_quality"] != "neutral" {
		t.Fatalf("dump quality = %q, want neutral", result.Signals["dump_window_quality"])
	}
}

func testSnapshot(
	openTime int64,
	values map[string]string,
	signals map[string]string,
) model.IndicatorSnapshot {
	return model.IndicatorSnapshot{
		Exchange:  "binance",
		Market:    "um",
		Symbol:    "ETHUSDT",
		Interval:  "1m",
		OpenTime:  openTime,
		CloseTime: openTime + 59,
		Values:    values,
		Signals:   signals,
		UpdatedAt: openTime + 60,
	}
}
