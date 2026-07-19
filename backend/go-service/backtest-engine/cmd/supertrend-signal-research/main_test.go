package main

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func singlePositionConfigForTest() signalresearch.SinglePositionConfig {
	return signalresearch.SinglePositionConfig{
		InitialEquity: 10000, MarginQuote: 100, Leverage: 100,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: 12 * time.Hour, CooldownBars: 2, FeeRate: 0.0006, SlippageBps: 2,
	}
}

func TestSupertrendFlipSide(t *testing.T) {
	window := strategy.IndicatorWindowView{Signals: map[string]strategy.SignalSeries{
		"supertrend_flip":          {Latest: "up"},
		"adaptive_supertrend_flip": {Latest: "down"},
		"ai_supertrend_flip":       {Latest: "none"},
	}}
	tests := []struct {
		key  string
		want strategy.SignalSide
		ok   bool
	}{
		{key: "supertrend_flip", want: strategy.SignalSideBuy, ok: true},
		{key: "adaptive_supertrend_flip", want: strategy.SignalSideSell, ok: true},
		{key: "ai_supertrend_flip", want: strategy.SignalSideHold, ok: false},
		{key: "missing", want: strategy.SignalSideHold, ok: false},
	}
	for _, test := range tests {
		got, ok := supertrendFlipSide(window, test.key)
		if got != test.want || ok != test.ok {
			t.Fatalf("key=%s side=%s ok=%t, want side=%s ok=%t", test.key, got, ok, test.want, test.ok)
		}
	}
}

func TestBuildSupertrendComparisonReplays(t *testing.T) {
	items, err := buildSupertrendComparisonReplays(singlePositionConfigForTest(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].name != "standard" || items[1].name != "adaptive" || items[2].name != "ai" {
		t.Fatalf("items=%#v", items)
	}
}
