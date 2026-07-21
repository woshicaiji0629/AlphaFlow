package supertrend

import (
	"testing"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

func TestRegimeDecisionReason(t *testing.T) {
	if got := regimeDecisionReason(strategy.SignalSideBuy, marketregime.Result{AllowLong: true}); got != "permitted" {
		t.Fatalf("permitted reason=%q", got)
	}
	if got := regimeDecisionReason(strategy.SignalSideBuy, marketregime.Result{AllowShort: true}); got != "v4_countertrend_signal" {
		t.Fatalf("countertrend reason=%q", got)
	}
	if got := regimeDecisionReason(strategy.SignalSideBuy, marketregime.Result{Reasons: []string{"v4_compression_locked", "v6_breakout_width_weak"}}); got != "v6_breakout_width_weak" {
		t.Fatalf("blocked reason=%q", got)
	}
}

func TestBuildFlipDiagnosticIncludesVolumeDecision(t *testing.T) {
	snapshot := strategy.Snapshot{
		Current: marketmodel.Kline{CloseTime: 123, Open: "99", High: "101", Low: "98", Close: "100"},
		Indicator: strategy.IndicatorView{
			NumericValues: map[string]float64{"volume_ratio20": 1.1, "atr14": 2, "macd_hist": 0.5},
			Signals:       map[string]string{"price_volume_confirmation": "confirm_up", "structure_event": "bos_up"},
		},
		Timeframes: map[string]strategy.TimeframeSnapshot{"5m": {
			Indicator: strategy.IndicatorView{
				NumericValues: map[string]float64{"adx14": 25, "di_plus14": 30, "di_minus14": 10},
				Signals:       map[string]string{"supertrend_direction": "up", "macd_momentum": "expanding_bull"},
			},
		}},
	}
	diagnostic := buildFlipDiagnostic(snapshot, strategy.SignalSideBuy, &marketregime.Result{AllowLong: true})
	fiveMinute := diagnostic.HigherTimeframes["5m"]
	if !diagnostic.Allowed || !diagnostic.VolumeRatioReady || diagnostic.VolumeRatio20 != 1.1 ||
		!diagnostic.VolumeLooseAllowed || diagnostic.VolumeStrongAllowed || diagnostic.BodyATR != 0.5 ||
		diagnostic.StructureEvent != "bos_up" || diagnostic.Direction5M != "up" || !fiveMinute.Available ||
		fiveMinute.ADX == nil || *fiveMinute.ADX != 25 || fiveMinute.MACDMomentum != "expanding_bull" ||
		diagnostic.HigherTimeframes["30m"].Available {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
}
