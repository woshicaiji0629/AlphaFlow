from alphaflow.strategy.models import MarketSnapshot, Signal, SignalSide


class RuleStrategy:
    def evaluate(self, snapshot: MarketSnapshot) -> Signal:
        indicator = snapshot.indicator
        health = snapshot.health
        if not health.is_ok():
            return hold(snapshot, f"health not ok: {health.reason or 'unknown'}")

        data_quality = indicator.signals.get("data_quality", "ok")
        if data_quality != "ok":
            reason = indicator.signals.get("data_quality_reason", data_quality)
            return hold(snapshot, f"indicator data quality not ok: {reason}")

        rsi = optional_float(indicator.values.get("rsi_14"))
        macd_hist = optional_float(indicator.values.get("macd_hist"))
        if rsi is None or macd_hist is None:
            return hold(snapshot, "required indicators missing: rsi_14 or macd_hist")

        score = score_signal(rsi, macd_hist)
        if score >= 0.65:
            return signal(
                snapshot,
                SignalSide.BUY,
                score,
                "rsi recovery with positive macd histogram",
            )
        if score <= -0.65:
            return signal(
                snapshot,
                SignalSide.SELL,
                score,
                "rsi weakness with negative macd histogram",
            )
        return hold(snapshot, "no strong rule match", score=score)


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
