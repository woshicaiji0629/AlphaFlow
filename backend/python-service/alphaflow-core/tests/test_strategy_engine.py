from alphaflow.strategy import (
    DataHealth,
    ExitReasonType,
    ExitRule,
    IndicatorSeriesAnalysis,
    IndicatorSnapshot,
    IndicatorWindowAnalysis,
    Kline,
    MarketSnapshot,
    PositionAction,
    PositionSide,
    PositionState,
    SignalSide,
    StrategyEngine,
    WindowAnalysis,
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

    signal = StrategyEngine().evaluate(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "health not ok: kline stale"


def test_strategy_holds_when_indicator_quality_is_not_ok() -> None:
    snapshot = make_snapshot(
        signals={"data_quality": "gap", "data_quality_reason": "missing kline"}
    )

    signal = StrategyEngine().evaluate(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "indicator data quality not ok: missing kline"


def test_strategy_holds_when_required_fields_are_missing() -> None:
    snapshot = make_snapshot(values={"rsi_14": "30", "macd_hist": ""})

    signal = StrategyEngine().evaluate(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "required indicators missing: rsi_14 or macd_hist"


def test_strategy_emits_buy_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "32", "macd_hist": "0.12"})

    decision = StrategyEngine().evaluate(snapshot)
    signal = decision.results[0].signal

    assert signal.side == SignalSide.BUY
    assert signal.score == 0.8
    assert signal.confidence == 0.8
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_LONG
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["110", "95"]


def test_strategy_emits_sell_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "70", "macd_hist": "-0.08"})

    decision = StrategyEngine().evaluate(snapshot)
    signal = decision.results[0].signal

    assert signal.side == SignalSide.SELL
    assert signal.score == -0.8
    assert signal.confidence == 0.8
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_SHORT
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["95", "110"]


def test_engine_evaluates_many_snapshots() -> None:
    snapshots = [
        make_snapshot(values={"rsi_14": "32", "macd_hist": "0.12"}),
        make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}),
    ]

    decisions = StrategyEngine().evaluate_many(snapshots)

    assert [decision.results[0].signal.side for decision in decisions] == [
        SignalSide.BUY,
        SignalSide.HOLD,
    ]


def test_engine_returns_multiple_strategy_results_with_analysis() -> None:
    snapshot = make_snapshot(
        values={"rsi_14": "58", "macd_hist": "0.2"},
        signals={"data_quality": "ok", "ema_alignment": "bullish", "ma_cross": "golden"},
    )

    decision = StrategyEngine().evaluate(snapshot)

    assert [result.strategy_name for result in decision.results] == ["rule", "trend_momentum"]
    assert decision.results[1].signal.side == SignalSide.BUY
    assert "current price 100" in decision.results[1].analysis.summary


def test_trend_momentum_uses_window_trend() -> None:
    snapshot = make_snapshot(
        values={"rsi_14": "58", "macd_hist": "0.2"},
        signals={"data_quality": "ok", "ma_cross": "golden"},
        window=WindowAnalysis(sample_count=200, lookback=200, trend="up", volume_state="normal"),
    )

    decision = StrategyEngine().evaluate(snapshot)

    assert decision.results[1].signal.side == SignalSide.BUY
    assert "window uptrend" in decision.results[1].signal.reason


def test_trend_momentum_uses_indicator_window_trend() -> None:
    snapshot = make_snapshot(
        values={"rsi_14": "58", "macd_hist": "0.2"},
        signals={"data_quality": "ok", "ma_cross": "golden"},
        indicator_window=IndicatorWindowAnalysis(
            sample_count=200,
            values={
                "macd_hist": IndicatorSeriesAnalysis(direction="rising"),
                "rsi_14": IndicatorSeriesAnalysis(direction="rising"),
            },
        ),
    )

    decision = StrategyEngine().evaluate(snapshot)

    assert decision.results[1].signal.side == SignalSide.BUY
    assert "macd histogram rising" in decision.results[1].signal.reason
    assert "rsi rising" in decision.results[1].signal.reason


def test_engine_closes_existing_long_before_new_short() -> None:
    snapshot = make_snapshot(values={"rsi_14": "70", "macd_hist": "-0.08"})
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
        side=PositionSide.LONG,
        size=0.8,
        entry_price="100",
        entry_time=1_700_000_000_000,
        entry_reason="previous buy",
    )

    decision = StrategyEngine().evaluate(snapshot, positions={"rule": position})

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_LONG
    assert "conflicts with long position" in decision.results[0].position_plan.reason


def test_engine_closes_long_on_take_profit_before_strategy_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="111")
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
        side=PositionSide.LONG,
        size=0.8,
        entry_price="100",
        exit_rules=(
            ExitRule(ExitReasonType.TAKE_PROFIT, "take profit target", trigger_price="110"),
            ExitRule(ExitReasonType.STOP_LOSS, "stop loss guard", trigger_price="95"),
        ),
        entry_time=1_700_000_000_000,
        entry_reason="previous buy",
    )

    decision = StrategyEngine().evaluate(snapshot, positions={"rule": position})

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_LONG
    assert decision.results[0].position_plan.exit_reason_type == ExitReasonType.TAKE_PROFIT


def test_engine_closes_short_on_stop_loss_before_strategy_signal() -> None:
    snapshot = make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="112")
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
        side=PositionSide.SHORT,
        size=0.8,
        entry_price="100",
        exit_rules=(
            ExitRule(ExitReasonType.TAKE_PROFIT, "take profit target", trigger_price="90"),
            ExitRule(ExitReasonType.STOP_LOSS, "stop loss guard", trigger_price="110"),
        ),
        entry_time=1_700_000_000_000,
        entry_reason="previous sell",
    )

    decision = StrategyEngine().evaluate(snapshot, positions={"rule": position})

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_SHORT
    assert decision.results[0].position_plan.exit_reason_type == ExitReasonType.STOP_LOSS


def make_snapshot(
    values: dict[str, str] | None = None,
    signals: dict[str, str] | None = None,
    health: DataHealth | None = None,
    close: str = "100",
    window: WindowAnalysis | None = None,
    indicator_window: IndicatorWindowAnalysis | None = None,
) -> MarketSnapshot:
    merged_values = {
        "rsi_14": "50",
        "macd_hist": "0",
        "support_1": "95",
        "resistance_1": "110",
    }
    if values is not None:
        merged_values.update(values)
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
            open_time=1_700_000_000_000,
            close_time=1_700_000_059_999,
            values=merged_values,
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
        klines=(
            Kline(
                exchange="binance",
                market="um",
                symbol="ETHUSDT",
                interval="1m",
                open_time=1_700_000_000_000,
                close_time=1_700_000_059_999,
                open="100",
                high="112",
                low="94",
                close=close,
                volume="10",
                is_closed=True,
            ),
        ),
        window=window,
        indicator_window=indicator_window,
    )
