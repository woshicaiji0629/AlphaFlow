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


def test_supertrend_emits_buy_signal_from_windows() -> None:
    decision = evaluate_snapshot(make_snapshot(SignalSide.BUY))
    result = decision.results[0]

    assert [item.strategy_name for item in decision.results] == ["supertrend"]
    assert result.signal.side == SignalSide.BUY
    assert result.signal.score >= 0.72
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_LONG
    assert decision.position_plan.target_size == 1.0
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["110", "96"]


def test_supertrend_emits_sell_signal_from_windows() -> None:
    decision = evaluate_snapshot(make_snapshot(SignalSide.SELL))
    result = decision.results[0]

    assert result.signal.side == SignalSide.SELL
    assert result.signal.score >= 0.72
    assert decision.position_plan is not None
    assert decision.position_plan.action == PositionAction.OPEN_SHORT
    assert decision.position_plan.target_size == 1.0
    assert [rule.trigger_price for rule in decision.position_plan.exit_rules] == ["95", "104"]


def test_supertrend_blocks_tangled_ema() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            values={
                "ema7": IndicatorSeriesAnalysis(latest=100.01, previous=100, direction="rising"),
                "ema25": IndicatorSeriesAnalysis(latest=100, previous=99.99, direction="rising"),
            },
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "ema7/ema25 tangled" in signal.reason


def test_supertrend_blocks_opposite_fast_macd() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            values={
                "macd_fast_hist": IndicatorSeriesAnalysis(
                    latest=-0.03,
                    previous=-0.01,
                    direction="falling",
                )
            },
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "fast macd histogram does not follow" in signal.reason


def test_supertrend_blocks_price_volume_divergence() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={
                "price_volume_confirmation": SignalSeriesAnalysis(
                    latest="divergence_bear",
                    previous="neutral",
                )
            },
        ),
    )

    signal = evaluate_snapshot(snapshot).results[0].signal

    assert signal.side == SignalSide.HOLD
    assert "price-volume blocks: divergence_bear" in signal.reason


def test_supertrend_blocks_when_5m_and_10m_block() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        timeframe_windows={
            "5m": make_timeframe_window(SignalSide.SELL),
            "10m": make_timeframe_window(SignalSide.SELL),
            "15m": make_timeframe_window(SignalSide.BUY),
            "30m": make_timeframe_window(SignalSide.BUY),
        },
    )

    signal = (
        evaluate_snapshot(
            snapshot,
            timeframe_windows=snapshot.timeframe_windows,
        )
        .results[0]
        .signal
    )

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

    decision = evaluate_snapshot(
        snapshot,
        position,
        timeframe_windows={
            "5m": make_timeframe_window(SignalSide.BUY),
            "10m": make_timeframe_window(SignalSide.BUY),
            "15m": make_timeframe_window(SignalSide.BUY),
            "30m": make_timeframe_window(SignalSide.BUY),
        },
    )

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.CLOSE_LONG
    assert "conflicts with long position" in decision.results[0].position_plan.reason


def test_engine_defers_long_exit_when_supertrend_turns_without_confirmation() -> None:
    snapshot = make_snapshot(
        SignalSide.BUY,
        indicator_window=make_indicator_window(
            SignalSide.BUY,
            signals={
                "supertrend_direction": SignalSeriesAnalysis(
                    latest="down",
                    previous="up",
                    changed=True,
                    stable_count=1,
                )
            },
        ),
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

    decision = evaluate_snapshot(
        snapshot,
        position,
        timeframe_windows={
            "5m": make_timeframe_window(SignalSide.BUY),
            "10m": make_timeframe_window(SignalSide.BUY),
            "15m": make_timeframe_window(SignalSide.BUY),
            "30m": make_timeframe_window(SignalSide.BUY),
        },
    )

    assert decision.results[0].position_plan is not None
    assert decision.results[0].position_plan.action == PositionAction.HOLD
    assert "long exit deferred" in decision.results[0].signal.reason


def test_engine_closes_long_when_supertrend_and_ema_macd_confirm_exit() -> None:
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
    assert "confirmed down exit" in decision.results[0].position_plan.reason


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
    if timeframe_windows is None:
        for interval in ("5m", "10m", "15m", "30m"):
            snapshots[interval] = make_snapshot(side, interval=interval)
    else:
        for interval, window in timeframe_windows.items():
            snapshots[interval] = snapshot_from_timeframe_window(window)
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
    signal_series = snapshot.indicator_window.signals.get("supertrend_direction")
    if signal_series is not None and signal_series.latest == "down":
        return SignalSide.SELL
    return SignalSide.BUY


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
) -> MarketSnapshot:
    supertrend = "96" if side == SignalSide.BUY else "104"
    merged_values = {
        "support_1": "95",
        "resistance_1": "110",
        "supertrend": supertrend,
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
            "5m": make_timeframe_window(side),
            "10m": make_timeframe_window(side),
            "15m": make_timeframe_window(side),
            "30m": make_timeframe_window(side),
        },
        window=WindowAnalysis(
            sample_count=200,
            lookback=200,
            trend="up" if side == SignalSide.BUY else "down",
            volume_state="expanding",
        ),
    )


def make_timeframe_window(side: SignalSide, interval: str = "5m") -> TimeframeWindow:
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
        window=WindowAnalysis(sample_count=200, lookback=200, trend="up", volume_state="normal"),
    )


def make_indicator_window(
    side: SignalSide,
    values: dict[str, IndicatorSeriesAnalysis] | None = None,
    signals: dict[str, SignalSeriesAnalysis] | None = None,
) -> IndicatorWindowAnalysis:
    bullish = side == SignalSide.BUY
    direction = "rising" if bullish else "falling"
    merged_values = {
        "ema7": IndicatorSeriesAnalysis(
            latest=105 if bullish else 95,
            previous=104 if bullish else 96,
            direction=direction,
        ),
        "ema25": IndicatorSeriesAnalysis(
            latest=100,
            previous=99 if bullish else 101,
            direction=direction,
        ),
        "ema99": IndicatorSeriesAnalysis(
            latest=90 if bullish else 110,
            previous=89 if bullish else 111,
            direction=direction,
        ),
        "ema25_slope5_pct": IndicatorSeriesAnalysis(
            latest=0.4 if bullish else -0.4,
            previous=0.2 if bullish else -0.2,
            direction=direction,
        ),
        "macd_hist": IndicatorSeriesAnalysis(
            latest=0.12 if bullish else -0.12,
            previous=0.04 if bullish else -0.04,
            direction=direction,
            rising_count=3 if bullish else 0,
            falling_count=0 if bullish else 3,
        ),
        "macd_hist_delta": IndicatorSeriesAnalysis(
            latest=0.08 if bullish else -0.08,
            previous=0.02 if bullish else -0.02,
            direction=direction,
        ),
        "macd_fast_hist": IndicatorSeriesAnalysis(
            latest=0.16 if bullish else -0.16,
            previous=0.06 if bullish else -0.06,
            direction=direction,
        ),
        "macd_fast_hist_delta": IndicatorSeriesAnalysis(
            latest=0.1 if bullish else -0.1,
            previous=0.03 if bullish else -0.03,
            direction=direction,
        ),
        "adx14": IndicatorSeriesAnalysis(latest=24, previous=18, direction="rising"),
        "rsi14": IndicatorSeriesAnalysis(
            latest=58 if bullish else 42,
            previous=52 if bullish else 48,
            direction=direction,
        ),
        "obv_slope5": IndicatorSeriesAnalysis(
            latest=8 if bullish else -8,
            previous=3 if bullish else -3,
            direction=direction,
        ),
    }
    if values is not None:
        merged_values.update(values)

    merged_signals = {
        "data_quality": SignalSeriesAnalysis(latest="ok", previous="ok", stable_count=20),
        "supertrend_direction": SignalSeriesAnalysis(
            latest="up" if bullish else "down",
            previous="up" if bullish else "down",
            stable_count=3,
        ),
        "ema_alignment": SignalSeriesAnalysis(
            latest="bull" if bullish else "bear",
            previous="bull" if bullish else "bear",
            stable_count=3,
        ),
        "macd_momentum": SignalSeriesAnalysis(
            latest="expanding_bull" if bullish else "expanding_bear",
            previous="expanding_bull" if bullish else "expanding_bear",
            stable_count=3,
        ),
        "macd_divergence": SignalSeriesAnalysis(latest="none", previous="none", stable_count=10),
        "macd_fast_momentum": SignalSeriesAnalysis(
            latest="expanding_bull" if bullish else "expanding_bear",
            previous="expanding_bull" if bullish else "expanding_bear",
            stable_count=3,
        ),
        "macd_fast_divergence": SignalSeriesAnalysis(
            latest="none",
            previous="none",
            stable_count=10,
        ),
        "di_direction": SignalSeriesAnalysis(
            latest="bull" if bullish else "bear",
            previous="bull" if bullish else "bear",
            stable_count=3,
        ),
        "price_volume_confirmation": SignalSeriesAnalysis(
            latest="confirm_up" if bullish else "confirm_down",
            previous="confirm_up" if bullish else "confirm_down",
            stable_count=3,
        ),
        "volume_state": SignalSeriesAnalysis(latest="spike", previous="normal", stable_count=1),
    }
    if signals is not None:
        merged_signals.update(signals)
    return IndicatorWindowAnalysis(
        sample_count=200,
        values=merged_values,
        signals=merged_signals,
    )
