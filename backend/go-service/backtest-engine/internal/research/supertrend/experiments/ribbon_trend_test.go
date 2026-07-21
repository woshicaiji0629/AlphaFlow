package experiments

import (
	"testing"
	"time"

	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func TestFrameHasEntrySeparatesFlipFromOtherEvents(t *testing.T) {
	entries := []EntryCandidate{
		{Side: strategy.SignalSideBuy, Sources: []string{"trend_pullback_resume"}},
		{Side: strategy.SignalSideSell, Sources: []string{"supertrend_flip"}},
	}
	if frameHasEntry(entries, strategy.SignalSideBuy, true) {
		t.Fatal("pullback event must not satisfy the flip-only trigger")
	}
	if !frameHasEntry(entries, strategy.SignalSideBuy, false) {
		t.Fatal("valid pullback entry must satisfy the all-event trigger")
	}
	if !frameHasEntry(entries, strategy.SignalSideSell, true) {
		t.Fatal("expected matching Supertrend flip trigger")
	}
}

func TestRibbonWindowReplayMatchesCausallyAndExpires(t *testing.T) {
	variant := ribbonWindowReplay{
		windowBars: 2, armed: [2]bool{true, false},
		ribbonAge: [2]int{-1, -1}, eventAge: [2]int{0, -1},
	}
	variant.advanceAges()
	variant.ribbonAge[0] = 0
	if !variant.matched(strategy.SignalSideBuy) {
		t.Fatal("expected events one bar apart to match inside a two-bar window")
	}
	variant.ribbonAge[0], variant.eventAge[0] = -1, 0
	variant.advanceAges()
	variant.advanceAges()
	variant.advanceAges()
	if variant.eventAge[0] != -1 || variant.matched(strategy.SignalSideBuy) {
		t.Fatal("expired Supertrend event must not match a later ribbon recovery")
	}
}

func TestRibbonWindowReplayRequiresArmedPullback(t *testing.T) {
	variant := ribbonWindowReplay{
		windowBars: 2, ribbonAge: [2]int{0, -1}, eventAge: [2]int{0, -1},
	}
	if variant.matched(strategy.SignalSideBuy) {
		t.Fatal("matching events without a post-exit pullback must not enter")
	}
	variant.armed[0] = true
	if !variant.matched(strategy.SignalSideBuy) {
		t.Fatal("expected armed pullback with matching events to enter")
	}
}

func TestRibbonWindowReplayLabelsOrderAndSources(t *testing.T) {
	variant := ribbonWindowReplay{
		ribbonAge: [2]int{1, -1}, eventAge: [2]int{0, -1},
		eventSources: [2][]string{{"supertrend_flip", "trend_pullback_resume"}, nil},
	}
	if got, want := variant.matchLabel(strategy.SignalSideBuy), "ribbon_then_supertrend/supertrend_flip+trend_pullback_resume"; got != want {
		t.Fatalf("matchLabel() = %q, want %q", got, want)
	}
	variant.ribbonAge[0], variant.eventAge[0] = 0, 1
	if got, want := variant.matchLabel(strategy.SignalSideBuy), "supertrend_then_ribbon/supertrend_flip+trend_pullback_resume"; got != want {
		t.Fatalf("matchLabel() = %q, want %q", got, want)
	}
}

func TestRibbonWindowReplayV6Filters(t *testing.T) {
	base := ribbonWindowReplay{
		armed: [2]bool{true, false}, ribbonAge: [2]int{0, -1}, eventAge: [2]int{1, -1},
		eventSources: [2][]string{{"supertrend_flip"}, nil},
	}
	base.filter = "structure_only"
	if base.allows(strategy.SignalSideBuy) {
		t.Fatal("V6A must reject standalone flip")
	}
	base.eventSources[0] = []string{"trend_pullback_resume"}
	if !base.allows(strategy.SignalSideBuy) {
		t.Fatal("V6A must accept pullback structure")
	}
	base.filter = "structure_or_confluence"
	base.eventSources[0] = []string{"supertrend_flip", "volatility_impulse"}
	if !base.allows(strategy.SignalSideBuy) {
		t.Fatal("V6B must accept multi-source confluence")
	}
	base.filter = "ordered_structure_or_confluence"
	base.ribbonAge[0], base.eventAge[0] = 1, 0
	if base.allows(strategy.SignalSideBuy) {
		t.Fatal("V6C must reject ribbon-first confluence without structure")
	}
	base.eventSources[0] = []string{"trend_platform_breakout"}
	if !base.allows(strategy.SignalSideBuy) {
		t.Fatal("V6C must retain ribbon-first structure")
	}
}

func TestRibbonTrendStateProducesCausalBullAndBearExpansions(t *testing.T) {
	var state ribbonTrendState
	var longSignals, shortSignals int
	price := 100.0
	for index := 0; index < 260; index++ {
		price += 0.2
		for _, signal := range state.update(price, price-0.1) {
			if signal.side == "buy" {
				longSignals++
			}
		}
	}
	for index := 0; index < 320; index++ {
		price -= 0.35
		for _, signal := range state.update(price, price-0.1) {
			if signal.side == "sell" {
				shortSignals++
			}
		}
	}
	if longSignals == 0 || shortSignals == 0 {
		t.Fatalf("expected symmetric ribbon signals, long=%d short=%d", longSignals, shortSignals)
	}
}

func TestRibbonTrendStateConfirmsExitAfterTwoOpposingBars(t *testing.T) {
	var state ribbonTrendState
	price := 100.0
	for index := 0; index < 260; index++ {
		price += 0.2
		state.update(price, price-0.1)
	}
	if state.exitConfirmed("buy") {
		t.Fatal("bull trend unexpectedly requested an exit")
	}
	confirmed := false
	for index := 0; index < 120; index++ {
		price -= 1
		state.update(price, price-0.1)
		if state.exitConfirmed("buy") {
			confirmed = true
			break
		}
	}
	if !confirmed {
		t.Fatal("expected opposing fast ribbon and DIF to confirm a long exit")
	}
}

func TestRibbonTrendStateRearmsAfterPullbackInsideSameMacroTrend(t *testing.T) {
	var state ribbonTrendState
	price := 100.0
	for index := 0; index < 320; index++ {
		price += 0.2
		state.update(price, price-0.1)
	}
	recovered := false
	for index := 0; index < 16; index++ {
		price -= 0.35
		state.update(price, price-0.1)
		if state.resetObserved("buy") {
			recovered = true
		}
	}
	if !recovered {
		t.Fatal("expected pullback reset inside the existing long macro trend")
	}
	recovered = false
	for index := 0; index < 40; index++ {
		price += 0.45
		for _, signal := range state.update(price, price-0.1) {
			if signal.side == "buy" {
				recovered = true
			}
		}
	}
	if !recovered {
		t.Fatal("expected a new long setup after pullback recovery within the same macro trend")
	}
}

func TestRibbonTrendStateProfitProtectionNeedsTwoContractionBars(t *testing.T) {
	var state ribbonTrendState
	price := 100.0
	for index := 0; index < 320; index++ {
		price += 0.2
		state.update(price, price-0.1)
	}
	sawFirstContraction := false
	for index := 0; index < 30; index++ {
		price -= 0.15
		state.update(price, price-0.1)
		bars := state.protectionBars[ribbonSideIndex("buy")]
		if bars == 1 {
			sawFirstContraction = true
			if state.protectionConfirmed("buy") {
				t.Fatal("one contraction bar must not confirm profit protection")
			}
		}
		if state.protectionConfirmed("buy") {
			if !sawFirstContraction {
				t.Fatal("profit protection skipped its first confirmation bar")
			}
			return
		}
	}
	t.Fatal("expected two contraction bars to confirm profit protection")
}

func TestNewRibbonTrendExperimentRejectsInvalidReplayConfig(t *testing.T) {
	_, err := NewRibbonTrendExperiment(signalresearch.SinglePositionConfig{MaxHolding: time.Hour})
	if err == nil {
		t.Fatal("expected invalid replay config error")
	}
}

func TestSummarizeRibbonTradeGroupsCalculatesAttribution(t *testing.T) {
	groups := map[string][]signalresearch.SinglePositionTrade{"initial": {
		{NetPnL: 20, MFEBps: 40, MAEBps: 10},
		{NetPnL: -10, MFEBps: 5, MAEBps: 20},
	}}
	summary := summarizeRibbonTradeGroups(groups, 16)["initial"]
	if summary.Trades != 2 || summary.WinningTrades != 1 || summary.LosingTrades != 1 || summary.NetPnL != 10 {
		t.Fatalf("unexpected attribution: %+v", summary)
	}
	if summary.ProfitFactor != 2 || summary.TradingCost != 32 || summary.MaxDrawdown != 10 {
		t.Fatalf("unexpected risk attribution: %+v", summary)
	}
}
