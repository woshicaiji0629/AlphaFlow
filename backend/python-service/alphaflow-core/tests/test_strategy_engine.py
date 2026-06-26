from alphaflow.strategy import (
    DataHealth,
    IndicatorSnapshot,
    MarketSnapshot,
    SignalSide,
    StrategyEngine,
)


def test_strategy_holds_when_health_is_not_ok() -> None:
    snapshot = make_snapshot(
        health=DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
            kline_status="stale",
            indicator_status="ok",
            reason="kline stale",
        )
    )

    signal = StrategyEngine().evaluate(snapshot)

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "health not ok: kline stale"


def test_strategy_holds_when_indicator_quality_is_not_ok() -> None:
    snapshot = make_snapshot(
        signals={"data_quality": "gap", "data_quality_reason": "missing kline"}
    )

    signal = StrategyEngine().evaluate(snapshot)

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "indicator data quality not ok: missing kline"


def test_strategy_holds_when_required_fields_are_missing() -> None:
    snapshot = make_snapshot(values={"rsi_14": "30"})

    signal = StrategyEngine().evaluate(snapshot)

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "required indicators missing: rsi_14 or macd_hist"


def test_strategy_emits_buy_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "32", "macd_hist": "0.12"})

    signal = StrategyEngine().evaluate(snapshot)

    assert signal.side == SignalSide.BUY
    assert signal.score == 0.8
    assert signal.confidence == 0.8


def test_strategy_emits_sell_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "70", "macd_hist": "-0.08"})

    signal = StrategyEngine().evaluate(snapshot)

    assert signal.side == SignalSide.SELL
    assert signal.score == -0.8
    assert signal.confidence == 0.8


def test_engine_evaluates_many_snapshots() -> None:
    snapshots = [
        make_snapshot(values={"rsi_14": "32", "macd_hist": "0.12"}),
        make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}),
    ]

    signals = StrategyEngine().evaluate_many(snapshots)

    assert [signal.side for signal in signals] == [SignalSide.BUY, SignalSide.HOLD]


def make_snapshot(
    values: dict[str, str] | None = None,
    signals: dict[str, str] | None = None,
    health: DataHealth | None = None,
) -> MarketSnapshot:
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
            open_time=1_700_000_000_000,
            close_time=1_700_000_059_999,
            values=values or {"rsi_14": "50", "macd_hist": "0"},
            signals=signals or {"data_quality": "ok"},
            updated_at=1_700_000_060_000,
        ),
        health=health
        or DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
            kline_status="ok",
            indicator_status="ok",
        ),
    )
