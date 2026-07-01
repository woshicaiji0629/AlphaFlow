from collections.abc import Mapping
from dataclasses import dataclass, replace

from alphaflow.strategy.models import (
    ExitReasonType,
    ExitRule,
    IndicatorWindowAnalysis,
    MarketAnalysis,
    MarketSnapshot,
    PositionSide,
    PositionState,
    Signal,
    SignalSide,
    StrategyContext,
    StrategyResult,
    StrategyTarget,
    TimeframeWindow,
)
from alphaflow.strategy.position import PositionManager


@dataclass(frozen=True)
class SupertrendStrategyConfig:
    entry_interval: str = "3m"
    confirmation_intervals: tuple[str, ...] = ("5m", "10m", "15m", "30m")
    entry_threshold: float = 0.72
    max_blocking_timeframes: int = 1


@dataclass(frozen=True)
class EntryDecision:
    side: SignalSide
    score: float
    blocked: bool
    reasons: tuple[str, ...]


class SupertrendStrategy:
    name = "supertrend"

    def __init__(
        self,
        config: SupertrendStrategyConfig | None = None,
        position_manager: PositionManager | None = None,
    ) -> None:
        self._config = config or SupertrendStrategyConfig()
        self._position_manager = position_manager or PositionManager()

    def required_intervals(self, target: StrategyTarget) -> tuple[str, ...]:
        intervals = (self._config.entry_interval, *self._config.confirmation_intervals)
        return tuple(dict.fromkeys(intervals))

    def evaluate(
        self,
        context: StrategyContext,
    ) -> StrategyResult:
        snapshot = self.snapshot_for_context(context)
        strategy_result = self.evaluate_snapshot(snapshot, context.position)
        plan = self._position_manager.plan(
            self.name,
            strategy_result.signal,
            context.position,
            current_price=current_price(snapshot),
            exit_rules=strategy_result.exit_rules,
        )
        return replace(strategy_result, position_plan=plan)

    def snapshot_for_context(self, context: StrategyContext) -> MarketSnapshot:
        snapshot = context.snapshots[self._config.entry_interval]
        windows = {
            interval: TimeframeWindow(
                interval=interval,
                health=related.health,
                indicator_window=related.indicator_window,
                window=related.window,
            )
            for interval, related in context.snapshots.items()
            if interval in self._config.confirmation_intervals
        }
        return replace(snapshot, timeframe_windows=windows)

    def evaluate_snapshot(
        self,
        snapshot: MarketSnapshot,
        position: PositionState | None,
    ) -> StrategyResult:
        if snapshot.indicator.interval != self._config.entry_interval:
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"waiting for {self._config.entry_interval} entry interval"),
            )
        if not snapshot.health.is_ok():
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"health not ok: {snapshot.health.reason or 'unknown'}"),
            )
        if snapshot.indicator_window is None:
            return result(snapshot, self.name, hold(snapshot, "indicator window missing"))

        if snapshot.freshness is not None and not snapshot.freshness.valid:
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"feature freshness not ok: {snapshot.freshness.reason}"),
            )

        quality = latest_signal(snapshot.indicator_window, "data_quality")
        if quality not in {"", "ok"}:
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"indicator quality not ok: {quality}"),
            )

        long_decision = entry_decision(snapshot, SignalSide.BUY, self._config)
        short_decision = entry_decision(snapshot, SignalSide.SELL, self._config)

        exit_result = exit_signal(
            snapshot,
            position,
            long_decision,
            short_decision,
            self._config,
        )
        if exit_result is not None:
            return result(snapshot, self.name, exit_result)

        if (
            not long_decision.blocked
            and long_decision.score >= self._config.entry_threshold
            and long_decision.score >= short_decision.score
        ):
            return result(
                snapshot,
                self.name,
                signal(
                    snapshot,
                    SignalSide.BUY,
                    long_decision.score,
                    "; ".join(long_decision.reasons),
                ),
            )
        if not short_decision.blocked and short_decision.score >= self._config.entry_threshold:
            return result(
                snapshot,
                self.name,
                signal(
                    snapshot,
                    SignalSide.SELL,
                    short_decision.score,
                    "; ".join(short_decision.reasons),
                ),
            )

        leading_decision = (
            long_decision if long_decision.score >= short_decision.score else short_decision
        )
        reasons = list(leading_decision.reasons)
        score = max(long_decision.score, short_decision.score)
        if long_decision.blocked and short_decision.blocked:
            reasons = ["both sides blocked by weak or conflicting windows"]
            reasons.extend(leading_decision.reasons)
        return result(snapshot, self.name, hold(snapshot, "; ".join(reasons), score=score))


def exit_signal(
    snapshot: MarketSnapshot,
    position: PositionState | None,
    long_decision: EntryDecision,
    short_decision: EntryDecision,
    config: SupertrendStrategyConfig,
) -> Signal | None:
    if position is None or position.is_flat():
        return None
    if position.side == PositionSide.LONG:
        should_exit, reason = should_exit_position(snapshot, SignalSide.SELL)
        if should_exit:
            return signal(
                snapshot,
                SignalSide.SELL,
                max(short_decision.score, config.entry_threshold),
                reason,
            )
        return hold(
            snapshot,
            "long exit deferred: waiting for confirmed bearish window; "
            + "; ".join(short_decision.reasons),
            score=short_decision.score,
        )
    if position.side == PositionSide.SHORT:
        should_exit, reason = should_exit_position(snapshot, SignalSide.BUY)
        if should_exit:
            return signal(
                snapshot,
                SignalSide.BUY,
                max(long_decision.score, config.entry_threshold),
                reason,
            )
        return hold(
            snapshot,
            "short exit deferred: waiting for confirmed bullish window; "
            + "; ".join(long_decision.reasons),
            score=long_decision.score,
        )
    return None


def entry_decision(
    snapshot: MarketSnapshot,
    side: SignalSide,
    config: SupertrendStrategyConfig,
) -> EntryDecision:
    window = snapshot.indicator_window
    if window is None:
        return EntryDecision(side=side, score=0.0, blocked=True, reasons=("indicator window missing",))

    bullish = side == SignalSide.BUY
    direction = side_direction(side)
    action_name = "pump" if bullish else "dump"
    signal_key = f"{action_name}_window_signal"
    fake_key = f"{action_name}_window_fake_risk"
    quality_key = f"{action_name}_window_quality"
    score_key = f"{action_name}_window_score"

    score = 0.0
    blocked = False
    reasons: list[str] = []

    if truthy_signal(window, signal_key) or latest_signal(window, "supertrend_direction") == direction:
        score += 0.30
        reasons.append(f"3m {action_name} trigger")
    else:
        blocked = True
        reasons.append(f"3m {action_name} signal missing")

    fake_risk = latest_signal(window, fake_key)
    if fake_risk not in {"", "low"}:
        blocked = True
        reasons.append(f"{action_name} fake risk: {fake_risk}")

    event_score = latest_value(window, score_key)
    if event_score > 0:
        score += min(event_score / 100.0, 1.0) * 0.10

    quality = latest_signal(window, quality_key)
    if quality in {"strong", "high"}:
        score += 0.08
        reasons.append(f"{action_name} quality strong")
    elif quality in {"normal", "medium", "weak"}:
        score += 0.04

    trend_ok, trend_score, trend_reasons = trend_confirmation(window, side)
    score += trend_score
    reasons.extend(trend_reasons)
    blocked = blocked or not trend_ok

    ma_ok, ma_score, ma_reasons = ma_confirmation(window, side)
    score += ma_score
    reasons.extend(ma_reasons)
    blocked = blocked or not ma_ok

    macd_ok, macd_score, macd_reasons = macd_confirmation(window, side)
    score += macd_score
    reasons.extend(macd_reasons)
    blocked = blocked or not macd_ok

    volume_ok, volume_score, volume_reasons = volume_confirmation(window, side)
    score += volume_score
    reasons.extend(volume_reasons)
    blocked = blocked or not volume_ok

    mtf_ok, mtf_score, mtf_reasons = multi_timeframe_confirmation(
        snapshot.timeframe_windows,
        side,
        config,
    )
    score += mtf_score
    reasons.extend(mtf_reasons)
    blocked = blocked or not mtf_ok

    return EntryDecision(
        side=side,
        score=min(round(score, 4), 1.0),
        blocked=blocked,
        reasons=tuple(reasons),
    )


def trend_confirmation(window: IndicatorWindowAnalysis, side: SignalSide) -> tuple[bool, float, list[str]]:
    direction = side_direction(side)
    expected_progress = "advancing" if side == SignalSide.BUY else "declining"
    reasons: list[str] = []
    score = 0.0

    trend_valid = latest_signal(window, "trend_valid")
    if trend_valid not in {"", "true"}:
        return False, score, [f"trend invalid: {trend_valid}"]

    trend_bias = latest_signal(window, "trend_window_bias")
    supertrend = latest_signal(window, "supertrend_direction")
    alphatrend = latest_signal(window, "alphatrend_direction")
    if trend_bias in {direction, direction_name(side)} or supertrend == direction or alphatrend == direction:
        score += 0.12
        reasons.append("trend direction aligned")
    elif trend_bias:
        return False, score, [f"trend direction blocks: {trend_bias}"]

    progress = latest_signal(window, "trend_price_progress")
    if progress in {"", expected_progress}:
        score += 0.06
    else:
        return False, score, [f"trend progress blocks: {progress}"]

    trend_quality = latest_signal(window, "trend_quality")
    if trend_quality in {"strong", "high"}:
        score += 0.04
    elif trend_quality in {"flat", "weak"}:
        return False, score, [f"trend quality weak: {trend_quality}"]

    return True, score, reasons


def ma_confirmation(window: IndicatorWindowAnalysis, side: SignalSide) -> tuple[bool, float, list[str]]:
    expected = "bull" if side == SignalSide.BUY else "bear"
    reasons: list[str] = []
    score = 0.0

    state = latest_signal(window, "ma_ribbon_state")
    phase = latest_signal(window, "ma_ribbon_phase")
    alignment = latest_signal(window, "ema_alignment")

    if state in {"tangled", "flat", "range"} or phase in {"tangled", "range", "flat"}:
        return False, score, [f"ma ribbon has no direction: {state or phase}"]

    expected_states = {f"{expected}ish_fan", f"{expected}_fan", expected, "expanding"}
    if state in expected_states or alignment == expected:
        score += 0.14
        reasons.append("ma ribbon direction aligned")
    elif state:
        return False, score, [f"ma ribbon blocks: {state}"]

    if phase in {"early_expand", "trend", "spreading", "expanding"}:
        score += 0.06

    return True, score, reasons


def macd_confirmation(window: IndicatorWindowAnalysis, side: SignalSide) -> tuple[bool, float, list[str]]:
    expected = "bull" if side == SignalSide.BUY else "bear"
    opposite = "bear" if side == SignalSide.BUY else "bull"
    reasons: list[str] = []
    score = 0.0

    bias = latest_signal(window, "macd_window_bias")
    momentum = latest_signal(window, "macd_momentum")
    if bias == expected or momentum.endswith(expected):
        score += 0.12
        reasons.append("macd follows direction")
    elif bias == opposite or momentum.endswith(opposite):
        return False, score, [f"macd blocks: {bias or momentum}"]

    quality = latest_signal(window, "macd_window_quality")
    if quality in {"strong", "high"}:
        score += 0.05
    elif quality in {"weak", "normal", "medium", ""}:
        score += 0.02

    divergence = latest_signal(window, "macd_divergence")
    if divergence in {f"divergence_{opposite}", opposite}:
        return False, score, [f"macd divergence blocks: {divergence}"]

    return True, score, reasons


def volume_confirmation(window: IndicatorWindowAnalysis, side: SignalSide) -> tuple[bool, float, list[str]]:
    expected = "confirm_up" if side == SignalSide.BUY else "confirm_down"
    opposite = "divergence_bear" if side == SignalSide.BUY else "divergence_bull"
    price_volume = latest_signal(window, "price_volume_confirmation")
    if price_volume == opposite:
        return False, 0.0, [f"price-volume blocks: {price_volume}"]

    score = 0.0
    reasons: list[str] = []
    if price_volume in {expected, "confirmed", "support"}:
        score += 0.05
        reasons.append("price-volume confirmed")

    volume_state = latest_signal(window, "volume_window_state") or latest_signal(window, "volume_state")
    if volume_state in {"spike", "expanding", "breakout", "above_average"}:
        score += 0.03

    return True, score, reasons


def multi_timeframe_confirmation(
    windows: Mapping[str, TimeframeWindow],
    side: SignalSide,
    config: SupertrendStrategyConfig,
) -> tuple[bool, float, list[str]]:
    if not windows:
        return True, 0.0, ["confirmation windows missing"]

    aligned: list[str] = []
    blocking: list[str] = []
    for interval in config.confirmation_intervals:
        window = windows.get(interval)
        if window is None:
            continue
        state = classify_timeframe(window, side)
        if state == "aligned":
            aligned.append(interval)
        elif state == "blocking":
            blocking.append(interval)

    reasons: list[str] = []
    if aligned:
        reasons.append(f"mtf aligned: {','.join(aligned)}")
    if blocking:
        reasons.append(f"mtf blocking: {','.join(blocking)}")
    if "5m" in blocking and "10m" in blocking:
        return False, min(len(aligned) * 0.04, 0.15), reasons + ["5m and 10m both blocking"]
    if len(blocking) > config.max_blocking_timeframes:
        return False, min(len(aligned) * 0.04, 0.15), reasons
    return True, min(len(aligned) * 0.04, 0.15), reasons


def should_exit_position(snapshot: MarketSnapshot, exit_side: SignalSide) -> tuple[bool, str]:
    indicator_window = snapshot.indicator_window
    if indicator_window is None:
        return False, "indicator window missing"

    expected = "up" if exit_side == SignalSide.BUY else "down"
    action_name = "pump" if exit_side == SignalSide.BUY else "dump"
    if not (
        truthy_signal(indicator_window, f"{action_name}_window_signal")
        or latest_signal(indicator_window, "supertrend_direction") == expected
    ):
        return False, f"3m supertrend has not turned {expected}"

    if entry_window_confirms_exit(indicator_window, exit_side):
        return True, f"confirmed {expected} exit: trend, ma and macd aligned"
    if short_timeframes_block_exit(snapshot.timeframe_windows, exit_side):
        return True, f"confirmed {expected} exit: 5m and 10m blocking"
    supertrend = indicator_window.signals.get("supertrend_direction")
    if supertrend is not None and supertrend.stable_count > 1:
        return (
            True,
            f"confirmed {expected} exit: supertrend stable for {supertrend.stable_count} bars",
        )
    return False, f"3m supertrend turned {expected} but exit confirmation is weak"


def short_timeframes_block_exit(
    windows: Mapping[str, TimeframeWindow],
    side: SignalSide,
) -> bool:
    return (
        classify_timeframe(windows["5m"], side) == "aligned"
        and classify_timeframe(windows["10m"], side) == "aligned"
        if "5m" in windows and "10m" in windows
        else False
    )


def entry_window_confirms_exit(window: IndicatorWindowAnalysis, side: SignalSide) -> bool:
    trend_ok, _, _ = trend_confirmation(window, side)
    ma_ok, _, _ = ma_confirmation(window, side)
    macd_ok, _, _ = macd_confirmation(window, side)
    return trend_ok and ma_ok and macd_ok


def classify_timeframe(window: TimeframeWindow, side: SignalSide) -> str:
    indicator_window = window.indicator_window
    if indicator_window is None or not window.health.is_ok():
        return "missing"

    expected = "bull" if side == SignalSide.BUY else "bear"
    opposite = "bear" if side == SignalSide.BUY else "bull"
    expected_direction = side_direction(side)
    opposite_action = "dump" if side == SignalSide.BUY else "pump"

    aligned_votes = 0
    blocking_votes = 0
    for key in ("trend_window_bias", "ma_window_bias", "macd_window_bias"):
        value = latest_signal(indicator_window, key)
        if value in {expected, expected_direction, direction_name(side)}:
            aligned_votes += 1
        elif value in {opposite, opposite_direction(side), direction_name(opposite_side(side))}:
            blocking_votes += 1

    if latest_signal(indicator_window, "supertrend_direction") == expected_direction:
        aligned_votes += 1
    elif latest_signal(indicator_window, "supertrend_direction") == opposite_direction(side):
        blocking_votes += 1

    if truthy_signal(indicator_window, f"{opposite_action}_window_signal"):
        blocking_votes += 2

    if blocking_votes >= 2:
        return "blocking"
    if aligned_votes >= 2:
        return "aligned"
    return "neutral"


def signal(snapshot: MarketSnapshot, side: SignalSide, score: float, reason: str) -> Signal:
    indicator = snapshot.indicator
    return Signal(
        exchange=indicator.exchange,
        market=indicator.market,
        symbol=indicator.symbol,
        interval=indicator.interval,
        side=side,
        score=score,
        confidence=abs(score),
        reason=reason,
        open_time=indicator.open_time,
        updated_at=indicator.updated_at,
    )


def hold(snapshot: MarketSnapshot, reason: str, score: float = 0.0) -> Signal:
    return signal(snapshot, SignalSide.HOLD, score, reason)


def result(snapshot: MarketSnapshot, strategy_name: str, sig: Signal) -> StrategyResult:
    return StrategyResult(
        strategy_name=strategy_name,
        signal=sig,
        analysis=analyze_market(snapshot, sig.reason),
        exit_rules=exit_rules(snapshot, sig.side),
    )


def analyze_market(snapshot: MarketSnapshot, summary: str) -> MarketAnalysis:
    window = snapshot.window
    price = current_price(snapshot)
    price_text = f"current price {price}" if price else "current price unavailable"
    indicator_window = snapshot.indicator_window
    return MarketAnalysis(
        summary=f"{summary}; {price_text}",
        trend=latest_signal(indicator_window, "trend_window_bias")
        or (window.trend if window is not None else ""),
        momentum=latest_signal(indicator_window, "macd_window_bias")
        or latest_signal(indicator_window, "macd_momentum"),
        volatility=latest_signal(indicator_window, "volatility_state"),
        volume=window.volume_state if window is not None else "",
        risk=latest_signal(indicator_window, "data_quality") or "unknown",
        key_levels={
            key: value
            for key, value in snapshot.indicator.values.items()
            if key
            in {
                "support_1",
                "support_2",
                "resistance_1",
                "resistance_2",
                "swing_high",
                "swing_low",
            }
        },
    )


def current_price(snapshot: MarketSnapshot) -> str:
    if snapshot.last_price is not None and snapshot.last_price.price:
        return snapshot.last_price.price
    if snapshot.mark_price is not None and snapshot.mark_price.mark_price:
        return snapshot.mark_price.mark_price
    if snapshot.klines:
        return snapshot.klines[-1].close
    return ""


def exit_rules(snapshot: MarketSnapshot, side: SignalSide) -> tuple[ExitRule, ...]:
    if side == SignalSide.HOLD:
        return ()
    values = snapshot.indicator.values
    supertrend_stop = values.get("supertrend", "")
    if side == SignalSide.BUY:
        return price_exit_rules(
            take_profit_price=values.get("resistance_1", ""),
            stop_loss_price=supertrend_stop or values.get("support_1", ""),
        )
    return price_exit_rules(
        take_profit_price=values.get("support_1", ""),
        stop_loss_price=supertrend_stop or values.get("resistance_1", ""),
    )


def price_exit_rules(take_profit_price: str, stop_loss_price: str) -> tuple[ExitRule, ...]:
    rules: list[ExitRule] = []
    if take_profit_price:
        rules.append(
            ExitRule(
                rule_type=ExitReasonType.TAKE_PROFIT,
                trigger_price=take_profit_price,
                reason="take profit target",
            )
        )
    if stop_loss_price:
        rules.append(
            ExitRule(
                rule_type=ExitReasonType.STOP_LOSS,
                trigger_price=stop_loss_price,
                reason="supertrend stop guard",
            )
        )
    return tuple(rules)


def latest_signal(window: IndicatorWindowAnalysis | None, key: str) -> str:
    if window is None:
        return ""
    signal_value = window.signals.get(key)
    if signal_value is None or signal_value.latest is None:
        return ""
    return str(signal_value.latest)


def latest_value(window: IndicatorWindowAnalysis | None, key: str) -> float:
    if window is None:
        return 0.0
    series = window.values.get(key)
    if series is None or series.latest is None:
        return 0.0
    try:
        return float(series.latest)
    except (TypeError, ValueError):
        return 0.0


def truthy_signal(window: IndicatorWindowAnalysis, key: str) -> bool:
    return latest_signal(window, key).lower() in {"true", "yes", "1", "buy", "sell"}


def side_direction(side: SignalSide) -> str:
    return "up" if side == SignalSide.BUY else "down"


def opposite_direction(side: SignalSide) -> str:
    return "down" if side == SignalSide.BUY else "up"


def direction_name(side: SignalSide) -> str:
    return "bull" if side == SignalSide.BUY else "bear"


def opposite_side(side: SignalSide) -> SignalSide:
    return SignalSide.SELL if side == SignalSide.BUY else SignalSide.BUY
