from collections.abc import Mapping
from dataclasses import dataclass, replace

from alphaflow.strategy.models import (
    ExitReasonType,
    ExitRule,
    IndicatorSeriesAnalysis,
    IndicatorWindowAnalysis,
    MarketAnalysis,
    MarketSnapshot,
    PositionSide,
    PositionState,
    Signal,
    SignalSeriesAnalysis,
    SignalSide,
    StrategyContext,
    StrategyResult,
    StrategyTarget,
    TimeframeWindow,
    WindowAnalysis,
)
from alphaflow.strategy.position import PositionManager


@dataclass(frozen=True)
class SupertrendStrategyConfig:
    entry_interval: str = "3m"
    confirmation_intervals: tuple[str, ...] = ("5m", "10m", "15m", "30m")
    entry_threshold: float = 0.72
    min_stable_supertrend_bars: int = 2
    max_blocking_timeframes: int = 1
    ema_tangle_distance_pct: float = 0.03


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

        quality = latest_signal(snapshot.indicator_window, "data_quality")
        if quality not in {"", "ok"}:
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"indicator quality not ok: {quality}"),
            )

        long_score, long_reasons, long_blocked = score_side(snapshot, SignalSide.BUY, self._config)
        short_score, short_reasons, short_blocked = score_side(
            snapshot,
            SignalSide.SELL,
            self._config,
        )

        exit_result = exit_signal(
            snapshot,
            position,
            long_score,
            long_reasons,
            short_score,
            short_reasons,
            self._config,
        )
        if exit_result is not None:
            return result(snapshot, self.name, exit_result)

        if (
            not long_blocked
            and long_score >= self._config.entry_threshold
            and long_score >= short_score
        ):
            return result(
                snapshot,
                self.name,
                signal(snapshot, SignalSide.BUY, long_score, "; ".join(long_reasons)),
            )
        if not short_blocked and short_score >= self._config.entry_threshold:
            return result(
                snapshot,
                self.name,
                signal(snapshot, SignalSide.SELL, short_score, "; ".join(short_reasons)),
            )

        reasons = long_reasons if long_score >= short_score else short_reasons
        score = max(long_score, short_score)
        if long_blocked and short_blocked:
            reasons = ["both sides blocked by weak or conflicting windows"]
            reasons.extend(long_reasons if long_score >= short_score else short_reasons)
        return result(snapshot, self.name, hold(snapshot, "; ".join(reasons), score=score))


def exit_signal(
    snapshot: MarketSnapshot,
    position: PositionState | None,
    long_score: float,
    long_reasons: list[str],
    short_score: float,
    short_reasons: list[str],
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
                max(short_score, config.entry_threshold),
                reason,
            )
        return hold(
            snapshot,
            "long exit deferred: waiting for confirmed bearish window; " + "; ".join(short_reasons),
            score=short_score,
        )
    if position.side == PositionSide.SHORT:
        should_exit, reason = should_exit_position(snapshot, SignalSide.BUY)
        if should_exit:
            return signal(
                snapshot,
                SignalSide.BUY,
                max(long_score, config.entry_threshold),
                reason,
            )
        return hold(
            snapshot,
            "short exit deferred: waiting for confirmed bullish window; " + "; ".join(long_reasons),
            score=long_score,
        )
    return None


def should_exit_position(snapshot: MarketSnapshot, exit_side: SignalSide) -> tuple[bool, str]:
    indicator_window = snapshot.indicator_window
    if indicator_window is None:
        return False, "indicator window missing"

    expected = "up" if exit_side == SignalSide.BUY else "down"
    supertrend = indicator_window.signals.get("supertrend_direction")
    if supertrend is None or supertrend.latest != expected:
        return False, f"3m supertrend has not turned {expected}"

    if ema_macd_confirm_exit(indicator_window, exit_side):
        return True, f"confirmed {expected} exit: supertrend, ema and macd aligned"
    if short_timeframes_block_exit(snapshot.timeframe_windows, exit_side):
        return True, f"confirmed {expected} exit: 5m and 10m blocking"
    if supertrend.stable_count > 1:
        return (
            True,
            f"confirmed {expected} exit: supertrend stable for {supertrend.stable_count} bars",
        )
    return False, f"3m supertrend turned {expected} but exit confirmation is weak"


def score_side(
    snapshot: MarketSnapshot,
    side: SignalSide,
    config: SupertrendStrategyConfig,
) -> tuple[float, list[str], bool]:
    indicator_window = snapshot.indicator_window
    if indicator_window is None:
        return 0.0, ["indicator window missing"], True

    expected = "up" if side == SignalSide.BUY else "down"
    score = 0.0
    reasons: list[str] = []
    blocked = False

    supertrend = indicator_window.signals.get("supertrend_direction")
    if not supertrend or supertrend.latest != expected:
        return 0.0, [f"3m supertrend is not {expected}"], True
    if supertrend.changed or supertrend.stable_count >= config.min_stable_supertrend_bars:
        score += 0.3
        reasons.append(f"3m supertrend {expected}")
    else:
        reasons.append("3m supertrend not stable enough")
        blocked = True

    if frequent_flip(supertrend):
        reasons.append("3m supertrend flipped too recently")
        blocked = True

    ema_score, ema_reasons, ema_blocked = score_ema_window(indicator_window, side, config)
    score += ema_score
    reasons.extend(ema_reasons)
    blocked = blocked or ema_blocked

    macd_score, macd_reasons, macd_blocked = score_macd_window(indicator_window, side)
    score += macd_score
    reasons.extend(macd_reasons)
    blocked = blocked or macd_blocked

    strength_score, strength_reasons, strength_blocked = score_strength_windows(
        indicator_window,
        snapshot.window,
        side,
    )
    score += strength_score
    reasons.extend(strength_reasons)
    blocked = blocked or strength_blocked

    multi_score, multi_reasons, multi_blocked = score_confirmation_timeframes(
        snapshot.timeframe_windows,
        side,
        config,
    )
    score += multi_score
    reasons.extend(multi_reasons)
    blocked = blocked or multi_blocked

    return min(1.0, score), reasons, blocked


def score_ema_window(
    window: IndicatorWindowAnalysis,
    side: SignalSide,
    config: SupertrendStrategyConfig,
) -> tuple[float, list[str], bool]:
    expected_alignment = "bull" if side == SignalSide.BUY else "bear"
    expected_direction = "rising" if side == SignalSide.BUY else "falling"
    reasons: list[str] = []
    score = 0.0
    blocked = False

    ema7 = window.values.get("ema7")
    ema25 = window.values.get("ema25")
    ema99 = window.values.get("ema99")
    slope = window.values.get("ema25_slope5_pct")
    alignment = window.signals.get("ema_alignment")

    if ema7 is None or ema25 is None:
        return 0.0, ["ema7 or ema25 window missing"], True

    if ema_relation_ok(ema7, ema25, side):
        score += 0.08
        reasons.append("ema7/ema25 directional relation")
    else:
        reasons.append("ema7/ema25 relation not directional")
        blocked = True

    if ema7.direction == expected_direction and ema25.direction == expected_direction:
        score += 0.05
        reasons.append("ema short and medium windows aligned")
    else:
        reasons.append("ema windows not moving together")
        blocked = True

    if slope is not None and slope.direction == expected_direction and latest_on_side(slope, side):
        score += 0.04
        reasons.append("ema25 slope confirms direction")
    else:
        reasons.append("ema25 slope weak")
        blocked = True

    if alignment is not None and alignment.latest == expected_alignment:
        score += 0.03
        reasons.append(f"ema alignment {expected_alignment}")
    elif ema99 is not None and ema_relation_ok(ema25, ema99, side):
        score += 0.02
        reasons.append("ema99 does not block trend")
    else:
        reasons.append("ema alignment mixed")

    if ema_tangled(ema7, ema25, config.ema_tangle_distance_pct):
        reasons.append("ema7/ema25 tangled")
        blocked = True

    return score, reasons, blocked


def score_macd_window(
    window: IndicatorWindowAnalysis,
    side: SignalSide,
) -> tuple[float, list[str], bool]:
    expected_direction = "rising" if side == SignalSide.BUY else "falling"
    opposite_momentum = "expanding_bear" if side == SignalSide.BUY else "expanding_bull"
    bad_divergence = "bearish" if side == SignalSide.BUY else "bullish"
    reasons: list[str] = []
    score = 0.0
    blocked = False

    hist = window.values.get("macd_hist")
    hist_delta = window.values.get("macd_hist_delta")
    fast_hist = window.values.get("macd_fast_hist")
    fast_hist_delta = window.values.get("macd_fast_hist_delta")
    momentum = window.signals.get("macd_momentum")
    fast_momentum = window.signals.get("macd_fast_momentum")
    divergence = window.signals.get("macd_divergence")
    fast_divergence = window.signals.get("macd_fast_divergence")

    if hist is None:
        return 0.0, ["macd histogram window missing"], True
    if hist.direction == expected_direction:
        score += 0.12
        reasons.append("macd histogram follows direction")
    else:
        reasons.append("macd histogram does not follow")
        blocked = True

    if hist_delta is not None and latest_on_side(hist_delta, side):
        score += 0.04
        reasons.append("macd histogram delta supports move")

    if fast_hist is not None:
        if fast_hist.direction == expected_direction:
            score += 0.04
            reasons.append("fast macd histogram follows direction")
        else:
            reasons.append("fast macd histogram does not follow")
            blocked = True

    if fast_hist_delta is not None and latest_on_side(fast_hist_delta, side):
        score += 0.02
        reasons.append("fast macd histogram delta supports move")

    if momentum is not None and momentum.latest == opposite_momentum:
        reasons.append(f"macd momentum blocks: {opposite_momentum}")
        blocked = True
    elif momentum is not None and momentum.latest:
        score += 0.02
        reasons.append(f"macd momentum {momentum.latest}")

    if divergence is not None and divergence.latest == bad_divergence:
        reasons.append(f"macd divergence blocks: {bad_divergence}")
        blocked = True

    if fast_momentum is not None and fast_momentum.latest == opposite_momentum:
        reasons.append(f"fast macd momentum blocks: {opposite_momentum}")
        blocked = True
    elif fast_momentum is not None and fast_momentum.latest:
        score += 0.01
        reasons.append(f"fast macd momentum {fast_momentum.latest}")

    if fast_divergence is not None and fast_divergence.latest == bad_divergence:
        reasons.append(f"fast macd divergence blocks: {bad_divergence}")
        blocked = True

    return score, reasons, blocked


def score_strength_windows(
    indicator_window: IndicatorWindowAnalysis,
    kline_window: WindowAnalysis | None,
    side: SignalSide,
) -> tuple[float, list[str], bool]:
    reasons: list[str] = []
    score = 0.0
    blocked = False

    adx = indicator_window.values.get("adx14")
    di = indicator_window.signals.get("di_direction")
    rsi = indicator_window.values.get("rsi14")
    obv_slope = indicator_window.values.get("obv_slope5")
    price_volume = indicator_window.signals.get("price_volume_confirmation")
    indicator_volume = indicator_window.signals.get("volume_state")

    if adx is not None and (adx.direction == "rising" or adx.latest >= 20):
        score += 0.07
        reasons.append("adx trend strength improving")
    else:
        reasons.append("adx trend strength weak")

    bad_di = "bear" if side == SignalSide.BUY else "bull"
    if di is not None and di.latest == bad_di:
        reasons.append(f"di direction blocks: {bad_di}")
        blocked = True
    elif di is not None and di.latest:
        score += 0.04
        reasons.append(f"di direction {di.latest}")

    if rsi is not None:
        if side == SignalSide.BUY and rsi.latest >= 75:
            reasons.append("rsi overheated")
            blocked = True
        elif side == SignalSide.SELL and rsi.latest <= 25:
            reasons.append("rsi oversold")
            blocked = True
        else:
            score += 0.03
            reasons.append("rsi not extreme")

    flow_score, flow_reasons, flow_blocked = score_money_flow(
        obv_slope,
        price_volume,
        indicator_volume,
        side,
    )
    score += flow_score
    reasons.extend(flow_reasons)
    blocked = blocked or flow_blocked

    if kline_window is not None:
        if side == SignalSide.BUY and kline_window.trend == "down":
            reasons.append("kline window downtrend blocks long")
            blocked = True
        elif side == SignalSide.SELL and kline_window.trend == "up":
            reasons.append("kline window uptrend blocks short")
            blocked = True
        elif kline_window.volume_state == "expanding":
            score += 0.04
            reasons.append("volume expanding")
        elif kline_window.trend == "range" and kline_window.volume_state != "expanding":
            reasons.append("range without volume expansion")
            blocked = True

    return score, reasons, blocked


def score_money_flow(
    obv_slope: IndicatorSeriesAnalysis | None,
    price_volume: SignalSeriesAnalysis | None,
    indicator_volume: SignalSeriesAnalysis | None,
    side: SignalSide,
) -> tuple[float, list[str], bool]:
    expected_direction = "rising" if side == SignalSide.BUY else "falling"
    expected_confirmation = "confirm_up" if side == SignalSide.BUY else "confirm_down"
    bad_confirmation = "divergence_bear" if side == SignalSide.BUY else "divergence_bull"
    reasons: list[str] = []
    score = 0.0
    blocked = False

    if obv_slope is not None:
        if latest_on_side(obv_slope, side) and obv_slope.direction == expected_direction:
            score += 0.04
            reasons.append("obv slope confirms flow")
        elif obv_slope.latest != 0:
            reasons.append("obv slope does not confirm flow")
            blocked = True

    if price_volume is not None:
        if price_volume.latest == expected_confirmation:
            score += 0.04
            reasons.append(f"price-volume {expected_confirmation}")
        elif price_volume.latest == bad_confirmation:
            reasons.append(f"price-volume blocks: {bad_confirmation}")
            blocked = True
        elif price_volume.latest == "neutral":
            reasons.append("price-volume neutral")

    if indicator_volume is not None and indicator_volume.latest in {"spike", "climax"}:
        score += 0.02
        reasons.append(f"indicator volume {indicator_volume.latest}")

    return score, reasons, blocked


def score_confirmation_timeframes(
    windows: Mapping[str, TimeframeWindow],
    side: SignalSide,
    config: SupertrendStrategyConfig,
) -> tuple[float, list[str], bool]:
    if not windows:
        return 0.0, ["confirmation windows missing"], False

    states = {
        interval: classify_timeframe(window, side)
        for interval, window in windows.items()
        if interval in config.confirmation_intervals
    }
    aligned = sum(1 for state in states.values() if state == "aligned")
    improving = sum(1 for state in states.values() if state == "improving")
    blocking = sum(1 for state in states.values() if state == "blocking")
    reasons = [f"timeframes aligned={aligned} improving={improving} blocking={blocking}"]

    if states.get("5m") == "blocking" and states.get("10m") == "blocking":
        reasons.append("5m and 10m both blocking")
        return 0.0, reasons, True
    if blocking > config.max_blocking_timeframes:
        return 0.0, reasons, True

    score = min(0.19, aligned * 0.05 + improving * 0.025)
    return score, reasons, False


def classify_timeframe(window: TimeframeWindow, side: SignalSide) -> str:
    if not window.is_ok() or window.indicator_window is None:
        return "missing"

    expected = "up" if side == SignalSide.BUY else "down"
    opposite = "down" if side == SignalSide.BUY else "up"
    expected_ema = "bull" if side == SignalSide.BUY else "bear"
    opposite_ema = "bear" if side == SignalSide.BUY else "bull"
    expected_macd = "rising" if side == SignalSide.BUY else "falling"
    opposite_macd = "falling" if side == SignalSide.BUY else "rising"

    signals = window.indicator_window.signals
    values = window.indicator_window.values
    supertrend = signals.get("supertrend_direction")
    ema = signals.get("ema_alignment")
    macd = values.get("macd_hist")
    slope = values.get("ema25_slope5_pct")

    if supertrend is not None and supertrend.latest == expected:
        return "aligned"
    if (
        supertrend is not None
        and supertrend.latest == opposite
        and ema is not None
        and ema.latest == opposite_ema
        and macd is not None
        and macd.direction == opposite_macd
    ):
        return "blocking"
    if (
        macd is not None
        and macd.direction == expected_macd
        and (slope is None or slope.direction == expected_macd)
    ):
        return "improving"
    if ema is not None and ema.latest == expected_ema:
        return "improving"
    return "neutral"


def ema_macd_confirm_exit(window: IndicatorWindowAnalysis, side: SignalSide) -> bool:
    expected_direction = "rising" if side == SignalSide.BUY else "falling"
    ema7 = window.values.get("ema7")
    ema25 = window.values.get("ema25")
    ema_slope = window.values.get("ema25_slope5_pct")
    macd = window.values.get("macd_hist")
    macd_momentum = window.signals.get("macd_momentum")
    expected_momentum = "expanding_bull" if side == SignalSide.BUY else "expanding_bear"
    ema_confirmed = (
        ema7 is not None
        and ema25 is not None
        and ema_relation_ok(ema7, ema25, side)
        and ema7.direction == expected_direction
        and ema25.direction == expected_direction
        and (ema_slope is None or ema_slope.direction == expected_direction)
    )
    macd_confirmed = (
        macd is not None
        and macd.direction == expected_direction
        and (macd_momentum is None or macd_momentum.latest in {"", "none", expected_momentum})
    )
    return ema_confirmed and macd_confirmed


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


def frequent_flip(signal_series: SignalSeriesAnalysis) -> bool:
    return (
        signal_series.changed
        and signal_series.previous not in {"", "none"}
        and signal_series.stable_count < 1
    )


def ema_relation_ok(
    fast: IndicatorSeriesAnalysis,
    slow: IndicatorSeriesAnalysis,
    side: SignalSide,
) -> bool:
    if side == SignalSide.BUY:
        return fast.latest > slow.latest
    return fast.latest < slow.latest


def ema_tangled(
    fast: IndicatorSeriesAnalysis,
    slow: IndicatorSeriesAnalysis,
    threshold_pct: float,
) -> bool:
    if slow.latest == 0:
        return False
    distance_pct = abs(fast.latest - slow.latest) / abs(slow.latest) * 100
    return distance_pct < threshold_pct


def latest_signal(window: IndicatorWindowAnalysis, key: str) -> str:
    signal_series = window.signals.get(key)
    if signal_series is None:
        return ""
    return signal_series.latest


def optional_float(value: str | None) -> float | None:
    if value is None or value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


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
    return MarketAnalysis(
        summary=f"{summary}; {price_text}",
        trend=window.trend if window is not None else "",
        momentum=latest_signal(snapshot.indicator_window, "macd_momentum")
        if snapshot.indicator_window is not None
        else "",
        volatility=latest_signal(snapshot.indicator_window, "volatility_state")
        if snapshot.indicator_window is not None
        else "",
        volume=window.volume_state if window is not None else "",
        risk=latest_signal(snapshot.indicator_window, "data_quality")
        if snapshot.indicator_window is not None
        else "unknown",
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


def latest_on_side(series: IndicatorSeriesAnalysis, side: SignalSide) -> bool:
    if side == SignalSide.BUY:
        return series.latest > 0
    return series.latest < 0
