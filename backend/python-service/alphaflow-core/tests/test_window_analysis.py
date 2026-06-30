from alphaflow.strategy import Kline
from alphaflow.strategy.window import analyze_klines


def test_window_analysis_detects_uptrend_and_volume_expansion() -> None:
    klines = tuple(
        make_kline(index, close=100 + index, volume=300 if index >= 180 else 10)
        for index in range(200)
    )

    analysis = analyze_klines(klines)

    assert analysis.sample_count == 200
    assert analysis.trend == "up"
    assert analysis.momentum == "strengthening"
    assert analysis.volume_state == "expanding"
    assert analysis.range_position_pct == 100


def test_window_analysis_uses_only_closed_klines() -> None:
    klines = (
        make_kline(0, close=100, volume=10),
        make_kline(1, close=110, volume=20, closed=False),
    )

    analysis = analyze_klines(klines)

    assert analysis.sample_count == 1
    assert analysis.trend == "unknown"


def make_kline(index: int, close: float, volume: float, closed: bool = True) -> Kline:
    return Kline(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        interval="1m",
        open_time=index * 60_000,
        close_time=index * 60_000 + 59_999,
        open=str(close - 1),
        high=str(close),
        low=str(close - 2),
        close=str(close),
        volume=str(volume),
        is_closed=closed,
    )
