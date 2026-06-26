import asyncio
import logging

import pytest

from alphaflow.strategy import (
    DataHealth,
    IndicatorSnapshot,
    MarketSnapshot,
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


def test_strategy_runner_logs_signal(caplog: pytest.LogCaptureFixture) -> None:
    asyncio.run(run_strategy_runner_logs_signal(caplog))


async def run_strategy_runner_logs_signal(caplog: pytest.LogCaptureFixture) -> None:
    snapshot = MarketSnapshot(
        indicator=IndicatorSnapshot(
            exchange="binance",
            market="um",
            symbol="ETHUSDT",
            interval="1m",
            open_time=1000,
            close_time=1999,
            values={"rsi_14": "32", "macd_hist": "0.1"},
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
    )
    logger = logging.getLogger("test-strategy-runner")
    runner = StrategyRunner(FakeReader(snapshot), StrategyEngine(), logger)

    with caplog.at_level(logging.INFO, logger="test-strategy-runner"):
        signals = await runner.run_once(
            [StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="1m")]
        )

    assert signals[0].side == SignalSide.BUY
    fields = caplog.records[0].__dict__
    assert fields["side"] == "buy"
    assert fields["symbol"] == "ETHUSDT"
