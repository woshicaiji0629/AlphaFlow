from collections.abc import Mapping
from types import MappingProxyType

from alphaflow.strategy.models import (
    IndicatorSeriesAnalysis,
    IndicatorSnapshot,
    IndicatorWindowAnalysis,
    SignalSeriesAnalysis,
)


def analyze_indicators(history: tuple[IndicatorSnapshot, ...]) -> IndicatorWindowAnalysis:
    if not history:
        return IndicatorWindowAnalysis(sample_count=0)
    return IndicatorWindowAnalysis(
        sample_count=len(history),
        values=MappingProxyType(analyze_value_series(history)),
        signals=MappingProxyType(analyze_signal_series(history)),
    )


def analyze_value_series(
    history: tuple[IndicatorSnapshot, ...],
) -> dict[str, IndicatorSeriesAnalysis]:
    keys = sorted({key for snapshot in history for key in snapshot.values})
    analyses: dict[str, IndicatorSeriesAnalysis] = {}
    for key in keys:
        values = [
            value
            for value in (optional_float(snapshot.values.get(key, "")) for snapshot in history)
            if value is not None
        ]
        if len(values) >= 2:
            analyses[key] = analyze_numeric_values(values)
    return analyses


def analyze_signal_series(
    history: tuple[IndicatorSnapshot, ...],
) -> dict[str, SignalSeriesAnalysis]:
    keys = sorted({key for snapshot in history for key in snapshot.signals})
    analyses: dict[str, SignalSeriesAnalysis] = {}
    for key in keys:
        values = [snapshot.signals.get(key, "") for snapshot in history if key in snapshot.signals]
        if values:
            analyses[key] = analyze_signal_values(values)
    return analyses


def analyze_numeric_values(values: list[float]) -> IndicatorSeriesAnalysis:
    latest = values[-1]
    previous = values[-2]
    change = latest - previous
    minimum = min(values)
    maximum = max(values)
    return IndicatorSeriesAnalysis(
        latest=latest,
        previous=previous,
        change=change,
        change_pct=percent_change(latest, previous),
        slope=slope(values),
        direction=direction(values),
        rising_count=consecutive_count(values, rising=True),
        falling_count=consecutive_count(values, rising=False),
        minimum=minimum,
        maximum=maximum,
        range_position_pct=range_position(latest, minimum, maximum),
    )


def analyze_signal_values(values: list[str]) -> SignalSeriesAnalysis:
    latest = values[-1]
    previous = values[-2] if len(values) >= 2 else ""
    return SignalSeriesAnalysis(
        latest=latest,
        previous=previous,
        changed=latest != previous,
        stable_count=stable_count(values),
        last_changed_ago=last_changed_ago(values),
    )


def optional_float(value: str) -> float | None:
    if value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


def percent_change(current: float, previous: float) -> float:
    if previous == 0:
        return 0.0
    return (current - previous) / previous * 100


def slope(values: list[float]) -> float:
    if len(values) < 2:
        return 0.0
    return (values[-1] - values[0]) / max(1, len(values) - 1)


def direction(values: list[float]) -> str:
    current_slope = slope(values[-min(20, len(values)) :])
    if current_slope > 0:
        return "rising"
    if current_slope < 0:
        return "falling"
    return "flat"


def consecutive_count(values: list[float], rising: bool) -> int:
    count = 0
    for previous, current in zip(reversed(values[:-1]), reversed(values[1:]), strict=False):
        if rising and current > previous:
            count += 1
            continue
        if not rising and current < previous:
            count += 1
            continue
        break
    return count


def range_position(value: float, minimum: float, maximum: float) -> float:
    if maximum <= minimum:
        return 50.0
    return max(0.0, min(100.0, (value - minimum) / (maximum - minimum) * 100))


def stable_count(values: list[str]) -> int:
    latest = values[-1]
    count = 0
    for value in reversed(values):
        if value != latest:
            break
        count += 1
    return count


def last_changed_ago(values: list[str]) -> int:
    latest = values[-1]
    for index, value in enumerate(reversed(values[:-1]), start=1):
        if value != latest:
            return index
    return len(values) - 1


def as_mapping(analysis: IndicatorWindowAnalysis) -> Mapping[str, IndicatorSeriesAnalysis]:
    return analysis.values
