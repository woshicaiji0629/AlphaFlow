from alphaflow.strategy.models import (
    ExitReasonType,
    ExitRule,
    MarketAnalysis,
    MarketSnapshot,
    Signal,
    SignalSide,
    StrategyResult,
)


class RuleStrategy:
    name = "rule"

    def evaluate(self, snapshot: MarketSnapshot) -> StrategyResult:
        indicator = snapshot.indicator
        health = snapshot.health
        if not health.is_ok():
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"health not ok: {health.reason or 'unknown'}"),
            )

        data_quality = indicator.signals.get("data_quality", "ok")
        if data_quality != "ok":
            reason = indicator.signals.get("data_quality_reason", data_quality)
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"indicator data quality not ok: {reason}"),
            )

        rsi = optional_float(indicator.values.get("rsi_14"))
        macd_hist = optional_float(indicator.values.get("macd_hist"))
        if rsi is None or macd_hist is None:
            return result(
                snapshot,
                self.name,
                hold(snapshot, "required indicators missing: rsi_14 or macd_hist"),
            )

        score = score_signal(rsi, macd_hist)
        if score >= 0.65:
            return result(
                snapshot,
                self.name,
                signal(
                    snapshot,
                    SignalSide.BUY,
                    score,
                    "rsi recovery with positive macd histogram",
                ),
            )
        if score <= -0.65:
            return result(
                snapshot,
                self.name,
                signal(
                    snapshot,
                    SignalSide.SELL,
                    score,
                    "rsi weakness with negative macd histogram",
                ),
            )
        return result(snapshot, self.name, hold(snapshot, "no strong rule match", score=score))


class TrendMomentumStrategy:
    name = "trend_momentum"

    def evaluate(self, snapshot: MarketSnapshot) -> StrategyResult:
        indicator = snapshot.indicator
        if not snapshot.health.is_ok():
            sig = hold(snapshot, f"health not ok: {snapshot.health.reason or 'unknown'}")
            return result(snapshot, self.name, sig)

        data_quality = indicator.signals.get("data_quality", "ok")
        if data_quality != "ok":
            reason = indicator.signals.get("data_quality_reason", data_quality)
            return result(
                snapshot,
                self.name,
                hold(snapshot, f"indicator data quality not ok: {reason}"),
            )

        score = 0.0
        reasons: list[str] = []

        ema_alignment = indicator.signals.get("ema_alignment", "")
        if ema_alignment in {"bullish", "bull"}:
            score += 0.35
            reasons.append("ema bullish")
        elif ema_alignment in {"bearish", "bear"}:
            score -= 0.35
            reasons.append("ema bearish")

        ma_cross = indicator.signals.get("ma_cross", "")
        if ma_cross == "golden":
            score += 0.2
            reasons.append("ma golden cross")
        elif ma_cross == "dead":
            score -= 0.2
            reasons.append("ma dead cross")

        rsi = optional_float(indicator.values.get("rsi_14"))
        if rsi is not None:
            if 45 <= rsi <= 70:
                score += 0.2
                reasons.append("rsi constructive")
            elif rsi < 40:
                score -= 0.2
                reasons.append("rsi weak")
            elif rsi > 75:
                score -= 0.1
                reasons.append("rsi overheated")

        macd_hist = optional_float(indicator.values.get("macd_hist"))
        if macd_hist is not None:
            if macd_hist > 0:
                score += 0.25
                reasons.append("macd positive")
            elif macd_hist < 0:
                score -= 0.25
                reasons.append("macd negative")

        if snapshot.indicator_window is not None:
            macd_window = snapshot.indicator_window.values.get("macd_hist")
            if macd_window is not None:
                if macd_window.direction == "rising":
                    score += 0.15
                    reasons.append("macd histogram rising")
                elif macd_window.direction == "falling":
                    score -= 0.15
                    reasons.append("macd histogram falling")
            rsi_window = snapshot.indicator_window.values.get("rsi_14")
            if rsi_window is not None:
                if rsi_window.direction == "rising":
                    score += 0.1
                    reasons.append("rsi rising")
                elif rsi_window.direction == "falling":
                    score -= 0.1
                    reasons.append("rsi falling")

        volatility = indicator.signals.get("volatility_state", "")
        if volatility in {"high", "extreme"}:
            score *= 0.75
            reasons.append("high volatility discount")

        if snapshot.window is not None:
            if snapshot.window.trend == "up":
                score += 0.15
                reasons.append("window uptrend")
            elif snapshot.window.trend == "down":
                score -= 0.15
                reasons.append("window downtrend")
            if snapshot.window.volume_state == "expanding":
                score *= 1.1
                reasons.append("volume expansion")

        score = max(-1.0, min(1.0, score))
        reason = ", ".join(reasons) if reasons else "no strong trend momentum evidence"
        if score >= 0.65:
            sig = signal(snapshot, SignalSide.BUY, score, reason)
        elif score <= -0.65:
            sig = signal(snapshot, SignalSide.SELL, score, reason)
        else:
            sig = hold(snapshot, reason, score=score)
        return result(snapshot, self.name, sig)


def optional_float(value: str | None) -> float | None:
    if value is None or value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


def score_signal(rsi: float, macd_hist: float) -> float:
    score = 0.0
    if rsi <= 35:
        score += 0.45
    elif rsi >= 65:
        score -= 0.45
    elif rsi > 55:
        score += 0.2
    elif rsi < 45:
        score -= 0.2

    if macd_hist > 0:
        score += 0.35
    elif macd_hist < 0:
        score -= 0.35

    return max(-1.0, min(1.0, score))


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
    values = snapshot.indicator.values
    signals = snapshot.indicator.signals
    window = snapshot.window
    key_levels = {
        key: value
        for key, value in values.items()
        if key
        in {
            "support_1",
            "support_2",
            "resistance_1",
            "resistance_2",
            "swing_high",
            "swing_low",
        }
    }
    price = current_price(snapshot)
    price_text = f"current price {price}" if price else "current price unavailable"
    trend = (
        window.trend
        if window is not None
        else signals.get("ema_alignment", "") or signals.get("trend", "")
    )
    volume = (
        window.volume_state
        if window is not None
        else signals.get("price_volume_confirmation", "") or signals.get("money_flow", "")
    )
    return MarketAnalysis(
        summary=f"{summary}; {price_text}",
        trend=trend,
        momentum=signals.get("macd_cross", "") or signals.get("kdj_cross", ""),
        volatility=signals.get("volatility_state", ""),
        volume=volume,
        risk=signals.get("data_quality", "ok"),
        key_levels=key_levels,
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
    if side == SignalSide.BUY:
        return price_exit_rules(
            take_profit_price=snapshot.indicator.values.get("resistance_1", ""),
            stop_loss_price=snapshot.indicator.values.get("support_1", ""),
        )
    if side == SignalSide.SELL:
        return price_exit_rules(
            take_profit_price=snapshot.indicator.values.get("support_1", ""),
            stop_loss_price=snapshot.indicator.values.get("resistance_1", ""),
        )
    return ()


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
                reason="stop loss guard",
            )
        )
    return tuple(rules)
