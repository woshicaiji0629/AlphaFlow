from collections.abc import Mapping

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
    SignalSeriesAnalysis,
    SignalSide,
    SnapshotFreshness,
    StrategyContext,
    StrategyEngine,
    StrategyTarget,
    TimeframeWindow,
    WindowAnalysis,
)


def test_supertrend_holds_when_health_is_not_ok() -> None:
    snapshot = make_snapshot(
        health=DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="3m",
            kline_status="stale",
            indicator_status="ok",
            reason="kline stale",
        )
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "health not ok: kline stale"


def test_supertrend_holds_when_indicator_quality_is_not_ok() -> None:
    snapshot = make_snapshot(
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={"data_quality": SignalSeriesAnalysis(latest="gap")},
        )
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "indicator quality not ok: gap"


def test_supertrend_holds_when_feature_hash_is_stale() -> None:
    snapshot = make_snapshot(
        freshness=SnapshotFreshness(
            valid=False,
            reason="realtime hash stale",
            window_bar_seq=10,
            realtime_bar_seq=11,
            expected_window_bar_seq=10,
            expected_realtime_bar_seq=11,
            window_updated_at=1_700_000_180_000,
            realtime_updated_at=1_700_000_100_000,
        )
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert signal.reason == "feature freshness not ok: realtime hash stale"


def test_supertrend_emits_buy_signal_from_semantic_windows() -> None:
    decision = evaluate_snapshot(make_snapshot(SignalSide.BUY))
    result = decision.results[0]

    assert result.signal.side == SignalSide.BUY
    assert result.signal.score >= 0.72
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_LONG
    assert decision.position_plan.target_size == 1.0
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["110", "96"]


def test_supertrend_emits_sell_signal_from_semantic_windows() -> None:
    decision = evaluate_snapshot(make_snapshot(SignalSide.SELL))
    result = decision.results[0]

    assert result.signal.side == SignalSide.SELL
    assert result.signal.score >= 0.72
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_SHORT
    assert decision.position_plan.target_size == 1.0
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["95", "104"]


def test_supertrend_blocks_fake_pump_risk() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={"pump_window_fake_risk": SignalSeriesAnalysis(latest="high")},
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "pump fake risk: high" in signal.reason


def test_supertrend_blocks_ribbon_without_direction() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={
                "ma_ribbon_state": SignalSeriesAnalysis(latest="tangled"),
                "ma_ribbon_phase": SignalSeriesAnalysis(latest="range"),
            },
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "ma ribbon has no direction" in signal.reason


def test_supertrend_blocks_opposite_macd_bias() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={"macd_window_bias": SignalSeriesAnalysis(latest="bear")},
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "macd blocks: bear" in signal.reason


def test_supertrend_blocks_when_5m_and_10m_block() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        timeframe_windows={
            "5m": make_timeframe_window(SignalSide.SELL, "5m"),
            "10m": make_timeframe_window(SignalSide.SELL, "10m"),
            "15m": make_timeframe_window(SignalSide.BUY, "15m"),
            "30m": make_timeframe_window(SignalSide.BUY, "30m"),
        },
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "5m and 10m both blocking" in signal.reason


def test_engine_closes_existing_long_before_new_short() -> None:
    snapshot = make_snapshot(SignalSide.SELL)
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        side=PositionSide.LONG,
        size=1.0,
        entry_price="100",
        entry_time=1_700_000_000_000,
        entry_reason="previous buy",
    )

    decision = evaluate_snapshot(snapshot, position)

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_LONG
    assert "conflicts with long position" in decision.results[0].position_plan.reason


def test_engine_defers_long_exit_when_bearish_window_is_weak() -> None:
    snapshot = make_snapshot(
        SignalSide.SELL,
        indicator_window=make_indicator_window(
            SignalSide.SELL,
            signals={
                "ma_ribbon_state": SignalSeriesAnalysis(latest="tangled"),
                "supertrend_direction": SignalSeriesAnalysis(latest="down", stable_count=1),
            },
        ),
        timeframe_windows={
            "5m": make_timeframe_window(SignalSide.BUY, "5m"),
            "10m": make_timeframe_window(SignalSide.BUY, "10m"),
            "15m": make_timeframe_window(SignalSide.BUY, "15m"),
            "30m": make_timeframe_window(SignalSide.BUY, "30m"),
        },
    )
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        side=PositionSide.LONG,
        size=1.0,
        entry_price="100",
        entry_time=1_700_000_000_000,
        entry_reason="previous buy",
    )

    decision = evaluate_snapshot(snapshot, position)

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.HOLD
    assert "long exit deferred" in decision.results[0].signal.reason


def test_engine_closes_long_on_take_profit_before_strategy_signal() -> None:
    snapshot = make_snapshot(SignalSide.BUY, close="111")
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        side=PositionSide.LONG,
        size=1.0,
        entry_price="100",
        exit_rules=(
            ExitRule(ExitReasonType.TAKE_PROFIT, "take profit target", trigger_price="110"),
            ExitRule(ExitReasonType.STOP_LOSS, "stop loss guard", trigger_price="95"),
        ),
        entry_time=1_700_000_000_000,
        entry_reason="previous buy",
    )

    decision = evaluate_snapshot(snapshot, position)

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_LONG
    assert decision.results[0].position_plan.exit_reason_type == ExitReasonType.TAKE_PROFIT


def evaluate_snapshot(
    snapshot: MarketSnapshot,
    position: PositionState | None = None,
    timeframe_windows: Mapping[str, TimeframeWindow] | None = None,
):
    snapshots = {snapshot.indicator.interval: snapshot}
    side = side_from_snapshot(snapshot)
    windows = timeframe_windows if timeframe_windows is not None else snapshot.timeframe_windows
    for interval, window in windows.items():
        snapshots[interval] = snapshot_from_timeframe_window(window)
    if not windows:
        for interval in ("5m", "10m", "15m", "30m"):
            snapshots[interval] = make_snapshot(side, interval=interval)
    return StrategyEngine().evaluate(
        [
            StrategyContext(
                strategy_name="supertrend",
                target=StrategyTarget(
                    exchange=snapshot.indicator.exchange,
                    market=snapshot.indicator.market,
                    symbol=snapshot.indicator.symbol,
                    interval="3m",
                ),
                snapshots=snapshots,
                position=position,
            )
        ]
    )


def side_from_snapshot(snapshot: MarketSnapshot) -> SignalSide:
    if snapshot.indicator_window is None:
        return SignalSide.BUY
    return (
        SignalSide.SELL
        if snapshot.indicator_window.signals.get("dump_window_signal", SignalSeriesAnalysis()).latest
        == "true"
        else SignalSide.BUY
    )


def snapshot_from_timeframe_window(window: TimeframeWindow) -> MarketSnapshot:
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange=window.health.exchange,
            market=window.health.market,
            symbol=window.health.symbol,
            interval=window.interval,
            open_time=1_700_000_000_000,
            close_time=1_700_000_179_999,
            values={},
            signals={"data_quality": "ok"},
            updated_at=1_700_000_180_000,
        ),
        health=window.health,
        indicator_window=window.indicator_window,
        window=window.window,
    )


def make_snapshot(
    side: SignalSide = SignalSide.BUY,
    interval: str = "3m",
    values: dict[str, str] | None = None,
    health: DataHealth | None = None,
    close: str = "100",
    indicator_window: IndicatorWindowAnalysis | None = None,
    timeframe_windows: dict[str, TimeframeWindow] | None = None,
    freshness: SnapshotFreshness | None = None,
) -> MarketSnapshot:
    bullish = side == SignalSide.BUY
    merged_values = {
        "support_1": "95",
        "resistance_1": "110",
        "supertrend": "96" if bullish else "104",
    }
    if values is not None:
        merged_values.update(values)
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval=interval,
            open_time=1_700_000_000_000,
            close_time=1_700_000_179_999,
            values=merged_values,
            signals={"data_quality": "ok"},
            updated_at=1_700_000_180_000,
        ),
        health=health
        or DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval=interval,
            kline_status="ok",
            indicator_status="ok",
        ),
        klines=(
            Kline(
                exchange="binance",
                market="um",
                symbol="ETHUSDT",
                interval=interval,
                open_time=1_700_000_000_000,
                close_time=1_700_000_179_999,
                open="100",
                high="112",
                low="94",
                close=close,
                volume="10",
                is_closed=True,
            ),
        ),
        indicator_window=indicator_window or make_indicator_window(side),
        timeframe_windows=timeframe_windows
        or {
            "5m": make_timeframe_window(side, "5m"),
            "10m": make_timeframe_window(side, "10m"),
            "15m": make_timeframe_window(side, "15m"),
            "30m": make_timeframe_window(side, "30m"),
        },
        window=WindowAnalysis(
            sample_count=200,
            lookback=200,
            trend="up" if bullish else "down",
            volume_state="expanding",
        ),
        freshness=freshness,
    )


def make_timeframe_window(side: SignalSide, interval: str) -> TimeframeWindow:
    bullish = side == SignalSide.BUY
    return TimeframeWindow(
        interval=interval,
        health=DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval=interval,
            kline_status="ok",
            indicator_status="ok",
        ),
        indicator_window=make_indicator_window(side),
        window=WindowAnalysis(
            sample_count=200,
            lookback=200,
            trend="up" if bullish else "down",
            volume_state="normal",
        ),
    )


def make_indicator_window(
    side: SignalSide,
    values: dict[str, IndicatorSeriesAnalysis] | None = None,
    signals: dict[str, SignalSeriesAnalysis] | None = None,
) -> IndicatorWindowAnalysis:
    bullish = side == SignalSide.BUY
    direction = "up" if bullish else "down"
    bias = "bull" if bullish else "bear"
    opposite_action = "dump" if bullish else "pump"
    action = "pump" if bullish else "dump"
    merged_values = {
        f"{action}_window_score": IndicatorSeriesAnalysis(latest=82),
        f"{opposite_action}_window_score": IndicatorSeriesAnalysis(latest=8),
    }
    if values is not None:
        merged_values.update(values)

    merged_signals = {
        "data_quality": SignalSeriesAnalysis(latest="ok", previous="ok", stable_count=20),
        "pump_window_signal": SignalSeriesAnalysis(latest="true" if bullish else "false"),
        "dump_window_signal": SignalSeriesAnalysis(latest="false" if bullish else "true"),
        "pump_window_fake_risk": SignalSeriesAnalysis(latest="low"),
        "dump_window_fake_risk": SignalSeriesAnalysis(latest="low"),
        "pump_window_quality": SignalSeriesAnalysis(latest="strong" if bullish else "weak"),
        "dump_window_quality": SignalSeriesAnalysis(latest="weak" if bullish else "strong"),
        "trend_valid": SignalSeriesAnalysis(latest="true"),
        "trend_window_bias": SignalSeriesAnalysis(latest=bias),
        "trend_price_progress": SignalSeriesAnalysis(latest="advancing" if bullish else "declining"),
        "trend_quality": SignalSeriesAnalysis(latest="strong"),
        "supertrend_direction": SignalSeriesAnalysis(latest=direction, stable_count=3),
        "alphatrend_direction": SignalSeriesAnalysis(latest=direction, stable_count=3),
        "ma_window_bias": SignalSeriesAnalysis(latest=bias),
        "ma_ribbon_state": SignalSeriesAnalysis(latest="bullish_fan" if bullish else "bearish_fan"),
        "ma_ribbon_phase": SignalSeriesAnalysis(latest="early_expand"),
        "ema_alignment": SignalSeriesAnalysis(latest=bias, stable_count=3),
        "macd_window_bias": SignalSeriesAnalysis(latest=bias),
        "macd_window_quality": SignalSeriesAnalysis(latest="strong"),
        "macd_momentum": SignalSeriesAnalysis(
            latest="expanding_bull" if bullish else "expanding_bear"
        ),
        "macd_divergence": SignalSeriesAnalysis(latest="none", stable_count=10),
        "price_volume_confirmation": SignalSeriesAnalysis(
            latest="confirm_up" if bullish else "confirm_down"
        ),
        "volume_window_state": SignalSeriesAnalysis(latest="spike"),
    }
    if signals is not None:
        merged_signals.update(signals)
    return IndicatorWindowAnalysis(
        sample_count=200,
        values=merged_values,
        signals=merged_signals,
    )
