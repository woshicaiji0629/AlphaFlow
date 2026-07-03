package indicatorwindow

import (
	"testing"

	model "alphaflow/go-service/pkg/marketmodel"
)

func TestTradingViewWindowAnalysis(t *testing.T) {
	snapshots := []model.IndicatorSnapshot{
		tradingViewWindowSnapshot(1, "0.8", "0.4", "bear", "down", "bear", "down", "none", "normal", "inside"),
		tradingViewWindowSnapshot(2, "1.2", "0.7", "bull", "up", "bull", "up", "none", "normal", "inside"),
		tradingViewWindowSnapshot(3, "1.8", "0.9", "bull", "up", "bull", "up", "sell", "normal", "breakout_up"),
	}

	result, err := Analyze(snapshots)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if result.Signals["channel_window_bias"] != "bull" {
		t.Fatalf("channel bias = %q, want bull", result.Signals["channel_window_bias"])
	}
	if result.Signals["channel_breakout_quality"] != "strong" {
		t.Fatalf("channel quality = %q, want strong", result.Signals["channel_breakout_quality"])
	}
	if result.Signals["channel_volatility_state"] != "expanding" {
		t.Fatalf("channel volatility = %q, want expanding", result.Signals["channel_volatility_state"])
	}
	if result.Signals["channel_position_state"] != "upper" {
		t.Fatalf("channel position = %q, want upper", result.Signals["channel_position_state"])
	}
	if result.Signals["qqe_window_bias"] != "bull" {
		t.Fatalf("qqe bias = %q, want bull", result.Signals["qqe_window_bias"])
	}
	if result.Signals["tradingview_window_bias"] != "bull" {
		t.Fatalf("tv bias = %q, want bull", result.Signals["tradingview_window_bias"])
	}
	if result.Values["tradingview_window_score"] != "5" {
		t.Fatalf("tv score = %q, want 5", result.Values["tradingview_window_score"])
	}
	if result.Signals["exhaustion_risk"] != "medium" {
		t.Fatalf("exhaustion risk = %q, want medium", result.Signals["exhaustion_risk"])
	}
}

func TestTradingViewWindowPanicExhaustionRisk(t *testing.T) {
	snapshots := []model.IndicatorSnapshot{
		tradingViewWindowSnapshot(1, "1", "0.5", "neutral", "up", "bull", "flat", "none", "normal", "inside"),
		tradingViewWindowSnapshot(2, "1", "0.5", "neutral", "up", "bull", "flat", "none", "panic", "inside"),
	}

	result, err := Analyze(snapshots)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if result.Signals["exhaustion_risk"] != "high" {
		t.Fatalf("exhaustion risk = %q, want high", result.Signals["exhaustion_risk"])
	}
}

func tradingViewWindowSnapshot(
	openTime int64,
	channelWidth string,
	channelPosition string,
	qqeTrend string,
	utDirection string,
	sslDirection string,
	rangeDirection string,
	tdExhaustion string,
	wvfState string,
	nwPosition string,
) model.IndicatorSnapshot {
	return model.IndicatorSnapshot{
		OpenTime:  openTime,
		CloseTime: openTime + 59,
		Values: map[string]string{
			"donchian_width_pct20":      channelWidth,
			"donchian_position20":       channelPosition,
			"keltner_width_pct20":       channelWidth,
			"keltner_position20":        channelPosition,
			"qqe_line":                  "55",
			"qqe_signal":                "50",
			"qqe_hist":                  "5",
			"ut_stop":                   "100",
			"ssl_width_pct":             "1",
			"range_filter":              "100",
			"range_filter_distance_pct": "1",
			"td_sell_setup_count":       "9",
			"nw_middle":                 "100",
			"nw_position":               channelPosition,
		},
		Signals: map[string]string{
			"donchian_breakout":      "breakout_up",
			"keltner_breakout":       "breakout_up",
			"qqe_trend":              qqeTrend,
			"qqe_cross":              "none",
			"ut_direction":           utDirection,
			"ut_signal":              "none",
			"ssl_direction":          sslDirection,
			"ssl_cross":              "none",
			"range_filter_direction": rangeDirection,
			"wvf_state":              wvfState,
			"td_exhaustion":          tdExhaustion,
			"nw_trend":               "up",
			"nw_position_state":      nwPosition,
		},
	}
}
