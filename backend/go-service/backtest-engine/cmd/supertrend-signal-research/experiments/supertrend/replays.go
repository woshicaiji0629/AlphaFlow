package supertrend

import (
	"fmt"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

type versionSpec struct {
	name         string
	flipKey      string
	valueKey     string
	directionKey string
}

var versionSpecs = []versionSpec{
	{name: "standard", flipKey: "supertrend_flip", valueKey: "supertrend", directionKey: "supertrend_direction"},
	{name: "sma_atr", flipKey: "sma_atr_supertrend_flip", valueKey: "sma_atr_supertrend", directionKey: "sma_atr_supertrend_direction"},
	{name: "adaptive", flipKey: "adaptive_supertrend_flip", valueKey: "adaptive_supertrend", directionKey: "adaptive_supertrend_direction"},
	{name: "ai", flipKey: "ai_supertrend_flip", valueKey: "ai_supertrend", directionKey: "ai_supertrend_direction"},
}

var entryModes = []string{
	"flip",
	"flip_volume_loose",
	"flip_volume_strong",
	"pullback",
	"combined",
	"deferred",
	"exhaust_adx30_di8",
	"exhaust_adx35_di8",
	"10m_15m_veto",
	"wait_1_bar",
	"retest_3_bars",
	"exhaust_deferred_reacceleration",
}

type modeReplay struct {
	name       string
	rawSignals int
	replay     *signalresearch.SinglePositionReplay
}

type versionReplay struct {
	spec                versionSpec
	pullback            *signalresearch.PullbackDetector
	followthrough       *signalresearch.ValidationReplay
	modes               []modeReplay
	currentPullbackSide strategy.SignalSide
	pendingFlip         *pendingFlip
	waitOnePending      *confirmationPending
	retestPending       *confirmationPending
	exhaustPending      *confirmationPending
	flipDiagnostics     []flipDiagnostic
	entryDiagnostics    []entryDiagnostic
}

type ModeSummary struct {
	EntryMode  string                               `json:"entry_mode"`
	RawSignals int                                  `json:"raw_signals"`
	Replay     signalresearch.SinglePositionSummary `json:"replay"`
}

type VersionSummary struct {
	Version string        `json:"version"`
	Modes   []ModeSummary `json:"modes"`
}

func newVersionReplays(base signalresearch.SinglePositionConfig, pullbackBase signalresearch.PullbackConfig) ([]*versionReplay, error) {
	versions := make([]*versionReplay, 0, len(versionSpecs))
	for _, spec := range versionSpecs {
		pullbackConfig := pullbackBase
		pullbackConfig.TrendValueKey = spec.valueKey
		pullbackConfig.TrendDirectionKey = spec.directionKey
		pullback, err := signalresearch.NewPullbackDetector(pullbackConfig)
		if err != nil {
			return nil, fmt.Errorf("build %s Supertrend pullback detector: %w", spec.name, err)
		}
		followthrough, err := signalresearch.NewValidationReplay(signalresearch.ValidationConfig{
			ObservationBars: []int{1, 3, 5, 10},
		})
		if err != nil {
			return nil, fmt.Errorf("build %s Supertrend follow-through replay: %w", spec.name, err)
		}
		version := &versionReplay{
			spec: spec, pullback: pullback, followthrough: followthrough,
			modes: make([]modeReplay, 0, len(entryModes)),
		}
		for _, mode := range entryModes {
			replay, err := signalresearch.NewSinglePositionReplay(base)
			if err != nil {
				return nil, fmt.Errorf("build %s Supertrend %s comparison replay: %w", spec.name, mode, err)
			}
			version.modes = append(version.modes, modeReplay{name: mode, replay: replay})
		}
		versions = append(versions, version)
	}
	return versions, nil
}

func (v *versionReplay) advance(bar marketmodel.Kline) error {
	for index := range v.modes {
		if err := v.modes[index].replay.Advance(bar); err != nil {
			return fmt.Errorf("advance %s %s replay: %w", v.spec.name, v.modes[index].name, err)
		}
	}
	if v.pendingFlip != nil {
		v.pendingFlip.age++
		if v.pendingFlip.age > 10 {
			v.pendingFlip = nil
		}
	}
	for _, pending := range []*confirmationPending{v.waitOnePending, v.retestPending, v.exhaustPending} {
		if pending != nil {
			pending.age++
		}
	}
	return nil
}

func (v *versionReplay) finish() VersionSummary {
	summary := VersionSummary{Version: v.spec.name, Modes: make([]ModeSummary, 0, len(v.modes))}
	for index := range v.modes {
		mode := &v.modes[index]
		mode.replay.Finish()
		summary.Modes = append(summary.Modes, ModeSummary{
			EntryMode: mode.name, RawSignals: mode.rawSignals, Replay: mode.replay.Summary(),
		})
	}
	return summary
}

func (v *versionReplay) mode(name string) *modeReplay {
	for index := range v.modes {
		if v.modes[index].name == name {
			return &v.modes[index]
		}
	}
	return nil
}
