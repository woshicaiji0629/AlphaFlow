from collections.abc import Iterable

from alphaflow.strategy.models import Kline, WindowAnalysis


def analyze_klines(klines: tuple[Kline, ...], lookback: int = 200) -> WindowAnalysis:
    window = tuple(kline for kline in klines[-lookback:] if kline.is_closed)
    if len(window) < 2:
        return WindowAnalysis(sample_count=len(window), lookback=lookback)

    closes = parsed_values(kline.close for kline in window)
    highs = parsed_values(kline.high for kline in window)
    lows = parsed_values(kline.low for kline in window)
    volumes = parsed_values(kline.volume for kline in window)
    if len(closes) < 2 or not highs or not lows:
        return WindowAnalysis(sample_count=len(window), lookback=lookback)

    first_close = closes[0]
    last_close = closes[-1]
    high = max(highs)
    low = min(lows)
    close_change_pct = percent_change(last_close, first_close)
    recent_change_pct = recent_percent_change(closes)
    close_slope_pct = slope_pct(closes)
    range_position_pct = range_position(last_close, low, high)
    volume_ratio = recent_volume_ratio(volumes)
    return WindowAnalysis(
        sample_count=len(window),
        lookback=lookback,
        close_change_pct=close_change_pct,
        recent_change_pct=recent_change_pct,
        high=format_number(high),
        low=format_number(low),
        range_position_pct=range_position_pct,
        close_slope_pct=close_slope_pct,
        volume_ratio=volume_ratio,
        trend=trend_state(close_change_pct, close_slope_pct),
        momentum=momentum_state(recent_change_pct),
        volume_state=volume_state(volume_ratio),
    )


def optional_float(value: str) -> float | None:
    if value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


def parsed_values(values: Iterable[str]) -> list[float]:
    return [value for value in (optional_float(item) for item in values) if value is not None]


def percent_change(current: float, previous: float) -> float:
    if previous == 0:
        return 0.0
    return (current - previous) / previous * 100


def recent_percent_change(values: list[float], lookback: int = 20) -> float:
    if len(values) < 2:
        return 0.0
    start = values[-min(lookback, len(values))]
    return percent_change(values[-1], start)


def slope_pct(values: list[float]) -> float:
    if len(values) < 2 or values[0] == 0:
        return 0.0
    return (values[-1] - values[0]) / values[0] * 100 / max(1, len(values) - 1)


def range_position(close: float, low: float, high: float) -> float:
    if high <= low:
        return 50.0
    return max(0.0, min(100.0, (close - low) / (high - low) * 100))


def recent_volume_ratio(volumes: list[float], recent: int = 20, baseline: int = 100) -> float:
    if not volumes:
        return 0.0
    recent_values = volumes[-min(recent, len(volumes)) :]
    baseline_values = volumes[-min(baseline, len(volumes)) :]
    baseline_avg = average(baseline_values)
    if baseline_avg == 0:
        return 0.0
    return average(recent_values) / baseline_avg


def average(values: list[float]) -> float:
    if not values:
        return 0.0
    return sum(values) / len(values)


def trend_state(close_change_pct: float, close_slope_pct: float) -> str:
    if close_change_pct >= 3 and close_slope_pct > 0:
        return "up"
    if close_change_pct <= -3 and close_slope_pct < 0:
        return "down"
    return "range"


def momentum_state(recent_change_pct: float) -> str:
    if recent_change_pct >= 1:
        return "strengthening"
    if recent_change_pct <= -1:
        return "weakening"
    return "neutral"


def volume_state(volume_ratio: float) -> str:
    if volume_ratio >= 1.5:
        return "expanding"
    if 0 < volume_ratio <= 0.7:
        return "contracting"
    return "normal"


def format_number(value: float) -> str:
    return f"{value:.12g}"
