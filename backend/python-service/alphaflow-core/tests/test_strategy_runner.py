import asyncio
import logging

import pytest

from alphaflow.strategy import (
    ClosedPosition,
    DataHealth,
    ExitReasonType,
    ExitRule,
    IndicatorSeriesAnalysis,
    IndicatorSnapshot,
    IndicatorWindowAnalysis,
    Kline,
    MarketSnapshot,
    PositionSide,
    PositionState,
    SignalSeriesAnalysis,
    SignalSide,
    StrategyEngine,
    WindowAnalysis,
)
from alphaflow.strategy.runner import StrategyRunner, StrategyTarget


class FakeReader:
    def __init__(self, snapshot: MarketSnapshot) -> None:
        self.snapshot = snapshot

    async def read_snapshot(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> MarketSnapshot:
        return self.snapshot


class FakePositionStore:
    def __init__(self, position: PositionState | None = None) -> None:
        self.position = position
        self.saved: list[PositionState] = []
        self.cleared: list[PositionState] = []

    async def get_active_position(
        self,
        exchange: str,
        market: str,
        symbol: str,
        strategy_name: str,
    ) -> PositionState | None:
        if self.position is None:
            return None
        if (
            self.position.exchange == exchange
            and self.position.market == market
            and self.position.symbol == symbol
            and self.position.strategy_name == strategy_name
        ):
            return self.position
        return None

    async def save_active_position(self, position: PositionState) -> None:
        self.position = position
        self.saved.append(position)

    async def clear_active_position(self, position: PositionState) -> None:
        self.position = None
        self.cleared.append(position)


class FakeHistoryStore:
    def __init__(self) -> None:
        self.initialized = False
        self.saved: list[ClosedPosition] = []

    async def initialize(self) -> None:
        self.initialized = True

    async def save_closed_position(self, position: ClosedPosition) -> None:
        self.saved.append(position)

    async def list_closed_positions(self, position_id: str) -> list[ClosedPosition]:
        return [position for position in self.saved if position.position_id == position_id]


def test_strategy_runner_logs_signal(caplog: pytest.LogCaptureFixture) -> None:
    asyncio.run(run_strategy_runner_logs_signal(caplog))


async def run_strategy_runner_logs_signal(caplog: pytest.LogCaptureFixture) -> None:
    snapshot = make_snapshot(side=SignalSide.BUY)
    logger = logging.getLogger("test-strategy-runner")
    runner = StrategyRunner(FakeReader(snapshot), StrategyEngine(), logger=logger)

    with caplog.at_level(logging.INFO, logger="test-strategy-runner"):
        decisions = await runner.run_once(
            [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
        )

    assert decisions[0].results[0].signal.side == SignalSide.BUY
    fields = caplog.records[0].__dict__
    assert fields["side"] == "buy"
    assert fields["symbol"] == "ETHUSDT"
    assert fields["strategy"] == "supertrend"
    assert fields["position_action"] == "open_long"
    assert fields["analysis"]


def test_strategy_runner_opens_position_with_reason() -> None:
    asyncio.run(run_strategy_runner_opens_position_with_reason())


async def run_strategy_runner_opens_position_with_reason() -> None:
    snapshot = make_snapshot(side=SignalSide.BUY)
    position_store = FakePositionStore()
    runner = StrategyRunner(
        FakeReader(snapshot),
        StrategyEngine(),
        position_store=position_store,
        logger=logging.getLogger("test-open-position"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert position_store.saved
    assert position_store.saved[0].strategy_name == "supertrend"
    assert position_store.saved[0].side == PositionSide.LONG
    assert [rule.trigger_price for rule in position_store.saved[0].exit_rules] == ["110", "96"]
    assert "buy signal opens long exposure" in position_store.saved[0].entry_reason


def test_strategy_runner_closes_position_to_history_before_clearing() -> None:
    asyncio.run(run_strategy_runner_closes_position_to_history_before_clearing())


async def run_strategy_runner_closes_position_to_history_before_clearing() -> None:
    snapshot = make_snapshot(side=SignalSide.SELL, close="105")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        position_id="position-1",
        side=PositionSide.LONG,
        size=1.0,
        initial_size=1.0,
        entry_price="100",
        exit_rules=(
            ExitRule(ExitReasonType.TAKE_PROFIT, "take profit target", trigger_price="110"),
            ExitRule(ExitReasonType.STOP_LOSS, "stop loss guard", trigger_price="95"),
        ),
        entry_time=1000,
        entry_reason="previous buy",
        updated_at=1000,
    )
    position_store = FakePositionStore(active)
    history_store = FakeHistoryStore()
    runner = StrategyRunner(
        FakeReader(snapshot),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-close-position"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert history_store.initialized
    assert len(history_store.saved) == 1
    assert history_store.saved[0].entry_reason == "previous buy"
    assert "conflicts with long position" in history_store.saved[0].exit_reason
    assert history_store.saved[0].exit_reason_type == ExitReasonType.STRATEGY
    assert [rule.trigger_price for rule in history_store.saved[0].exit_rules] == ["110", "95"]
    assert history_store.saved[0].realized_pnl == 500.0
    assert history_store.saved[0].fee == 12.299999999999999
    assert history_store.saved[0].net_pnl == 487.7
    assert history_store.saved[0].net_pnl_pct == 487.7
    assert history_store.saved[0].is_final_exit
    assert history_store.saved[0].total_net_pnl == 487.7
    assert len(position_store.cleared) == 1
    assert position_store.cleared[0].strategy_name == active.strategy_name
    assert position_store.cleared[0].size == active.size


def test_strategy_runner_closes_position_on_take_profit() -> None:
    asyncio.run(run_strategy_runner_closes_position_on_take_profit())


async def run_strategy_runner_closes_position_on_take_profit() -> None:
    snapshot = make_snapshot(side=SignalSide.BUY, close="111")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        position_id="position-2",
        side=PositionSide.LONG,
        size=1.0,
        initial_size=1.0,
        entry_price="100",
        exit_rules=(
            ExitRule(ExitReasonType.TAKE_PROFIT, "take profit target", trigger_price="110"),
            ExitRule(ExitReasonType.STOP_LOSS, "stop loss guard", trigger_price="95"),
        ),
        entry_time=1000,
        entry_reason="previous buy",
        updated_at=1000,
    )
    position_store = FakePositionStore(active)
    history_store = FakeHistoryStore()
    runner = StrategyRunner(
        FakeReader(snapshot),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-take-profit"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert len(history_store.saved) == 1
    assert history_store.saved[0].exit_reason_type == ExitReasonType.TAKE_PROFIT
    assert history_store.saved[0].exit_reason == "take profit target"
    assert history_store.saved[0].triggered_exit_rule is not None
    assert history_store.saved[0].triggered_exit_rule.trigger_price == "110"


def test_strategy_runner_partially_closes_position() -> None:
    asyncio.run(run_strategy_runner_partially_closes_position())


async def run_strategy_runner_partially_closes_position() -> None:
    snapshot = make_snapshot(side=SignalSide.BUY, close="111")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        position_id="position-3",
        side=PositionSide.LONG,
        size=2.0,
        initial_size=2.0,
        entry_price="100",
        exit_rules=(
            ExitRule(
                ExitReasonType.TAKE_PROFIT,
                "partial take profit",
                trigger_price="110",
                size_pct=0.5,
            ),
            ExitRule(
                ExitReasonType.TAKE_PROFIT,
                "final take profit",
                trigger_price="110",
                size_pct=1.0,
            ),
        ),
        entry_time=1000,
        entry_reason="previous buy",
        updated_at=1000,
    )
    position_store = FakePositionStore(active)
    history_store = FakeHistoryStore()
    runner = StrategyRunner(
        FakeReader(snapshot),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-partial-close"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert history_store.saved[0].size == 1.0
    assert history_store.saved[0].margin == 50.0
    assert history_store.saved[0].realized_pnl == 550.0
    assert history_store.saved[0].fee == 6.329999999999999
    assert history_store.saved[0].net_pnl == 543.67
    assert not history_store.saved[0].is_final_exit
    assert history_store.saved[0].remaining_size_after_exit == 1.0
    assert position_store.position is not None
    assert position_store.position.size == 1.0

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert history_store.saved[-1].is_final_exit
    assert history_store.saved[-1].total_realized_pnl == 1100.0
    assert history_store.saved[-1].total_fee == 12.659999999999998
    assert history_store.saved[-1].total_net_pnl == 1087.34
    assert history_store.saved[-1].total_net_pnl_pct == 1087.34
    assert position_store.position is None


def test_strategy_runner_trailing_stop_uses_updated_high() -> None:
    asyncio.run(run_strategy_runner_trailing_stop_uses_updated_high())


async def run_strategy_runner_trailing_stop_uses_updated_high() -> None:
    first_snapshot = make_snapshot(side=SignalSide.BUY, close="120")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="supertrend",
        position_id="position-4",
        side=PositionSide.LONG,
        size=1.0,
        initial_size=1.0,
        entry_price="100",
        highest_price="100",
        lowest_price="100",
        exit_rules=(
            ExitRule(
                ExitReasonType.TRAILING_STOP,
                "trailing stop",
                size_pct=1.0,
                metadata={"trail_pct": "10", "reference_price": "100"},
            ),
        ),
        entry_time=1000,
        entry_reason="previous buy",
        updated_at=1000,
    )
    position_store = FakePositionStore(active)
    history_store = FakeHistoryStore()
    runner = StrategyRunner(
        FakeReader(first_snapshot),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-trailing-stop"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert position_store.position is not None
    assert position_store.position.highest_price == "120"
    assert position_store.position.exit_rules[0].metadata["reference_price"] == "120"

    runner = StrategyRunner(
        FakeReader(make_snapshot(side=SignalSide.BUY, close="107")),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-trailing-stop"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")]
    )

    assert history_store.saved[-1].exit_reason_type == ExitReasonType.TRAILING_STOP
    assert position_store.position is None


def make_snapshot(side: SignalSide, close: str = "101") -> MarketSnapshot:
    bullish = side == SignalSide.BUY
    merged_values = {
        "support_1": "95",
        "resistance_1": "110",
        "supertrend": "96" if bullish else "104",
    }
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="3m",
            open_time=1000,
            close_time=1999,
            values=merged_values,
            signals={"data_quality": "ok"},
            updated_at=2000,
        ),
        health=DataHealth(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="3m",
            kline_status="ok",
            indicator_status="ok",
        ),
        klines=(
            Kline(
                exchange="binance",
                market="um",
                symbol="ETHUSDT",
                interval="3m",
                open_time=1000,
                close_time=1999,
                open="100",
                high="106",
                low="99",
                close=close,
                volume="10",
                is_closed=True,
            ),
        ),
        indicator_window=make_indicator_window(side),
        window=WindowAnalysis(
            sample_count=200,
            lookback=200,
            trend="up" if bullish else "down",
            volume_state="expanding",
        ),
    )


def make_indicator_window(side: SignalSide) -> IndicatorWindowAnalysis:
    bullish = side == SignalSide.BUY
    direction = "up" if bullish else "down"
    bias = "bull" if bullish else "bear"
    action = "pump" if bullish else "dump"
    opposite_action = "dump" if bullish else "pump"
    return IndicatorWindowAnalysis(
        sample_count=200,
        values={
            f"{action}_window_score": IndicatorSeriesAnalysis(latest=82),
            f"{opposite_action}_window_score": IndicatorSeriesAnalysis(latest=8),
        },
        signals={
            "data_quality": SignalSeriesAnalysis(latest="ok", previous="ok", stable_count=20),
            "pump_window_signal": SignalSeriesAnalysis(latest="true" if bullish else "false"),
            "dump_window_signal": SignalSeriesAnalysis(latest="false" if bullish else "true"),
            "pump_window_fake_risk": SignalSeriesAnalysis(latest="low"),
            "dump_window_fake_risk": SignalSeriesAnalysis(latest="low"),
            "pump_window_quality": SignalSeriesAnalysis(latest="strong" if bullish else "weak"),
            "dump_window_quality": SignalSeriesAnalysis(latest="weak" if bullish else "strong"),
            "trend_valid": SignalSeriesAnalysis(latest="true"),
            "trend_window_bias": SignalSeriesAnalysis(latest=bias),
            "trend_price_progress": SignalSeriesAnalysis(
                latest="advancing" if bullish else "declining"
            ),
            "trend_quality": SignalSeriesAnalysis(latest="strong"),
            "supertrend_direction": SignalSeriesAnalysis(
                latest=direction,
                previous=direction,
                stable_count=3,
            ),
            "alphatrend_direction": SignalSeriesAnalysis(latest=direction, stable_count=3),
            "ma_window_bias": SignalSeriesAnalysis(latest=bias),
            "ma_ribbon_state": SignalSeriesAnalysis(
                latest="bullish_fan" if bullish else "bearish_fan"
            ),
            "ma_ribbon_phase": SignalSeriesAnalysis(latest="early_expand"),
            "ema_alignment": SignalSeriesAnalysis(
                latest=bias,
                previous=bias,
                stable_count=3,
            ),
            "macd_window_bias": SignalSeriesAnalysis(latest=bias),
            "macd_window_quality": SignalSeriesAnalysis(latest="strong"),
            "macd_momentum": SignalSeriesAnalysis(
                latest="expanding_bull" if bullish else "expanding_bear",
                previous="expanding_bull" if bullish else "expanding_bear",
                stable_count=3,
            ),
            "macd_divergence": SignalSeriesAnalysis(latest="none", previous="none", stable_count=5),
            "price_volume_confirmation": SignalSeriesAnalysis(
                latest="confirm_up" if bullish else "confirm_down"
            ),
            "volume_window_state": SignalSeriesAnalysis(latest="spike"),
        },
    )
