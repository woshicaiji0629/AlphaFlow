from __future__ import annotations

import asyncio
import logging
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Protocol

from alphaflow.market_data.reader import AsyncMarketDataReader, MarketDataNotReadyError
from alphaflow.strategy.engine import StrategyEngine
from alphaflow.strategy.models import MarketSnapshot, Signal


@dataclass(frozen=True)
class StrategyTarget:
    exchange: str
    market: str
    symbol: str
    interval: str

    def as_tuple(self) -> tuple[str, str, str, str]:
        return self.exchange, self.market, self.symbol, self.interval


class MarketDataReader(Protocol):
    async def read_snapshot(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> MarketSnapshot: ...


class StrategyRunner:
    def __init__(
        self,
        reader: MarketDataReader,
        engine: StrategyEngine | None = None,
        logger: logging.Logger | None = None,
    ) -> None:
        self._reader = reader
        self._engine = engine or StrategyEngine()
        self._logger = logger or logging.getLogger(__name__)

    async def run_once(self, targets: Sequence[StrategyTarget]) -> list[Signal]:
        signals: list[Signal] = []
        for target in targets:
            try:
                snapshot = await self._reader.read_snapshot(*target.as_tuple())
            except MarketDataNotReadyError as exc:
                self._logger.warning(
                    "market data not ready",
                    extra={
                        "exchange": target.exchange,
                        "market": target.market,
                        "symbol": target.symbol,
                        "interval": target.interval,
                        "error": str(exc),
                    },
                )
                continue
            signal = self._engine.evaluate(snapshot)
            signals.append(signal)
            self.log_signal(signal)
        return signals

    async def run_forever(
        self,
        targets: Sequence[StrategyTarget],
        interval_seconds: float,
    ) -> None:
        while True:
            await self.run_once(targets)
            await asyncio.sleep(interval_seconds)

    def log_signal(self, signal: Signal) -> None:
        self._logger.info(
            "strategy signal",
            extra={
                "exchange": signal.exchange,
                "market": signal.market,
                "symbol": signal.symbol,
                "interval": signal.interval,
                "side": signal.side.value,
                "score": signal.score,
                "confidence": signal.confidence,
                "reason": signal.reason,
                "open_time": signal.open_time,
                "updated_at": signal.updated_at,
            },
        )


def build_default_runner(redis_url: str) -> StrategyRunner:
    return StrategyRunner(AsyncMarketDataReader.from_url(redis_url))
