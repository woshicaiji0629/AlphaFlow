package supertrend

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

func (v *versionReplay) onSignalFrame(snapshot strategy.Snapshot, regime *marketregime.Result) error {
	flipSide, hasFlip := signalSide(snapshot.Window, v.spec.flipKey)
	if hasFlip {
		if err := v.onFlip(snapshot, flipSide, regime); err != nil {
			return err
		}
	} else if err := v.onConfirmationFrame(snapshot, regime); err != nil {
		return err
	}
	if v.pendingFlip != nil && !hasFlip {
		activated, err := v.tryDeferredEntry(snapshot, regime)
		if err != nil {
			return err
		}
		if activated {
			v.mode("deferred").rawSignals++
		}
	}
	pullbackSide := v.currentPullbackSide
	if pullbackSide != strategy.SignalSideHold {
		mode := v.mode("pullback")
		mode.rawSignals++
		entered, err := mode.replay.TryEnter(snapshot, pullbackSide, regime)
		if err != nil {
			return err
		}
		if entered && v.entryDiagnostics != nil {
			v.entryDiagnostics = append(v.entryDiagnostics, buildEntryDiagnostic("pullback", snapshot, pullbackSide, regime))
		}
	}
	combinedSide, conflict := combinedEntrySide(flipSide, hasFlip, pullbackSide)
	combined := v.mode("combined")
	if conflict {
		combined.replay.SkipConflict()
	} else if combinedSide != strategy.SignalSideHold {
		combined.rawSignals++
		if _, err := combined.replay.TryEnter(snapshot, combinedSide, regime); err != nil {
			return err
		}
	}
	return nil
}

func (v *versionReplay) onFlip(snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) error {
	v.exhaustPending = nil
	if v.flipDiagnostics != nil {
		v.flipDiagnostics = append(v.flipDiagnostics, buildFlipDiagnostic(snapshot, side, regime))
	}
	flip := v.mode("flip")
	flip.rawSignals++
	entered, err := flip.replay.TryEnter(snapshot, side, regime)
	if err != nil {
		return err
	}
	if entered {
		if v.entryDiagnostics != nil {
			v.entryDiagnostics = append(v.entryDiagnostics, buildEntryDiagnostic("flip", snapshot, side, regime))
		}
		entryKey := fmt.Sprintf("%s:%d", v.spec.name, snapshot.Current.CloseTime)
		if err := v.followthrough.AddSignal(entryKey, snapshot, side, []string{v.spec.flipKey}); err != nil {
			return err
		}
	}
	for _, filter := range []struct {
		mode             string
		minimumRatio     float64
		allowDirectional bool
	}{
		{mode: "flip_volume_loose", minimumRatio: 1.0, allowDirectional: true},
		{mode: "flip_volume_strong", minimumRatio: 1.2},
	} {
		if volumeAllowsFlip(snapshot, side, filter.minimumRatio, filter.allowDirectional) {
			mode := v.mode(filter.mode)
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, side, regime); err != nil {
				return err
			}
		}
	}
	for _, filter := range []struct {
		mode   string
		minADX float64
	}{
		{mode: "exhaust_adx30_di8", minADX: 30},
		{mode: "exhaust_adx35_di8", minADX: 35},
	} {
		if !exhaustionBlocked(snapshot, side, filter.minADX, 8) {
			mode := v.mode(filter.mode)
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, side, regime); err != nil {
				return err
			}
		}
	}
	if regimeAllowsSide(regime, side) {
		if exhaustionBlocked(snapshot, side, 35, 8) {
			pending, err := newConfirmationPending(snapshot, side)
			if err != nil {
				return err
			}
			v.exhaustPending = &pending
		} else {
			mode := v.mode("exhaust_deferred_reacceleration")
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, side, regime); err != nil {
				return err
			}
		}
	}
	if !macroMomentumBlocked(snapshot, side) {
		mode := v.mode("10m_15m_veto")
		mode.rawSignals++
		if _, err := mode.replay.TryEnter(snapshot, side, regime); err != nil {
			return err
		}
	}
	if regimeAllowsSide(regime, side) {
		pending, err := newConfirmationPending(snapshot, side)
		if err != nil {
			return err
		}
		v.waitOnePending = &pending
		retest := pending
		v.retestPending = &retest
	} else {
		v.waitOnePending, v.retestPending = nil, nil
	}
	if regimeAllowsSide(regime, side) {
		v.pendingFlip = nil
		mode := v.mode("deferred")
		mode.rawSignals++
		if _, err := mode.replay.TryEnter(snapshot, side, regime); err != nil {
			return err
		}
	} else if compressionBlocked(regime) {
		pending, err := newPendingFlip(snapshot, side)
		if err != nil {
			return err
		}
		v.pendingFlip = &pending
	} else {
		v.pendingFlip = nil
	}
	return nil
}

func (v *versionReplay) onConfirmationFrame(snapshot strategy.Snapshot, regime *marketregime.Result) error {
	if v.exhaustPending != nil {
		allowed, expired, err := v.exhaustPending.exhaustReaccelerationAllows(snapshot)
		if err != nil {
			return err
		}
		if allowed && regimeAllowsSide(regime, v.exhaustPending.side) {
			mode := v.mode("exhaust_deferred_reacceleration")
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, v.exhaustPending.side, regime); err != nil {
				return err
			}
			v.exhaustPending = nil
		} else if expired {
			v.exhaustPending = nil
		}
	}
	if v.waitOnePending != nil {
		allowed, expired, err := v.waitOnePending.waitOneAllows(snapshot)
		if err != nil {
			return err
		}
		if allowed {
			mode := v.mode("wait_1_bar")
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, v.waitOnePending.side, regime); err != nil {
				return err
			}
		}
		if allowed || expired {
			v.waitOnePending = nil
		}
	}
	if v.retestPending != nil {
		allowed, expired, err := v.retestPending.retestAllows(snapshot)
		if err != nil {
			return err
		}
		if allowed {
			mode := v.mode("retest_3_bars")
			mode.rawSignals++
			if _, err := mode.replay.TryEnter(snapshot, v.retestPending.side, regime); err != nil {
				return err
			}
		}
		if allowed || expired {
			v.retestPending = nil
		}
	}
	return nil
}

func (v *versionReplay) tryDeferredEntry(snapshot strategy.Snapshot, regime *marketregime.Result) (bool, error) {
	pending := v.pendingFlip
	if pending == nil || !regimeAllowsSide(regime, pending.side) {
		return false, nil
	}
	v.pendingFlip = nil
	price, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || price <= 0 {
		return false, fmt.Errorf("parse deferred entry price %q", snapshot.Current.Close)
	}
	continued := pending.side == strategy.SignalSideBuy && price >= pending.signalPrice ||
		pending.side == strategy.SignalSideSell && price <= pending.signalPrice
	if !continued || math.Abs(price-pending.signalPrice) > pending.atr {
		return false, nil
	}
	return v.mode("deferred").replay.TryEnter(snapshot, pending.side, regime)
}

func signalSide(window strategy.IndicatorWindowView, key string) (strategy.SignalSide, bool) {
	series, ok := window.Signal(key)
	if !ok {
		return strategy.SignalSideHold, false
	}
	switch strings.ToLower(strings.TrimSpace(series.Latest)) {
	case "up", "bull", "buy", "long":
		return strategy.SignalSideBuy, true
	case "down", "bear", "sell", "short":
		return strategy.SignalSideSell, true
	default:
		return strategy.SignalSideHold, false
	}
}

func eventSide(events []signalresearch.PlatformEvent) strategy.SignalSide {
	if len(events) != 1 {
		return strategy.SignalSideHold
	}
	return events[0].Side
}

func combinedEntrySide(flipSide strategy.SignalSide, hasFlip bool, pullbackSide strategy.SignalSide) (strategy.SignalSide, bool) {
	if pullbackSide == "" {
		pullbackSide = strategy.SignalSideHold
	}
	if !hasFlip {
		return pullbackSide, false
	}
	if pullbackSide == strategy.SignalSideHold || pullbackSide == flipSide {
		return flipSide, false
	}
	return strategy.SignalSideHold, true
}

func volumeAllowsFlip(snapshot strategy.Snapshot, side strategy.SignalSide, minRatio float64, allowDirectional bool) bool {
	confirmation := strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["price_volume_confirmation"]))
	if confirmation == "" {
		if series, ok := snapshot.Window.Signal("price_volume_confirmation"); ok {
			confirmation = strings.ToLower(strings.TrimSpace(series.Latest))
		}
	}
	if side == strategy.SignalSideBuy && confirmation == "divergence_bear" ||
		side == strategy.SignalSideSell && confirmation == "divergence_bull" {
		return false
	}
	if ratio, ok := snapshot.Indicator.Float("volume_ratio20"); ok && ratio >= minRatio {
		return true
	}
	if !allowDirectional {
		return false
	}
	expected := "confirm_up"
	if side == strategy.SignalSideSell {
		expected = "confirm_down"
	}
	return confirmation == expected
}
