package supertrend

import (
	"testing"

	"alphaflow/go-service/pkg/strategy"
)

func TestSignalSide(t *testing.T) {
	window := strategy.IndicatorWindowView{Signals: map[string]strategy.SignalSeries{
		"buy":  {Latest: "up"},
		"sell": {Latest: "down"},
		"hold": {Latest: "none"},
	}}
	for _, test := range []struct {
		key  string
		want strategy.SignalSide
		ok   bool
	}{
		{key: "buy", want: strategy.SignalSideBuy, ok: true},
		{key: "sell", want: strategy.SignalSideSell, ok: true},
		{key: "hold", want: strategy.SignalSideHold, ok: false},
		{key: "missing", want: strategy.SignalSideHold, ok: false},
	} {
		got, ok := signalSide(window, test.key)
		if got != test.want || ok != test.ok {
			t.Fatalf("key=%s side=%s ok=%t, want side=%s ok=%t", test.key, got, ok, test.want, test.ok)
		}
	}
}

func TestCombinedEntrySide(t *testing.T) {
	for _, test := range []struct {
		name         string
		flipSide     strategy.SignalSide
		hasFlip      bool
		pullbackSide strategy.SignalSide
		want         strategy.SignalSide
		conflict     bool
	}{
		{name: "pullback only", pullbackSide: strategy.SignalSideBuy, want: strategy.SignalSideBuy},
		{name: "flip only", flipSide: strategy.SignalSideSell, hasFlip: true, want: strategy.SignalSideSell},
		{name: "same side", flipSide: strategy.SignalSideBuy, hasFlip: true, pullbackSide: strategy.SignalSideBuy, want: strategy.SignalSideBuy},
		{name: "conflict", flipSide: strategy.SignalSideBuy, hasFlip: true, pullbackSide: strategy.SignalSideSell, want: strategy.SignalSideHold, conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, conflict := combinedEntrySide(test.flipSide, test.hasFlip, test.pullbackSide)
			if got != test.want || conflict != test.conflict {
				t.Fatalf("side=%s conflict=%t, want side=%s conflict=%t", got, conflict, test.want, test.conflict)
			}
		})
	}
}

func TestVolumeAllowsFlip(t *testing.T) {
	for _, test := range []struct {
		name         string
		side         strategy.SignalSide
		ratio        float64
		confirmation string
		minimum      float64
		directional  bool
		want         bool
	}{
		{name: "ratio", side: strategy.SignalSideBuy, ratio: 1, minimum: 1, want: true},
		{name: "directional", side: strategy.SignalSideBuy, ratio: 0.8, confirmation: "confirm_up", minimum: 1, directional: true, want: true},
		{name: "strong rejects directional", side: strategy.SignalSideBuy, ratio: 1.1, confirmation: "confirm_up", minimum: 1.2},
		{name: "bear divergence blocks buy", side: strategy.SignalSideBuy, ratio: 2, confirmation: "divergence_bear", minimum: 1, directional: true},
		{name: "bull divergence blocks sell", side: strategy.SignalSideSell, ratio: 2, confirmation: "divergence_bull", minimum: 1, directional: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := strategy.Snapshot{Indicator: strategy.IndicatorView{
				NumericValues: map[string]float64{"volume_ratio20": test.ratio},
				Signals:       map[string]string{"price_volume_confirmation": test.confirmation},
			}}
			if got := volumeAllowsFlip(snapshot, test.side, test.minimum, test.directional); got != test.want {
				t.Fatalf("volumeAllowsFlip()=%t want=%t", got, test.want)
			}
		})
	}
}
