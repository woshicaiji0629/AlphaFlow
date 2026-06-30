import pytest

from alphaflow.strategy import IndicatorSnapshot
from alphaflow.strategy.indicator_window import analyze_indicators


def test_indicator_window_analyzes_numeric_series() -> None:
    analysis = analyze_indicators(
        (
            indicator_snapshot(1000, {"macd_hist": "0.30", "rsi_14": "60"}),
            indicator_snapshot(2000, {"macd_hist": "0.20", "rsi_14": "55"}),
            indicator_snapshot(3000, {"macd_hist": "0.05", "rsi_14": "50"}),
        )
    )

    macd = analysis.values["macd_hist"]

    assert analysis.sample_count == 3
    assert macd.latest == 0.05
    assert macd.previous == 0.2
    assert macd.change == pytest.approx(-0.15)
    assert macd.direction == "falling"
    assert macd.falling_count == 2
    assert macd.range_position_pct == 0


def test_indicator_window_analyzes_signal_series() -> None:
    analysis = analyze_indicators(
        (
            indicator_snapshot(1000, signals={"ma_cross": "golden"}),
            indicator_snapshot(2000, signals={"ma_cross": "dead"}),
            indicator_snapshot(3000, signals={"ma_cross": "dead"}),
        )
    )

    ma_cross = analysis.signals["ma_cross"]

    assert ma_cross.latest == "dead"
    assert ma_cross.previous == "dead"
    assert not ma_cross.changed
    assert ma_cross.stable_count == 2
    assert ma_cross.last_changed_ago == 2


def indicator_snapshot(
    open_time: int,
    values: dict[str, str] | None = None,
    signals: dict[str, str] | None = None,
) -> IndicatorSnapshot:
    return IndicatorSnapshot(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        interval="1m",
        open_time=open_time,
        close_time=open_time + 59999,
        values=values or {},
        signals=signals or {},
        updated_at=open_time + 60000,
    )
