from __future__ import annotations

import json
from collections.abc import Sequence
from typing import Any, Protocol, cast

from redis.asyncio import Redis

from alphaflow.market_data.keys import (
    data_health_key,
    indicator_key,
    kline_data_key,
    kline_index_key,
)
from alphaflow.strategy.models import DataHealth, IndicatorSnapshot, Kline, MarketSnapshot


class RedisClient(Protocol):
    async def get(self, name: str) -> bytes | str | None: ...

    async def zrevrange(self, name: str, start: int, end: int) -> list[bytes | str]: ...

    async def hmget(self, name: str, keys: Sequence[str]) -> list[bytes | str | None]: ...

    async def aclose(self) -> None: ...


class MarketDataNotReadyError(RuntimeError):
    pass


class AsyncMarketDataReader:
    def __init__(self, redis: RedisClient, kline_limit: int = 50) -> None:
        self._redis = redis
        self._kline_limit = kline_limit

    @classmethod
    def from_url(cls, url: str, kline_limit: int = 50) -> AsyncMarketDataReader:
        return cls(cast(RedisClient, Redis.from_url(url)), kline_limit=kline_limit)

    async def close(self) -> None:
        await self._redis.aclose()

    async def read_snapshot(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> MarketSnapshot:
        indicator_payload = await self._redis.get(indicator_key(exchange, market, symbol, interval))
        if indicator_payload is None:
            raise MarketDataNotReadyError(f"indicator snapshot missing: {symbol} {interval}")
        health_payload = await self._redis.get(data_health_key(exchange, market, symbol, interval))
        if health_payload is None:
            raise MarketDataNotReadyError(f"data health missing: {symbol} {interval}")

        klines = await self.read_recent_klines(
            exchange,
            market,
            symbol,
            interval,
            self._kline_limit,
        )
        return MarketSnapshot(
            indicator=decode_indicator(indicator_payload),
            health=decode_health(health_payload),
            klines=tuple(klines),
        )

    async def read_many(
        self,
        targets: Sequence[tuple[str, str, str, str]],
    ) -> list[MarketSnapshot]:
        snapshots: list[MarketSnapshot] = []
        for exchange, market, symbol, interval in targets:
            snapshots.append(await self.read_snapshot(exchange, market, symbol, interval))
        return snapshots

    async def read_recent_klines(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
        limit: int,
    ) -> list[Kline]:
        fields = await self._redis.zrevrange(
            kline_index_key(exchange, market, symbol, interval),
            0,
            max(0, limit - 1),
        )
        if not fields:
            return []
        ordered_fields = [decode_text(field) for field in reversed(fields)]
        values = await self._redis.hmget(
            kline_data_key(exchange, market, symbol, interval),
            ordered_fields,
        )
        return [decode_kline(value) for value in values if value is not None]


def decode_indicator(payload: bytes | str) -> IndicatorSnapshot:
    data = decode_json(payload)
    return IndicatorSnapshot(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        interval=str(data["interval"]),
        open_time=int(data["open_time"]),
        close_time=int(data["close_time"]),
        values={str(key): str(value) for key, value in data.get("values", {}).items()},
        signals={str(key): str(value) for key, value in data.get("signals", {}).items()},
        updated_at=int(data.get("updated_at", 0)),
    )


def decode_health(payload: bytes | str) -> DataHealth:
    data = decode_json(payload)
    return DataHealth(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        interval=str(data["interval"]),
        kline_status=str(data["kline_status"]),
        indicator_status=str(data["indicator_status"]),
        last_kline_open_time=int(data.get("last_kline_open_time", 0)),
        last_indicator_open_time=int(data.get("last_indicator_open_time", 0)),
        reason=str(data.get("reason", "")),
        updated_at=int(data.get("updated_at", 0)),
    )


def decode_kline(payload: bytes | str) -> Kline:
    data = decode_json(payload)
    return Kline(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        interval=str(data["interval"]),
        open_time=int(data["open_time"]),
        close_time=int(data["close_time"]),
        open=str(data["open"]),
        high=str(data["high"]),
        low=str(data["low"]),
        close=str(data["close"]),
        volume=str(data["volume"]),
        quote_volume=str(data.get("quote_volume", "")),
        trade_count=int(data.get("trade_count", 0)),
        taker_buy_volume=str(data.get("taker_buy_volume", "")),
        taker_buy_quote_volume=str(data.get("taker_buy_quote_volume", "")),
        is_closed=bool(data.get("is_closed", False)),
        event_time=int(data.get("event_time", 0)),
    )


def decode_json(payload: bytes | str) -> dict[str, Any]:
    decoded = json.loads(decode_text(payload))
    if not isinstance(decoded, dict):
        raise ValueError("market data payload must be a JSON object")
    return decoded


def decode_text(value: bytes | str) -> str:
    if isinstance(value, bytes):
        return value.decode("utf-8")
    return value
