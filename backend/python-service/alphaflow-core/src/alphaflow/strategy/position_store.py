from __future__ import annotations

import json
from typing import Any, Protocol, cast

import asyncpg
from redis.asyncio import Redis

from alphaflow.strategy.models import (
    ClosedPosition,
    ExitReasonType,
    ExitRule,
    PositionSide,
    PositionState,
)


class RedisPositionClient(Protocol):
    async def get(self, name: str) -> bytes | str | None: ...

    async def set(self, name: str, value: str) -> object: ...

    async def delete(self, *names: str) -> int: ...

    async def aclose(self) -> None: ...


class PositionStore(Protocol):
    async def get_active_position(
        self,
        exchange: str,
        market: str,
        symbol: str,
        strategy_name: str,
    ) -> PositionState | None: ...

    async def save_active_position(self, position: PositionState) -> None: ...

    async def clear_active_position(self, position: PositionState) -> None: ...


class PositionHistoryStore(Protocol):
    async def initialize(self) -> None: ...

    async def save_closed_position(self, position: ClosedPosition) -> None: ...

    async def list_closed_positions(self, position_id: str) -> list[ClosedPosition]: ...


class RedisPositionStore:
    def __init__(self, redis: RedisPositionClient) -> None:
        self._redis = redis

    @classmethod
    def from_url(cls, url: str) -> RedisPositionStore:
        return cls(cast(RedisPositionClient, Redis.from_url(url)))

    async def close(self) -> None:
        await self._redis.aclose()

    async def get_active_position(
        self,
        exchange: str,
        market: str,
        symbol: str,
        strategy_name: str,
    ) -> PositionState | None:
        payload = await self._redis.get(position_key(exchange, market, symbol, strategy_name))
        if payload is None:
            return None
        return decode_position(payload)

    async def save_active_position(self, position: PositionState) -> None:
        await self._redis.set(position_key_for(position), json.dumps(encode_position(position)))

    async def clear_active_position(self, position: PositionState) -> None:
        await self._redis.delete(position_key_for(position))


class PostgresPositionHistoryStore:
    def __init__(self, dsn: str) -> None:
        self._dsn = dsn
        self._pool: asyncpg.Pool | None = None

    async def initialize(self) -> None:
        self._pool = await asyncpg.create_pool(self._dsn)
        async with self._pool.acquire() as connection:
            await connection.execute(CREATE_POSITION_HISTORY_TABLE_SQL)
            await connection.execute(ALTER_POSITION_HISTORY_TABLE_SQL)

    async def close(self) -> None:
        if self._pool is not None:
            await self._pool.close()

    async def save_closed_position(self, position: ClosedPosition) -> None:
        if self._pool is None:
            await self.initialize()
        if self._pool is None:
            raise RuntimeError("postgres position history pool is not initialized")
        async with self._pool.acquire() as connection:
            await connection.execute(
                INSERT_CLOSED_POSITION_SQL,
                position.position_id,
                position.exchange,
                position.market,
                position.symbol,
                position.strategy_name,
                position.side.value,
                position.size,
                position.initial_size,
                position.entry_price,
                position.exit_price,
                json.dumps([encode_exit_rule(rule) for rule in position.exit_rules]),
                json.dumps(encode_exit_rule(position.triggered_exit_rule))
                if position.triggered_exit_rule is not None
                else None,
                position.entry_time,
                position.exit_time,
                position.realized_pnl,
                position.realized_pnl_pct,
                position.entry_reason,
                position.exit_reason,
                position.exit_reason_type.value,
                position.margin,
                position.leverage,
                position.fee,
                position.net_pnl,
                position.net_pnl_pct,
                position.remaining_size_after_exit,
                position.is_final_exit,
                position.total_realized_pnl,
                position.total_fee,
                position.total_net_pnl,
                position.total_net_pnl_pct,
            )

    async def list_closed_positions(self, position_id: str) -> list[ClosedPosition]:
        if self._pool is None:
            await self.initialize()
        if self._pool is None:
            raise RuntimeError("postgres position history pool is not initialized")
        async with self._pool.acquire() as connection:
            rows = await connection.fetch(
                "SELECT * FROM strategy_position_history WHERE position_id = $1 ORDER BY id",
                position_id,
            )
        return [closed_position_from_row(dict(row)) for row in rows]


def position_key(exchange: str, market: str, symbol: str, strategy_name: str) -> str:
    return f"strategy:position:{exchange}:{market}:{symbol}:{strategy_name}"


def position_key_for(position: PositionState) -> str:
    return position_key(position.exchange, position.market, position.symbol, position.strategy_name)


def encode_position(position: PositionState) -> dict[str, object]:
    return {
        "exchange": position.exchange,
        "market": position.market,
        "symbol": position.symbol,
        "strategy_name": position.strategy_name,
        "position_id": position.position_id,
        "side": position.side.value,
        "size": position.size,
        "initial_size": position.initial_size,
        "entry_price": position.entry_price,
        "highest_price": position.highest_price,
        "lowest_price": position.lowest_price,
        "exit_rules": [encode_exit_rule(rule) for rule in position.exit_rules],
        "entry_time": position.entry_time,
        "entry_reason": position.entry_reason,
        "updated_at": position.updated_at,
    }


def decode_position(payload: bytes | str) -> PositionState:
    data = json.loads(payload.decode("utf-8") if isinstance(payload, bytes) else payload)
    if not isinstance(data, dict):
        raise ValueError("position payload must be a JSON object")
    return PositionState(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        strategy_name=str(data["strategy_name"]),
        position_id=str(data.get("position_id", "")),
        side=PositionSide(str(data.get("side", PositionSide.FLAT.value))),
        size=float(data.get("size", 0.0)),
        initial_size=float(data.get("initial_size", data.get("size", 0.0))),
        entry_price=str(data.get("entry_price", "")),
        highest_price=str(data.get("highest_price", "")),
        lowest_price=str(data.get("lowest_price", "")),
        exit_rules=tuple(decode_exit_rule(item) for item in data.get("exit_rules", [])),
        entry_time=int(data.get("entry_time", 0)),
        entry_reason=str(data.get("entry_reason", "")),
        updated_at=int(data.get("updated_at", 0)),
    )


def closed_position_from_row(row: dict[str, Any]) -> ClosedPosition:
    return ClosedPosition(
        position_id=str(row.get("position_id", "")),
        exchange=str(row["exchange"]),
        market=str(row["market"]),
        symbol=str(row["symbol"]),
        strategy_name=str(row["strategy_name"]),
        side=PositionSide(str(row["side"])),
        size=float(row["size"]),
        initial_size=float(row.get("initial_size", row["size"])),
        entry_price=str(row["entry_price"]),
        exit_price=str(row["exit_price"]),
        exit_rules=tuple(
            decode_exit_rule(item) for item in json.loads(row.get("exit_rules", "[]"))
        ),
        triggered_exit_rule=decode_optional_exit_rule(row.get("triggered_exit_rule")),
        entry_time=int(row["entry_time"]),
        exit_time=int(row["exit_time"]),
        entry_reason=str(row["entry_reason"]),
        exit_reason=str(row["exit_reason"]),
        exit_reason_type=ExitReasonType(str(row.get("exit_reason_type", ExitReasonType.STRATEGY))),
        realized_pnl=float(row["realized_pnl"]),
        realized_pnl_pct=float(row["realized_pnl_pct"]),
        margin=float(row.get("margin", 0.0)),
        leverage=float(row.get("leverage", 1.0)),
        fee=float(row.get("fee", 0.0)),
        net_pnl=float(row.get("net_pnl", row["realized_pnl"])),
        net_pnl_pct=float(row.get("net_pnl_pct", row["realized_pnl_pct"])),
        remaining_size_after_exit=float(row.get("remaining_size_after_exit", 0.0)),
        is_final_exit=bool(row.get("is_final_exit", True)),
        total_realized_pnl=float(row.get("total_realized_pnl", 0.0)),
        total_fee=float(row.get("total_fee", 0.0)),
        total_net_pnl=float(row.get("total_net_pnl", 0.0)),
        total_net_pnl_pct=float(row.get("total_net_pnl_pct", 0.0)),
    )


def encode_exit_rule(rule: ExitRule | None) -> dict[str, object] | None:
    if rule is None:
        return None
    return {
        "rule_type": rule.rule_type.value,
        "reason": rule.reason,
        "trigger_price": rule.trigger_price,
        "size_pct": rule.size_pct,
        "metadata": dict(rule.metadata),
    }


def decode_optional_exit_rule(value: object) -> ExitRule | None:
    if value is None:
        return None
    if isinstance(value, str):
        return decode_exit_rule(json.loads(value))
    if isinstance(value, dict):
        return decode_exit_rule(value)
    return None


def decode_exit_rule(data: object) -> ExitRule:
    if not isinstance(data, dict):
        raise ValueError("exit rule payload must be a JSON object")
    return ExitRule(
        rule_type=ExitReasonType(str(data["rule_type"])),
        reason=str(data.get("reason", "")),
        trigger_price=str(data.get("trigger_price", "")),
        size_pct=float(data.get("size_pct", 1.0)),
        metadata={str(key): str(value) for key, value in data.get("metadata", {}).items()},
    )


CREATE_POSITION_HISTORY_TABLE_SQL = """
CREATE TABLE IF NOT EXISTS strategy_position_history (
    id BIGSERIAL PRIMARY KEY,
    position_id TEXT NOT NULL DEFAULT '',
    exchange TEXT NOT NULL,
    market TEXT NOT NULL,
    symbol TEXT NOT NULL,
    strategy_name TEXT NOT NULL,
    side TEXT NOT NULL,
    size DOUBLE PRECISION NOT NULL,
    initial_size DOUBLE PRECISION NOT NULL DEFAULT 0,
    entry_price NUMERIC NOT NULL,
    exit_price NUMERIC NOT NULL,
    exit_rules JSONB NOT NULL DEFAULT '[]',
    triggered_exit_rule JSONB,
    entry_time BIGINT NOT NULL,
    exit_time BIGINT NOT NULL,
    realized_pnl DOUBLE PRECISION NOT NULL,
    realized_pnl_pct DOUBLE PRECISION NOT NULL,
    entry_reason TEXT NOT NULL,
    exit_reason TEXT NOT NULL,
    exit_reason_type TEXT NOT NULL DEFAULT 'strategy',
    margin DOUBLE PRECISION NOT NULL DEFAULT 0,
    leverage DOUBLE PRECISION NOT NULL DEFAULT 1,
    fee DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_pnl_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    remaining_size_after_exit DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_final_exit BOOLEAN NOT NULL DEFAULT true,
    total_realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_fee DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_net_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_net_pnl_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
"""

ALTER_POSITION_HISTORY_TABLE_SQL = """
ALTER TABLE strategy_position_history
    ADD COLUMN IF NOT EXISTS position_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS initial_size DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS exit_rules JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS triggered_exit_rule JSONB,
    ADD COLUMN IF NOT EXISTS exit_reason_type TEXT NOT NULL DEFAULT 'strategy',
    ADD COLUMN IF NOT EXISTS margin DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS leverage DOUBLE PRECISION NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS fee DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS net_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS net_pnl_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS remaining_size_after_exit DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_final_exit BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS total_realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_fee DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_net_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_net_pnl_pct DOUBLE PRECISION NOT NULL DEFAULT 0;
"""

INSERT_CLOSED_POSITION_SQL = """
INSERT INTO strategy_position_history (
    position_id,
    exchange,
    market,
    symbol,
    strategy_name,
    side,
    size,
    initial_size,
    entry_price,
    exit_price,
    exit_rules,
    triggered_exit_rule,
    entry_time,
    exit_time,
    realized_pnl,
    realized_pnl_pct,
    entry_reason,
    exit_reason,
    exit_reason_type,
    margin,
    leverage,
    fee,
    net_pnl,
    net_pnl_pct,
    remaining_size_after_exit,
    is_final_exit,
    total_realized_pnl,
    total_fee,
    total_net_pnl,
    total_net_pnl_pct
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30
);
"""
