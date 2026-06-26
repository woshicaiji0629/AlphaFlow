import asyncio
import json
from collections.abc import Sequence

import pytest

from alphaflow.market_data import data_health_key, indicator_key, kline_data_key, kline_index_key
from alphaflow.market_data.reader import AsyncMarketDataReader, MarketDataNotReadyError


class FakeRedis:
    def __init__(self) -> None:
        self.values: dict[str, bytes | str] = {}
        self.zsets: dict[str, list[bytes | str]] = {}
        self.hashes: dict[str, dict[str, bytes | str]] = {}
        self.closed = False

    async def get(self, name: str) -> bytes | str | None:
        return self.values.get(name)

    async def zrevrange(self, name: str, start: int, end: int) -> list[bytes | str]:
        values = list(reversed(self.zsets.get(name, [])))
        return values[start : end + 1]

    async def hmget(self, name: str, keys: Sequence[str]) -> list[bytes | str | None]:
        values = self.hashes.get(name, {})
        return [values.get(key) for key in keys]

    async def aclose(self) -> None:
        self.closed = True


def test_reader_decodes_market_snapshot() -> None:
    asyncio.run(run_reader_decodes_market_snapshot())


async def run_reader_decodes_market_snapshot() -> None:
    redis = FakeRedis()
    target = ("binance", "um", "ETHUSDT", "1m")
    redis.values[indicator_key(*target)] = json.dumps(
        {
            "exchange": "binance",
            "market": "um",
            "symbol": "ETHUSDT",
            "interval": "1m",
            "open_time": 1000,
            "close_time": 1999,
            "values": {"rsi_14": "32", "macd_hist": "0.1"},
            "signals": {"data_quality": "ok"},
            "updated_at": 2000,
        }
    )
    redis.values[data_health_key(*target)] = json.dumps(
        {
            "exchange": "binance",
            "market": "um",
            "symbol": "ETHUSDT",
            "interval": "1m",
            "kline_status": "ok",
            "indicator_status": "ok",
            "last_kline_open_time": 1000,
            "last_indicator_open_time": 1000,
            "updated_at": 2000,
        }
    )
    redis.zsets[kline_index_key(*target)] = [b"1000", b"2000"]
    redis.hashes[kline_data_key(*target)] = {
        "1000": json.dumps(kline_payload(1000, "100")),
        "2000": json.dumps(kline_payload(2000, "101")),
    }

    snapshot = await AsyncMarketDataReader(redis, kline_limit=2).read_snapshot(*target)

    assert snapshot.indicator.values["rsi_14"] == "32"
    assert snapshot.health.is_ok()
    assert [kline.open_time for kline in snapshot.klines] == [1000, 2000]


def test_reader_reports_missing_indicator() -> None:
    asyncio.run(run_reader_reports_missing_indicator())


async def run_reader_reports_missing_indicator() -> None:
    redis = FakeRedis()

    with pytest.raises(MarketDataNotReadyError):
        await AsyncMarketDataReader(redis).read_snapshot("binance", "um", "ETHUSDT", "1m")


def test_market_data_keys_match_go_shape() -> None:
    assert indicator_key("binance", "um", "ETHUSDT", "1m") == "bn:um:ind:ETHUSDT:1m"
    assert data_health_key("binance", "um", "ETHUSDT", "1m") == "bn:um:health:ETHUSDT:1m"
    assert kline_index_key("binance", "um", "ETHUSDT", "1m") == "bn:um:k:ETHUSDT:1m:idx"
    assert kline_data_key("binance", "um", "ETHUSDT", "1m") == "bn:um:k:ETHUSDT:1m:data"


def kline_payload(open_time: int, close: str) -> dict[str, object]:
    return {
        "exchange": "binance",
        "market": "um",
        "symbol": "ETHUSDT",
        "interval": "1m",
        "open_time": open_time,
        "close_time": open_time + 59999,
        "open": "100",
        "high": "102",
        "low": "99",
        "close": close,
        "volume": "10",
        "is_closed": True,
    }
