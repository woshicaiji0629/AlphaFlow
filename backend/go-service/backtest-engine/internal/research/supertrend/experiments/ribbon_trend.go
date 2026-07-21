package experiments

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

var ribbonTrendDescriptor = Descriptor{Name: "ribbon_trend", Version: "v5"}

type RibbonTrendSummary struct {
	Signals                int                                  `json:"signals"`
	LongSignals            int                                  `json:"long_signals"`
	ShortSignals           int                                  `json:"short_signals"`
	InitialExpansions      int                                  `json:"initial_expansions"`
	ContinuationSignals    int                                  `json:"continuation_reaccelerations"`
	AcceptedEntries        int                                  `json:"accepted_entries"`
	Replay                 signalresearch.SinglePositionSummary `json:"replay"`
	BySource               map[string]RibbonTradeBreakdown      `json:"by_source"`
	ByMonth                map[string]RibbonTradeBreakdown      `json:"by_month"`
	TrendHoldEntries       int                                  `json:"trend_hold_accepted_entries"`
	TrendHoldReplay        signalresearch.SinglePositionSummary `json:"trend_hold_replay"`
	TrendHoldBySource      map[string]RibbonTradeBreakdown      `json:"trend_hold_by_source"`
	TrendHoldByMonth       map[string]RibbonTradeBreakdown      `json:"trend_hold_by_month"`
	TrendHoldBySourceMonth map[string]RibbonTradeBreakdown      `json:"trend_hold_by_source_month"`
	SetupEntries           int                                  `json:"setup_v3a_accepted_entries"`
	SetupReplay            signalresearch.SinglePositionSummary `json:"setup_v3a_replay"`
	SetupByMonth           map[string]RibbonTradeBreakdown      `json:"setup_v3a_by_month"`
	ProtectedEntries       int                                  `json:"setup_v3b_accepted_entries"`
	ProtectedReplay        signalresearch.SinglePositionSummary `json:"setup_v3b_replay"`
	ProtectedByMonth       map[string]RibbonTradeBreakdown      `json:"setup_v3b_by_month"`
	FlipEntries            int                                  `json:"supertrend_flip_v4a_accepted_entries"`
	FlipReplay             signalresearch.SinglePositionSummary `json:"supertrend_flip_v4a_replay"`
	FlipByMonth            map[string]RibbonTradeBreakdown      `json:"supertrend_flip_v4a_by_month"`
	EventEntries           int                                  `json:"supertrend_event_v4b_accepted_entries"`
	EventReplay            signalresearch.SinglePositionSummary `json:"supertrend_event_v4b_replay"`
	EventByMonth           map[string]RibbonTradeBreakdown      `json:"supertrend_event_v4b_by_month"`
	WindowVariants         []RibbonWindowVariantSummary         `json:"supertrend_window_v5_variants"`
}

type RibbonWindowVariantSummary struct {
	Variant             string                               `json:"variant"`
	WindowBars          int                                  `json:"window_bars"`
	Filter              string                               `json:"filter,omitempty"`
	StartupEntries      int                                  `json:"startup_entries"`
	ContinuationEntries int                                  `json:"continuation_entries"`
	Replay              signalresearch.SinglePositionSummary `json:"replay"`
	BySource            map[string]RibbonTradeBreakdown      `json:"by_source"`
	ByMonth             map[string]RibbonTradeBreakdown      `json:"by_month"`
}

type ribbonWindowReplay struct {
	name                string
	filter              string
	windowBars          int
	replay              *signalresearch.SinglePositionReplay
	armed               [2]bool
	ribbonAge, eventAge [2]int
	eventSources        [2][]string
	startupEntries      int
	continuationEntries int
	entryTypes          []string
}

func newRibbonWindowReplay(name string, windowBars int, filter string, config signalresearch.SinglePositionConfig) (ribbonWindowReplay, error) {
	replay, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return ribbonWindowReplay{}, err
	}
	return ribbonWindowReplay{
		name: name, windowBars: windowBars, filter: filter, replay: replay,
		ribbonAge: [2]int{-1, -1}, eventAge: [2]int{-1, -1},
	}, nil
}

func (r *ribbonWindowReplay) allows(side strategy.SignalSide) bool {
	index := ribbonSideIndex(side)
	sources := r.eventSources[index]
	hasStructure := false
	for _, source := range sources {
		if source == "trend_platform_breakout" || source == "trend_pullback_resume" {
			hasStructure = true
			break
		}
	}
	switch r.filter {
	case "structure_only":
		return hasStructure
	case "structure_or_confluence":
		return hasStructure || len(sources) >= 2
	case "ordered_structure_or_confluence":
		if !hasStructure && len(sources) < 2 {
			return false
		}
		return r.ribbonAge[index] <= r.eventAge[index] || hasStructure
	default:
		return true
	}
}

func (r *ribbonWindowReplay) advanceAges() {
	for index := range r.ribbonAge {
		if r.ribbonAge[index] >= 0 {
			r.ribbonAge[index]++
			if r.ribbonAge[index] > r.windowBars {
				r.ribbonAge[index] = -1
			}
		}
		if r.eventAge[index] >= 0 {
			r.eventAge[index]++
			if r.eventAge[index] > r.windowBars {
				r.eventAge[index] = -1
			}
		}
	}
}

func (r *ribbonWindowReplay) matched(side strategy.SignalSide) bool {
	index := ribbonSideIndex(side)
	return r.armed[index] && r.ribbonAge[index] >= 0 && r.ribbonAge[index] <= r.windowBars &&
		r.eventAge[index] >= 0 && r.eventAge[index] <= r.windowBars
}

func (r *ribbonWindowReplay) clear(side strategy.SignalSide) {
	index := ribbonSideIndex(side)
	r.armed[index], r.ribbonAge[index], r.eventAge[index] = false, -1, -1
	r.eventSources[index] = nil
}

func (r *ribbonWindowReplay) matchLabel(side strategy.SignalSide) string {
	index := ribbonSideIndex(side)
	order := "same_bar"
	if r.ribbonAge[index] > r.eventAge[index] {
		order = "ribbon_then_supertrend"
	} else if r.eventAge[index] > r.ribbonAge[index] {
		order = "supertrend_then_ribbon"
	}
	source := "unknown"
	if len(r.eventSources[index]) > 0 {
		source = strings.Join(r.eventSources[index], "+")
	}
	return order + "/" + source
}

type RibbonTradeBreakdown struct {
	Trades        int     `json:"trades"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
	WinRate       float64 `json:"win_rate"`
	NetPnL        float64 `json:"net_pnl"`
	GrossProfit   float64 `json:"gross_profit"`
	GrossLoss     float64 `json:"gross_loss"`
	ProfitFactor  float64 `json:"profit_factor"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	TradingCost   float64 `json:"trading_cost"`
	AverageMFEBps float64 `json:"average_mfe_bps"`
	AverageMAEBps float64 `json:"average_mae_bps"`
}

type ribbonEMA struct {
	value       float64
	initialized bool
}

func (e *ribbonEMA) update(value float64, period int) float64 {
	if !e.initialized {
		e.value, e.initialized = value, true
		return e.value
	}
	alpha := 2 / float64(period+1)
	e.value += alpha * (value - e.value)
	return e.value
}

type ribbonTrendState struct {
	ema6, ema7, ema19, ema52, ema77, ema168, ema208 ribbonEMA
	dea, typicalEMA2, strengthEMA21                 ribbonEMA
	typicalWindow                                   []float64
	previousDirectional                             [2][3]float64
	expansionBars                                   [2]int
	fastCrossAge                                    [2]int
	exitBars                                        [2]int
	protectionBars                                  [2]int
	pullbackReset                                   [2]bool
	macroAuthorized                                 [2]bool
	previousFastSign                                int
	ready                                           bool
}

type ribbonSignal struct {
	side    strategy.SignalSide
	initial bool
}

func ribbonSideIndex(side strategy.SignalSide) int {
	if side == strategy.SignalSideSell {
		return 1
	}
	return 0
}

func ribbonDirection(side strategy.SignalSide) float64 {
	if side == strategy.SignalSideSell {
		return -1
	}
	return 1
}

func (s *ribbonTrendState) update(closeValue, lowValue float64) []ribbonSignal {
	ema6 := s.ema6.update(closeValue, 6)
	ema7 := s.ema7.update(closeValue, 7)
	ema19 := s.ema19.update(closeValue, 19)
	ema52 := s.ema52.update(closeValue, 52)
	ema77 := s.ema77.update(closeValue, 77)
	ema168 := s.ema168.update(closeValue, 168)
	ema208 := s.ema208.update(closeValue, 208)
	diff := ema7 - ema19
	dea := s.dea.update(diff, 9)
	typical := s.typicalEMA2.update((closeValue+lowValue)/2, 2)
	s.typicalWindow = append(s.typicalWindow, typical)
	if len(s.typicalWindow) > 5 {
		s.typicalWindow = s.typicalWindow[len(s.typicalWindow)-5:]
	}
	if len(s.typicalWindow) < 5 {
		return nil
	}
	startup := 0.0
	for _, value := range s.typicalWindow {
		startup += value
	}
	startup /= 10.158
	strength := s.strengthEMA21.update(startup, 21)

	fastSign := 0
	if ema6 > ema52 {
		fastSign = 1
	} else if ema6 < ema52 {
		fastSign = -1
	}
	if fastSign != s.previousFastSign {
		if fastSign > 0 {
			s.fastCrossAge[0] = 0
		} else if fastSign < 0 {
			s.fastCrossAge[1] = 0
		}
		s.previousFastSign = fastSign
	}
	for index := range s.fastCrossAge {
		s.fastCrossAge[index]++
	}

	signals := make([]ribbonSignal, 0, 1)
	for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
		index := ribbonSideIndex(side)
		s.pullbackReset[index] = false
		direction := ribbonDirection(side)
		fastSpread := direction * (ema6 - ema52)
		diffDirection := direction * diff
		strengthGap := direction * (startup - strength)
		macroAligned := direction*(ema52-ema77) > 0 && direction*(ema168-ema208) > 0
		s.macroAuthorized[index] = macroAligned
		aligned := fastSpread > 0 && macroAligned &&
			diffDirection > 0 && direction*(diff-dea) > 0 && strengthGap > 0
		expanding := s.ready && fastSpread > s.previousDirectional[index][0] &&
			diffDirection > s.previousDirectional[index][1] && strengthGap > s.previousDirectional[index][2]
		if aligned && expanding {
			s.expansionBars[index]++
		} else {
			s.expansionBars[index] = 0
		}
		if s.expansionBars[index] == 2 {
			signals = append(signals, ribbonSignal{
				side: side, initial: s.fastCrossAge[index] <= 3,
			})
		}
		fastContracting := s.ready && fastSpread < s.previousDirectional[index][0]
		oscillatorReset := direction*(diff-dea) <= 0 || strengthGap <= 0
		if macroAligned && fastContracting && oscillatorReset {
			s.pullbackReset[index] = true
		}
		if fastSpread < 0 && direction*(diff-dea) < 0 {
			s.exitBars[index]++
		} else {
			s.exitBars[index] = 0
		}
		contracting := s.ready && fastSpread < s.previousDirectional[index][0] &&
			diffDirection < s.previousDirectional[index][1] && strengthGap < s.previousDirectional[index][2]
		if contracting {
			s.protectionBars[index]++
		} else {
			s.protectionBars[index] = 0
		}
		s.previousDirectional[index] = [3]float64{fastSpread, diffDirection, strengthGap}
	}
	s.ready = true
	return signals
}

func (s *ribbonTrendState) protectionConfirmed(side strategy.SignalSide) bool {
	return s.protectionBars[ribbonSideIndex(side)] >= 2
}

func (s *ribbonTrendState) resetObserved(side strategy.SignalSide) bool {
	return s.pullbackReset[ribbonSideIndex(side)]
}

func (s *ribbonTrendState) trendAuthorized(side strategy.SignalSide) bool {
	return s.macroAuthorized[ribbonSideIndex(side)]
}

func (s *ribbonTrendState) exitConfirmed(side strategy.SignalSide) bool {
	return s.exitBars[ribbonSideIndex(side)] >= 2
}

type RibbonTrendExperiment struct {
	replay           *signalresearch.SinglePositionReplay
	trendHoldReplay  *signalresearch.SinglePositionReplay
	setupReplay      *signalresearch.SinglePositionReplay
	protectedReplay  *signalresearch.SinglePositionReplay
	flipReplay       *signalresearch.SinglePositionReplay
	eventReplay      *signalresearch.SinglePositionReplay
	state            ribbonTrendState
	summary          RibbonTrendSummary
	acceptedSources  []string
	trendHoldSources []string
	setupArmed       [2]bool
	protectedArmed   [2]bool
	flipArmed        [2]bool
	eventArmed       [2]bool
	windowReplays    []ribbonWindowReplay
	costPerTrade     float64
}

func NewRibbonTrendExperiment(config signalresearch.SinglePositionConfig) (*RibbonTrendExperiment, error) {
	replay, err := signalresearch.NewSinglePositionReplay(config)
	if err != nil {
		return nil, fmt.Errorf("build ribbon trend replay: %w", err)
	}
	trendHoldConfig := config
	trendHoldConfig.BreakEvenTriggerBps = 1_000_000_000
	trendHoldConfig.TrailingTriggerBps = 1_000_000_000
	trendHoldReplay, err := signalresearch.NewSinglePositionReplay(trendHoldConfig)
	if err != nil {
		return nil, fmt.Errorf("build ribbon trend hold replay: %w", err)
	}
	setupReplay, err := signalresearch.NewSinglePositionReplay(trendHoldConfig)
	if err != nil {
		return nil, fmt.Errorf("build ribbon setup replay: %w", err)
	}
	protectedReplay, err := signalresearch.NewSinglePositionReplay(trendHoldConfig)
	if err != nil {
		return nil, fmt.Errorf("build ribbon protected replay: %w", err)
	}
	flipReplay, err := signalresearch.NewSinglePositionReplay(trendHoldConfig)
	if err != nil {
		return nil, fmt.Errorf("build ribbon Supertrend flip replay: %w", err)
	}
	eventReplay, err := signalresearch.NewSinglePositionReplay(trendHoldConfig)
	if err != nil {
		return nil, fmt.Errorf("build ribbon Supertrend event replay: %w", err)
	}
	windowReplays := make([]ribbonWindowReplay, 0, 8)
	for _, item := range []struct {
		name, filter string
		bars         int
	}{
		{"window_0", "", 0}, {"window_1", "", 1}, {"window_2", "", 2}, {"window_3", "", 3}, {"window_5", "", 5},
		{"v6a_structure", "structure_only", 1},
		{"v6b_structure_or_confluence", "structure_or_confluence", 1},
		{"v6c_ordered", "ordered_structure_or_confluence", 1},
	} {
		variant, err := newRibbonWindowReplay(item.name, item.bars, item.filter, trendHoldConfig)
		if err != nil {
			return nil, fmt.Errorf("build ribbon %s replay: %w", item.name, err)
		}
		windowReplays = append(windowReplays, variant)
	}
	costBps := config.FeeRate*2*10000 + config.SlippageBps*2
	return &RibbonTrendExperiment{
		replay: replay, trendHoldReplay: trendHoldReplay, setupReplay: setupReplay, protectedReplay: protectedReplay,
		flipReplay: flipReplay, eventReplay: eventReplay,
		windowReplays: windowReplays,
		costPerTrade:  config.MarginQuote * config.Leverage * costBps / 10000,
	}, nil
}

func (e *RibbonTrendExperiment) Descriptor() Descriptor { return ribbonTrendDescriptor }

func (e *RibbonTrendExperiment) OnFrame(_ context.Context, frame Frame) error {
	_, setupWasOpen := e.setupReplay.OpenSide()
	_, protectedWasOpen := e.protectedReplay.OpenSide()
	_, flipWasOpen := e.flipReplay.OpenSide()
	_, eventWasOpen := e.eventReplay.OpenSide()
	windowWasOpen := make([]bool, len(e.windowReplays))
	for index := range e.windowReplays {
		_, windowWasOpen[index] = e.windowReplays[index].replay.OpenSide()
	}
	if err := e.replay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon replay: %w", err)
	}
	if err := e.trendHoldReplay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon trend hold replay: %w", err)
	}
	if err := e.setupReplay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon setup replay: %w", err)
	}
	if err := e.protectedReplay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon protected replay: %w", err)
	}
	if err := e.flipReplay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon Supertrend flip replay: %w", err)
	}
	if err := e.eventReplay.Advance(frame.Snapshot.Current); err != nil {
		return fmt.Errorf("advance ribbon Supertrend event replay: %w", err)
	}
	for index := range e.windowReplays {
		if err := e.windowReplays[index].replay.Advance(frame.Snapshot.Current); err != nil {
			return fmt.Errorf("advance ribbon %s replay: %w", e.windowReplays[index].name, err)
		}
		e.windowReplays[index].advanceAges()
	}
	closeValue, err := strconv.ParseFloat(frame.Snapshot.Current.Close, 64)
	if err != nil {
		return fmt.Errorf("parse ribbon close: %w", err)
	}
	lowValue, err := strconv.ParseFloat(frame.Snapshot.Current.Low, 64)
	if err != nil {
		return fmt.Errorf("parse ribbon low: %w", err)
	}
	signals := e.state.update(closeValue, lowValue)
	ribbonSignals := [2]bool{}
	for _, signal := range signals {
		ribbonSignals[ribbonSideIndex(signal.side)] = true
	}
	for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
		index := ribbonSideIndex(side)
		if !e.state.trendAuthorized(side) {
			e.setupArmed[index] = false
			e.protectedArmed[index] = false
			e.flipArmed[index] = false
			e.eventArmed[index] = false
			for variantIndex := range e.windowReplays {
				e.windowReplays[variantIndex].clear(side)
			}
			continue
		}
		_, setupOpen := e.setupReplay.OpenSide()
		if !setupWasOpen && !setupOpen && e.state.resetObserved(side) {
			e.setupArmed[index] = true
		}
		_, protectedOpen := e.protectedReplay.OpenSide()
		if !protectedWasOpen && !protectedOpen && e.state.resetObserved(side) {
			e.protectedArmed[index] = true
		}
		_, flipOpen := e.flipReplay.OpenSide()
		if !flipWasOpen && !flipOpen && e.state.resetObserved(side) {
			e.flipArmed[index] = true
		}
		_, eventOpen := e.eventReplay.OpenSide()
		if !eventWasOpen && !eventOpen && e.state.resetObserved(side) {
			e.eventArmed[index] = true
		}
		for variantIndex := range e.windowReplays {
			variant := &e.windowReplays[variantIndex]
			_, open := variant.replay.OpenSide()
			if !windowWasOpen[variantIndex] && !open && e.state.resetObserved(side) {
				variant.armed[index] = true
				variant.ribbonAge[index], variant.eventAge[index] = -1, -1
				variant.eventSources[index] = nil
			}
			if variant.armed[index] {
				if ribbonSignals[index] {
					variant.ribbonAge[index] = 0
				}
				if frameHasEntry(frame.Entries, side, false) {
					variant.eventAge[index] = 0
					variant.eventSources[index] = frameEntrySources(frame.Entries, side)
				}
			}
		}
	}
	if side, ok := e.trendHoldReplay.OpenSide(); ok && e.state.exitConfirmed(side) {
		if _, err := e.trendHoldReplay.CloseAtMarket(frame.Snapshot.Current, "ribbon_reversal"); err != nil {
			return fmt.Errorf("exit ribbon trend hold replay: %w", err)
		}
	}
	if side, ok := e.setupReplay.OpenSide(); ok && e.state.exitConfirmed(side) {
		if _, err := e.setupReplay.CloseAtMarket(frame.Snapshot.Current, "ribbon_reversal"); err != nil {
			return fmt.Errorf("exit ribbon setup replay: %w", err)
		}
	}
	if side, ok := e.protectedReplay.OpenSide(); ok {
		grossBps, _, err := e.protectedReplay.OpenReturnBps(frame.Snapshot.Current)
		if err != nil {
			return fmt.Errorf("measure ribbon protected replay: %w", err)
		}
		reason := ""
		if grossBps >= 16 && e.state.protectionConfirmed(side) {
			reason = "ribbon_profit_protection"
		} else if e.state.exitConfirmed(side) {
			reason = "ribbon_reversal"
		}
		if reason != "" {
			if _, err := e.protectedReplay.CloseAtMarket(frame.Snapshot.Current, reason); err != nil {
				return fmt.Errorf("exit ribbon protected replay: %w", err)
			}
		}
	}
	if side, ok := e.flipReplay.OpenSide(); ok && e.state.exitConfirmed(side) {
		if _, err := e.flipReplay.CloseAtMarket(frame.Snapshot.Current, "ribbon_reversal"); err != nil {
			return fmt.Errorf("exit ribbon Supertrend flip replay: %w", err)
		}
	}
	if side, ok := e.eventReplay.OpenSide(); ok && e.state.exitConfirmed(side) {
		if _, err := e.eventReplay.CloseAtMarket(frame.Snapshot.Current, "ribbon_reversal"); err != nil {
			return fmt.Errorf("exit ribbon Supertrend event replay: %w", err)
		}
	}
	for index := range e.windowReplays {
		variant := &e.windowReplays[index]
		if side, ok := variant.replay.OpenSide(); ok && e.state.exitConfirmed(side) {
			if _, err := variant.replay.CloseAtMarket(frame.Snapshot.Current, "ribbon_reversal"); err != nil {
				return fmt.Errorf("exit ribbon %s replay: %w", variant.name, err)
			}
		}
	}
	if !frame.InWindow {
		return nil
	}
	for _, signal := range signals {
		e.summary.Signals++
		if signal.side == strategy.SignalSideBuy {
			e.summary.LongSignals++
		} else {
			e.summary.ShortSignals++
		}
		if signal.initial {
			e.summary.InitialExpansions++
		} else {
			e.summary.ContinuationSignals++
		}
		regime := marketregime.Result{AllowNewPosition: true}
		if signal.side == strategy.SignalSideBuy {
			regime.Direction, regime.AllowLong = marketregime.DirectionLong, true
		} else {
			regime.Direction, regime.AllowShort = marketregime.DirectionShort, true
		}
		accepted, err := e.replay.TryEnter(frame.Snapshot, signal.side, &regime)
		if err != nil {
			return fmt.Errorf("enter ribbon replay: %w", err)
		}
		if accepted {
			e.summary.AcceptedEntries++
			if signal.initial {
				e.acceptedSources = append(e.acceptedSources, "initial_expansion")
			} else {
				e.acceptedSources = append(e.acceptedSources, "continuation_reacceleration")
			}
		}
		trendAccepted, err := e.trendHoldReplay.TryEnter(frame.Snapshot, signal.side, &regime)
		if err != nil {
			return fmt.Errorf("enter ribbon trend hold replay: %w", err)
		}
		if trendAccepted {
			e.summary.TrendHoldEntries++
			if signal.initial {
				e.trendHoldSources = append(e.trendHoldSources, "initial_expansion")
			} else {
				e.trendHoldSources = append(e.trendHoldSources, "continuation_reacceleration")
			}
		}
		index := ribbonSideIndex(signal.side)
		if e.setupArmed[index] {
			setupAccepted, err := e.setupReplay.TryEnter(frame.Snapshot, signal.side, &regime)
			if err != nil {
				return fmt.Errorf("enter ribbon setup replay: %w", err)
			}
			if setupAccepted {
				e.summary.SetupEntries++
				e.setupArmed[index] = false
			}
		}
		if e.protectedArmed[index] {
			protectedAccepted, err := e.protectedReplay.TryEnter(frame.Snapshot, signal.side, &regime)
			if err != nil {
				return fmt.Errorf("enter ribbon protected replay: %w", err)
			}
			if protectedAccepted {
				e.summary.ProtectedEntries++
				e.protectedArmed[index] = false
			}
		}
		if e.flipArmed[index] && frameHasEntry(frame.Entries, signal.side, true) {
			flipAccepted, err := e.flipReplay.TryEnter(frame.Snapshot, signal.side, &regime)
			if err != nil {
				return fmt.Errorf("enter ribbon Supertrend flip replay: %w", err)
			}
			if flipAccepted {
				e.summary.FlipEntries++
				e.flipArmed[index] = false
			}
		}
		if e.eventArmed[index] && frameHasEntry(frame.Entries, signal.side, false) {
			eventAccepted, err := e.eventReplay.TryEnter(frame.Snapshot, signal.side, &regime)
			if err != nil {
				return fmt.Errorf("enter ribbon Supertrend event replay: %w", err)
			}
			if eventAccepted {
				e.summary.EventEntries++
				e.eventArmed[index] = false
			}
		}
	}
	for _, side := range []strategy.SignalSide{strategy.SignalSideBuy, strategy.SignalSideSell} {
		if !e.state.trendAuthorized(side) {
			continue
		}
		regime := marketregime.Result{AllowNewPosition: true}
		if side == strategy.SignalSideBuy {
			regime.Direction, regime.AllowLong = marketregime.DirectionLong, true
		} else {
			regime.Direction, regime.AllowShort = marketregime.DirectionShort, true
		}
		for index := range e.windowReplays {
			variant := &e.windowReplays[index]
			if !variant.matched(side) || !variant.allows(side) {
				continue
			}
			accepted, err := variant.replay.TryEnter(frame.Snapshot, side, &regime)
			if err != nil {
				return fmt.Errorf("enter ribbon %s replay: %w", variant.name, err)
			}
			if !accepted {
				continue
			}
			variant.entryTypes = append(variant.entryTypes, variant.matchLabel(side))
			variant.continuationEntries++
			variant.clear(side)
		}
	}
	return nil
}

func frameHasEntry(entries []EntryCandidate, side strategy.SignalSide, flipOnly bool) bool {
	for _, entry := range entries {
		if entry.Side != side {
			continue
		}
		if !flipOnly {
			return true
		}
		for _, source := range entry.Sources {
			if source == "supertrend_flip" {
				return true
			}
		}
	}
	return false
}

func frameEntrySources(entries []EntryCandidate, side strategy.SignalSide) []string {
	for _, entry := range entries {
		if entry.Side == side {
			return append([]string(nil), entry.Sources...)
		}
	}
	return nil
}

func (e *RibbonTrendExperiment) Finish(context.Context) (Result, error) {
	e.replay.Finish()
	e.trendHoldReplay.Finish()
	e.setupReplay.Finish()
	e.protectedReplay.Finish()
	e.flipReplay.Finish()
	e.eventReplay.Finish()
	e.summary.Replay = e.replay.Summary()
	trades := e.replay.Trades()
	sourceTrades := make(map[string][]signalresearch.SinglePositionTrade)
	monthTrades := make(map[string][]signalresearch.SinglePositionTrade)
	for index, trade := range trades {
		source := "unknown"
		if index < len(e.acceptedSources) {
			source = e.acceptedSources[index]
		}
		sourceTrades[source] = append(sourceTrades[source], trade)
		month := time.UnixMilli(trade.EntryTimeMS).UTC().Format("2006-01")
		monthTrades[month] = append(monthTrades[month], trade)
	}
	e.summary.BySource = summarizeRibbonTradeGroups(sourceTrades, e.costPerTrade)
	e.summary.ByMonth = summarizeRibbonTradeGroups(monthTrades, e.costPerTrade)
	e.summary.TrendHoldReplay = e.trendHoldReplay.Summary()
	trendSourceTrades, trendMonthTrades, trendSourceMonthTrades := groupRibbonTrades(e.trendHoldReplay.Trades(), e.trendHoldSources)
	e.summary.TrendHoldBySource = summarizeRibbonTradeGroups(trendSourceTrades, e.costPerTrade)
	e.summary.TrendHoldByMonth = summarizeRibbonTradeGroups(trendMonthTrades, e.costPerTrade)
	e.summary.TrendHoldBySourceMonth = summarizeRibbonTradeGroups(trendSourceMonthTrades, e.costPerTrade)
	e.summary.SetupReplay = e.setupReplay.Summary()
	_, setupMonths, _ := groupRibbonTrades(e.setupReplay.Trades(), nil)
	e.summary.SetupByMonth = summarizeRibbonTradeGroups(setupMonths, e.costPerTrade)
	e.summary.ProtectedReplay = e.protectedReplay.Summary()
	_, protectedMonths, _ := groupRibbonTrades(e.protectedReplay.Trades(), nil)
	e.summary.ProtectedByMonth = summarizeRibbonTradeGroups(protectedMonths, e.costPerTrade)
	e.summary.FlipReplay = e.flipReplay.Summary()
	_, flipMonths, _ := groupRibbonTrades(e.flipReplay.Trades(), nil)
	e.summary.FlipByMonth = summarizeRibbonTradeGroups(flipMonths, e.costPerTrade)
	e.summary.EventReplay = e.eventReplay.Summary()
	_, eventMonths, _ := groupRibbonTrades(e.eventReplay.Trades(), nil)
	e.summary.EventByMonth = summarizeRibbonTradeGroups(eventMonths, e.costPerTrade)
	e.summary.WindowVariants = make([]RibbonWindowVariantSummary, 0, len(e.windowReplays))
	for index := range e.windowReplays {
		variant := &e.windowReplays[index]
		variant.replay.Finish()
		sources, months, _ := groupRibbonTrades(variant.replay.Trades(), variant.entryTypes)
		e.summary.WindowVariants = append(e.summary.WindowVariants, RibbonWindowVariantSummary{
			Variant: variant.name, WindowBars: variant.windowBars, Filter: variant.filter,
			StartupEntries: variant.startupEntries, ContinuationEntries: variant.continuationEntries,
			Replay: variant.replay.Summary(), BySource: summarizeRibbonTradeGroups(sources, e.costPerTrade),
			ByMonth: summarizeRibbonTradeGroups(months, e.costPerTrade),
		})
	}
	return Result{Descriptor: ribbonTrendDescriptor, Summary: e.summary}, nil
}

func groupRibbonTrades(trades []signalresearch.SinglePositionTrade, sources []string) (map[string][]signalresearch.SinglePositionTrade, map[string][]signalresearch.SinglePositionTrade, map[string][]signalresearch.SinglePositionTrade) {
	sourceTrades := make(map[string][]signalresearch.SinglePositionTrade)
	monthTrades := make(map[string][]signalresearch.SinglePositionTrade)
	sourceMonthTrades := make(map[string][]signalresearch.SinglePositionTrade)
	for index, trade := range trades {
		source := "unknown"
		if index < len(sources) {
			source = sources[index]
		}
		sourceTrades[source] = append(sourceTrades[source], trade)
		month := time.UnixMilli(trade.EntryTimeMS).UTC().Format("2006-01")
		monthTrades[month] = append(monthTrades[month], trade)
		sourceMonthTrades[source+"/"+month] = append(sourceMonthTrades[source+"/"+month], trade)
	}
	return sourceTrades, monthTrades, sourceMonthTrades
}

func summarizeRibbonTradeGroups(groups map[string][]signalresearch.SinglePositionTrade, costPerTrade float64) map[string]RibbonTradeBreakdown {
	result := make(map[string]RibbonTradeBreakdown, len(groups))
	for name, trades := range groups {
		var summary RibbonTradeBreakdown
		var equity, peak float64
		for _, trade := range trades {
			summary.Trades++
			summary.NetPnL += trade.NetPnL
			summary.TradingCost += costPerTrade
			summary.AverageMFEBps += trade.MFEBps
			summary.AverageMAEBps += trade.MAEBps
			if trade.NetPnL > 0 {
				summary.WinningTrades++
				summary.GrossProfit += trade.NetPnL
			} else if trade.NetPnL < 0 {
				summary.LosingTrades++
				summary.GrossLoss -= trade.NetPnL
			}
			equity += trade.NetPnL
			peak = math.Max(peak, equity)
			summary.MaxDrawdown = math.Max(summary.MaxDrawdown, peak-equity)
		}
		if summary.Trades > 0 {
			summary.WinRate = float64(summary.WinningTrades) / float64(summary.Trades)
			summary.AverageMFEBps /= float64(summary.Trades)
			summary.AverageMAEBps /= float64(summary.Trades)
		}
		if summary.GrossLoss > 0 {
			summary.ProfitFactor = summary.GrossProfit / summary.GrossLoss
		}
		result[name] = summary
	}
	return result
}
