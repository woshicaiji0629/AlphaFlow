import asyncio
import logging

import pytest

from alphaflow.strategy import (
    ClosedPosition,
    DataHealth,
    ExitReasonType,
    ExitRule,
    IndicatorSnapshot,
    Kline,
    MarketSnapshot,
    PositionSide,
    PositionState,
    SignalSide,
    StrategyEngine,
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
    snapshot = make_snapshot(values={"rsi_14": "32", "macd_hist": "0.1"})
    logger = logging.getLogger("test-strategy-runner")
    runner = StrategyRunner(FakeReader(snapshot), StrategyEngine(), logger=logger)

    with caplog.at_level(logging.INFO, logger="test-strategy-runner"):
        decisions = await runner.run_once(
            [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
        )

    assert decisions[0].results[0].signal.side == SignalSide.BUY
    fields = caplog.records[0].__dict__
    assert fields["side"] == "buy"
    assert fields["symbol"] == "ETHUSDT"
    assert fields["strategy"] == "rule"
    assert fields["position_action"] == "open_long"
    assert fields["analysis"]


def test_strategy_runner_opens_position_with_reason() -> None:
    asyncio.run(run_strategy_runner_opens_position_with_reason())


async def run_strategy_runner_opens_position_with_reason() -> None:
    snapshot = make_snapshot(values={"rsi_14": "32", "macd_hist": "0.1"})
    position_store = FakePositionStore()
    runner = StrategyRunner(
        FakeReader(snapshot),
        StrategyEngine(),
        position_store=position_store,
        logger=logging.getLogger("test-open-position"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
    )

    assert position_store.saved
    assert position_store.saved[0].strategy_name == "rule"
    assert position_store.saved[0].side == PositionSide.LONG
    assert [rule.trigger_price for rule in position_store.saved[0].exit_rules] == ["110", "95"]
    assert "buy signal opens long exposure" in position_store.saved[0].entry_reason


def test_strategy_runner_closes_position_to_history_before_clearing() -> None:
    asyncio.run(run_strategy_runner_closes_position_to_history_before_clearing())


async def run_strategy_runner_closes_position_to_history_before_clearing() -> None:
    snapshot = make_snapshot(values={"rsi_14": "70", "macd_hist": "-0.08"}, close="105")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
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
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
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
    snapshot = make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="111")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
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
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
    )

    assert len(history_store.saved) == 1
    assert history_store.saved[0].exit_reason_type == ExitReasonType.TAKE_PROFIT
    assert history_store.saved[0].exit_reason == "take profit target"
    assert history_store.saved[0].triggered_exit_rule is not None
    assert history_store.saved[0].triggered_exit_rule.trigger_price == "110"


def test_strategy_runner_partially_closes_position() -> None:
    asyncio.run(run_strategy_runner_partially_closes_position())


async def run_strategy_runner_partially_closes_position() -> None:
    snapshot = make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="111")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
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
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
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
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
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
    first_snapshot = make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="120")
    active = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="rule",
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
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
    )

    assert position_store.position is not None
    assert position_store.position.highest_price == "120"
    assert position_store.position.exit_rules[0].metadata["reference_price"] == "120"

    runner = StrategyRunner(
        FakeReader(make_snapshot(values={"rsi_14": "50", "macd_hist": "0"}, close="107")),
        StrategyEngine(),
        position_store=position_store,
        history_store=history_store,
        logger=logging.getLogger("test-trailing-stop"),
    )

    await runner.run_once(
        [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
    )

    assert history_store.saved[-1].exit_reason_type == ExitReasonType.TRAILING_STOP
    assert position_store.position is None


def make_snapshot(values: dict[str, str], close: str = "101") -> MarketSnapshot:
    merged_values = {
        "support_1": "95",
        "resistance_1": "110",
    }
    merged_values.update(values)
    return MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
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
    )
