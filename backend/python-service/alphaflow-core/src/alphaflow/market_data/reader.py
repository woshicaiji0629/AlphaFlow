from __future__ import annotations

import json
import time
from collections.abc import Callable, Mapping, Sequence
from types import MappingProxyType
from typing import Any, Protocol, cast

from redis.asyncio import Redis

from alphaflow.market_data.keys import (
    data_health_key,
    indicator_key,
    indicator_realtime_key,
    indicator_window_key,
    kline_data_key,
    kline_index_key,
    last_price_key,
    mark_price_key,
)
from alphaflow.strategy.indicator_window import analyze_indicators
from alphaflow.strategy.models import (
    DataHealth,
    IndicatorSeriesAnalysis,
    IndicatorSnapshot,
    IndicatorWindowAnalysis,
    Kline,
    LastPrice,
    MarketSnapshot,
    MarkPrice,
    SignalSeriesAnalysis,
    SnapshotFreshness,
)
from alphaflow.strategy.window import analyze_klines


class RedisClient(Protocol):
    async def get(self, name: str) -> bytes | str | None: ...

    async def hgetall(self, name: str) -> dict[bytes | str, bytes | str]: ...

    async def zrevrange(self, name: str, start: int, end: int) -> list[bytes | str]: ...

    async def hmget(self, name: str, keys: Sequence[str]) -> list[bytes | str | None]: ...

    async def aclose(self) -> None: ...


class MarketDataNotReadyError(RuntimeError):
    pass


class AsyncMarketDataReader:
    def __init__(
        self,
        redis: RedisClient,
        kline_limit: int = 200,
        now_ms: Callable[[], int] | None = None,
    ) -> None:
        self._redis = redis
        self._kline_limit = kline_limit
        self._now_ms = now_ms or current_time_millis

    @classmethod
    def from_url(
        cls,
        url: str,
        kline_limit: int = 200,
        now_ms: Callable[[], int] | None = None,
    ) -> AsyncMarketDataReader:
        return cls(
            cast(RedisClient, Redis.from_url(url)),
            kline_limit=kline_limit,
            now_ms=now_ms,
        )

    async def close(self) -> None:
        await self._redis.aclose()

    async def read_snapshot(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> MarketSnapshot:
        feature_snapshot = await self.read_feature_hash_snapshot(
            exchange,
            market,
            symbol,
            interval,
        )
        if feature_snapshot is not None:
            return feature_snapshot

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
        last_price_payload = await self._redis.get(last_price_key(exchange, market, symbol))
        mark_price_payload = await self._redis.get(mark_price_key(exchange, market, symbol))
        indicator = decode_indicator(indicator_payload)
        indicator_history = (indicator,)
        return MarketSnapshot(
            indicator=indicator,
            health=decode_health(health_payload),
            klines=tuple(klines),
            indicator_history=indicator_history,
            indicator_window=analyze_indicators(indicator_history),
            last_price=decode_last_price(last_price_payload) if last_price_payload else None,
            mark_price=decode_mark_price(mark_price_payload) if mark_price_payload else None,
            window=analyze_klines(tuple(klines), lookback=self._kline_limit),
        )

    async def read_feature_hash_snapshot(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> MarketSnapshot | None:
        window_hash = decode_hash(
            await self._redis.hgetall(indicator_window_key(exchange, market, symbol, interval))
        )
        realtime_hash = decode_hash(
            await self._redis.hgetall(indicator_realtime_key(exchange, market, symbol, interval))
        )
        if not window_hash and not realtime_hash:
            return None
        if not window_hash:
            raise MarketDataNotReadyError(f"indicator window hash missing: {symbol} {interval}")
        if not realtime_hash:
            raise MarketDataNotReadyError(f"indicator realtime hash missing: {symbol} {interval}")

        freshness = validate_feature_hash_freshness(window_hash, realtime_hash, self._now_ms())
        if not freshness.valid:
            raise MarketDataNotReadyError(
                f"feature hash stale: {symbol} {interval}: {freshness.reason}"
            )

        kline = decode_realtime_kline(realtime_hash)
        realtime_values = prefixed_fields(realtime_hash, "value:")
        realtime_values.setdefault("close", kline.close)
        indicator = IndicatorSnapshot(
            exchange=realtime_hash.get("meta:exchange", exchange),
            market=realtime_hash.get("meta:market", market),
            symbol=realtime_hash.get("meta:symbol", symbol),
            interval=realtime_hash.get("meta:interval", interval),
            open_time=int_field(realtime_hash, "meta:open_time"),
            close_time=int_field(realtime_hash, "meta:close_time"),
            values=MappingProxyType(realtime_values),
            signals=MappingProxyType(prefixed_fields(realtime_hash, "signal:")),
            updated_at=int_field(realtime_hash, "meta:updated_at"),
        )
        return MarketSnapshot(
            indicator=indicator,
            health=DataHealth(
                exchange=indicator.exchange,
                market=indicator.market,
                symbol=indicator.symbol,
                interval=indicator.interval,
                kline_status="ok",
                indicator_status="ok",
                last_kline_open_time=kline.open_time,
                last_indicator_open_time=indicator.open_time,
                reason="feature_hash",
                updated_at=indicator.updated_at,
            ),
            klines=(kline,),
            indicator_history=(indicator,),
            indicator_window=decode_indicator_window_hash(window_hash),
            freshness=freshness,
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


def decode_last_price(payload: bytes | str) -> LastPrice:
    data = decode_json(payload)
    return LastPrice(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        price=str(data["price"]),
        quantity=str(data.get("quantity", "")),
        event_time=int(data.get("event_time", 0)),
        trade_time=int(data.get("trade_time", 0)),
        trade_id=int(data.get("trade_id", 0)),
    )


def decode_mark_price(payload: bytes | str) -> MarkPrice:
    data = decode_json(payload)
    return MarkPrice(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        mark_price=str(data["mark_price"]),
        index_price=str(data.get("index_price", "")),
        funding_rate=str(data.get("funding_rate", "")),
        next_funding_time=int(data.get("next_funding_time", 0)),
        event_time=int(data.get("event_time", 0)),
    )


def decode_indicator_window_hash(fields: Mapping[str, str]) -> IndicatorWindowAnalysis:
    raw_values = prefixed_fields(fields, "value:")
    raw_signals = prefixed_fields(fields, "signal:")
    return IndicatorWindowAnalysis(
        sample_count=int(raw_values.get("window_sample_count", "0")),
        values=MappingProxyType(decode_window_values(raw_values, raw_signals)),
        signals=MappingProxyType(decode_window_signals(raw_signals)),
    )


def decode_window_values(
    values: dict[str, str],
    signals: dict[str, str],
) -> dict[str, IndicatorSeriesAnalysis]:
    grouped: dict[str, dict[str, str]] = {}
    direct: dict[str, str] = {}
    suffixes = (
        "_win_latest",
        "_win_previous",
        "_win_change",
        "_win_change_pct",
        "_win_slope",
        "_win_min",
        "_win_max",
        "_win_range_pos_pct",
        "_win_rising_count",
        "_win_falling_count",
    )
    for key, value in values.items():
        matched = False
        for suffix in suffixes:
            if key.endswith(suffix):
                grouped.setdefault(key[: -len(suffix)], {})[suffix] = value
                matched = True
                break
        if not matched:
            direct[key] = value

    analyses: dict[str, IndicatorSeriesAnalysis] = {}
    for key, items in grouped.items():
        analyses[key] = IndicatorSeriesAnalysis(
            latest=float_field(items, "_win_latest"),
            previous=float_field(items, "_win_previous"),
            change=float_field(items, "_win_change"),
            change_pct=float_field(items, "_win_change_pct"),
            slope=float_field(items, "_win_slope"),
            direction=signals.get(f"{key}_win_direction", "unknown"),
            rising_count=int_string(items.get("_win_rising_count", "0")),
            falling_count=int_string(items.get("_win_falling_count", "0")),
            minimum=float_field(items, "_win_min"),
            maximum=float_field(items, "_win_max"),
            range_position_pct=float_field(items, "_win_range_pos_pct"),
        )

    for key, value in direct.items():
        parsed = optional_float(value)
        if parsed is not None:
            analyses[key] = IndicatorSeriesAnalysis(latest=parsed, previous=parsed)
    return analyses


def decode_window_signals(signals: dict[str, str]) -> dict[str, SignalSeriesAnalysis]:
    grouped: dict[str, dict[str, str]] = {}
    direct: dict[str, str] = {}
    suffixes = (
        "_win_latest",
        "_win_previous",
        "_win_changed",
        "_win_stable_count",
        "_win_last_changed_ago",
    )
    for key, value in signals.items():
        matched = False
        for suffix in suffixes:
            if key.endswith(suffix):
                grouped.setdefault(key[: -len(suffix)], {})[suffix] = value
                matched = True
                break
        if not matched and not key.endswith("_win_direction"):
            direct[key] = value

    analyses: dict[str, SignalSeriesAnalysis] = {}
    for key, items in grouped.items():
        analyses[key] = SignalSeriesAnalysis(
            latest=items.get("_win_latest", ""),
            previous=items.get("_win_previous", ""),
            changed=bool_field(items.get("_win_changed", "false")),
            stable_count=int_string(items.get("_win_stable_count", "0")),
            last_changed_ago=int_string(items.get("_win_last_changed_ago", "0")),
        )
    for key, value in direct.items():
        analyses[key] = SignalSeriesAnalysis(latest=value)
    return analyses


def decode_realtime_kline(fields: dict[str, str]) -> Kline:
    return Kline(
        exchange=fields.get("meta:exchange", ""),
        market=fields.get("meta:market", ""),
        symbol=fields.get("meta:symbol", ""),
        interval=fields.get("meta:interval", ""),
        open_time=int_field(fields, "kline:open_time"),
        close_time=int_field(fields, "kline:close_time"),
        open=fields.get("kline:open", ""),
        high=fields.get("kline:high", ""),
        low=fields.get("kline:low", ""),
        close=fields.get("kline:close", ""),
        volume=fields.get("kline:volume", ""),
        quote_volume=fields.get("kline:quote_volume", ""),
        trade_count=int_field(fields, "kline:trade_count"),
        taker_buy_volume=fields.get("kline:taker_buy_volume", ""),
        taker_buy_quote_volume=fields.get("kline:taker_buy_quote_volume", ""),
        is_closed=bool_field(fields.get("kline:is_closed", "false")),
    )


def validate_feature_hash_freshness(
    window_hash: dict[str, str],
    realtime_hash: dict[str, str],
    now_ms: int,
) -> SnapshotFreshness:
    interval_ms = int_field(realtime_hash, "meta:bar_interval_ms")
    if interval_ms <= 0:
        return SnapshotFreshness(valid=False, reason="missing realtime interval")
    if int_field(window_hash, "meta:bar_interval_ms") != interval_ms:
        return SnapshotFreshness(valid=False, reason="window/realtime interval mismatch")

    expected_realtime = now_ms // interval_ms
    expected_window = expected_realtime - 1
    realtime_seq = int_field(realtime_hash, "meta:bar_seq")
    window_seq = int_field(window_hash, "meta:bar_seq")
    realtime_updated_at = int_field(realtime_hash, "meta:updated_at")
    window_updated_at = int_field(window_hash, "meta:updated_at")
    realtime_age_limit = int_field(realtime_hash, "meta:age_limit_ms")
    window_age_limit = int_field(window_hash, "meta:age_limit_ms")

    freshness = SnapshotFreshness(
        valid=True,
        window_bar_seq=window_seq,
        realtime_bar_seq=realtime_seq,
        expected_window_bar_seq=expected_window,
        expected_realtime_bar_seq=expected_realtime,
        window_updated_at=window_updated_at,
        realtime_updated_at=realtime_updated_at,
    )
    if window_seq != expected_window:
        return invalid_freshness(freshness, "window bar is not latest closed bar")
    if realtime_seq != expected_realtime:
        return invalid_freshness(freshness, "realtime bar is not current bar")
    if now_ms - realtime_updated_at > realtime_age_limit:
        return invalid_freshness(freshness, "realtime hash is stale")
    if now_ms - window_updated_at > window_age_limit:
        return invalid_freshness(freshness, "window hash is stale")
    if bool_field(realtime_hash.get("kline:is_closed", "false")):
        return invalid_freshness(freshness, "realtime kline is closed")
    return freshness


def invalid_freshness(freshness: SnapshotFreshness, reason: str) -> SnapshotFreshness:
    return SnapshotFreshness(
        valid=False,
        reason=reason,
        window_bar_seq=freshness.window_bar_seq,
        realtime_bar_seq=freshness.realtime_bar_seq,
        expected_window_bar_seq=freshness.expected_window_bar_seq,
        expected_realtime_bar_seq=freshness.expected_realtime_bar_seq,
        window_updated_at=freshness.window_updated_at,
        realtime_updated_at=freshness.realtime_updated_at,
    )


def decode_hash(payload: dict[bytes | str, bytes | str]) -> dict[str, str]:
    return {decode_text(key): decode_text(value) for key, value in payload.items()}


def prefixed_fields(fields: Mapping[str, str], prefix: str) -> dict[str, str]:
    return {
        key[len(prefix) :]: value
        for key, value in fields.items()
        if str(key).startswith(prefix)
    }


def int_field(fields: dict[str, str], key: str) -> int:
    return int_string(fields.get(key, "0"))


def int_string(value: str) -> int:
    try:
        return int(value)
    except ValueError:
        return 0


def float_field(fields: dict[str, str], key: str) -> float:
    return optional_float(fields.get(key, "")) or 0.0


def optional_float(value: str) -> float | None:
    try:
        return float(value)
    except ValueError:
        return None


def bool_field(value: str) -> bool:
    return value.strip().lower() == "true"


def current_time_millis() -> int:
    return int(time.time() * 1000)


def decode_json(payload: bytes | str) -> dict[str, Any]:
    decoded = json.loads(decode_text(payload))
    if not isinstance(decoded, dict):
        raise ValueError("market data payload must be a JSON object")
    return decoded


def decode_text(value: bytes | str) -> str:
    if isinstance(value, bytes):
        return value.decode("utf-8")
    return value
