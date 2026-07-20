package signalresearch

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestExtractMarketStructureFeaturesUsesOnlyClosedHigherTimeframes(t *testing.T) {
	snapshot := strategy.Snapshot{AsOf: 100, Timeframes: map[string]strategy.TimeframeSnapshot{
		"15m": marketStructureTimeframe(90, 1.2, "up"),
		"30m": marketStructureTimeframe(80, 2.4, "down"),
	}}
	features, err := ExtractMarketStructureFeatures(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if features.Version != MarketStructureRegimeFeatureVersion || features.Numeric["15m.atr_pct14"] != 1.2 || features.Numeric["30m.atr_pct14"] != 2.4 {
		t.Fatalf("features=%#v", features)
	}
	if features.Signals["15m.supertrend_direction"] != "up" || features.Signals["30m.supertrend_direction"] != "down" {
		t.Fatalf("signals=%v", features.Signals)
	}
	if _, ok := features.Numeric["15m.market_strength_score"]; ok {
		t.Fatal("aggregate capability score leaked into feature protocol")
	}
}

func TestExtractMarketStructureFeaturesRejectsFutureTimeframe(t *testing.T) {
	snapshot := strategy.Snapshot{AsOf: 100, Timeframes: map[string]strategy.TimeframeSnapshot{
		"15m": marketStructureTimeframe(101, 1.2, "up"),
		"30m": marketStructureTimeframe(80, 2.4, "down"),
	}}
	features, err := ExtractMarketStructureFeatures(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := features.Numeric["15m.atr_pct14"]; ok || len(features.Missing) == 0 {
		t.Fatalf("features=%#v", features)
	}
}

func TestExtractMarketStructureFeaturesRequiresAsOf(t *testing.T) {
	if _, err := ExtractMarketStructureFeatures(strategy.Snapshot{}); err == nil {
		t.Fatal("expected missing as_of error")
	}
}

func marketStructureTimeframe(closeTime int64, atr float64, direction string) strategy.TimeframeSnapshot {
	return strategy.TimeframeSnapshot{
		Indicator: strategy.IndicatorView{
			CloseTime:     closeTime,
			NumericValues: map[string]float64{"atr_pct14": atr},
			Signals:       map[string]string{"supertrend_direction": direction},
		},
		Window: strategy.IndicatorWindowView{CloseTime: closeTime},
	}
}
