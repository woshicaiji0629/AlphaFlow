package supertrend

import (
	"reflect"
	"testing"
	"time"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/signalresearch"
)

func TestNewVersionReplaysBuildsDeterministicMatrix(t *testing.T) {
	versions, err := newVersionReplays(replayConfigForTest(), pullbackConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != len(versionSpecs) {
		t.Fatalf("versions=%d, want %d", len(versions), len(versionSpecs))
	}
	for index, version := range versions {
		if version.spec != versionSpecs[index] {
			t.Fatalf("version[%d]=%+v, want %+v", index, version.spec, versionSpecs[index])
		}
		if version.pullback == nil || version.followthrough == nil {
			t.Fatalf("version %s detectors are incomplete", version.spec.name)
		}
		gotModes := make([]string, 0, len(version.modes))
		for _, mode := range version.modes {
			if mode.replay == nil {
				t.Fatalf("version %s mode %s replay is nil", version.spec.name, mode.name)
			}
			gotModes = append(gotModes, mode.name)
		}
		if !reflect.DeepEqual(gotModes, entryModes) {
			t.Fatalf("version %s modes=%v, want %v", version.spec.name, gotModes, entryModes)
		}
	}
}

func TestVersionReplayLifecycle(t *testing.T) {
	versions, err := newVersionReplays(replayConfigForTest(), pullbackConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	bar := marketmodel.Kline{OpenTime: 1, CloseTime: 2, Open: "100", High: "101", Low: "99", Close: "100"}
	if err := versions[0].advance(bar); err != nil {
		t.Fatal(err)
	}
	versions[0].mode("flip").rawSignals++
	summary := versions[0].finish()
	if summary.Version != "standard" || len(summary.Modes) != len(entryModes) || summary.Modes[0].RawSignals != 1 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestNewVersionReplaysRejectsInvalidConfig(t *testing.T) {
	_, err := newVersionReplays(signalresearch.SinglePositionConfig{}, pullbackConfigForTest())
	if err == nil {
		t.Fatal("expected invalid replay config error")
	}
}

func replayConfigForTest() signalresearch.SinglePositionConfig {
	return signalresearch.SinglePositionConfig{
		InitialEquity: 10000, MarginQuote: 100, Leverage: 100,
		InitialStopBps: 50, BreakEvenTriggerBps: 50, BreakEvenFloorBps: 16,
		TrailingTriggerBps: 75, TrailingDrawdownBps: 30,
		MaxHolding: 12 * time.Hour, CooldownBars: 2, FeeRate: 0.0006, SlippageBps: 2,
	}
}

func pullbackConfigForTest() signalresearch.PullbackConfig {
	return signalresearch.PullbackConfig{
		TouchDistancePct: 0.15, ResumeBars: 3, MaxArmedBars: 10,
		MinVolumeRatio: 1, CooldownBars: 20,
	}
}
