import asyncio
import json
from collections.abc import Sequence

import pytest

from alphaflow.market_data import (
    data_health_key,
    indicator_key,
    kline_data_key,
    kline_index_key,
    last_price_key,
    mark_price_key,
)
from alphaflow.market_data.reader import AsyncMarketDataReader, MarketDataNotReadyError
from alphaflow.strategy import IndicatorSnapshot


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


class FakeIndicatorHistoryReader:
    def __init__(self, history: tuple[IndicatorSnapshot, ...]) -> None:
        self.history = history

    async def read_indicator_history(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> tuple[IndicatorSnapshot, ...]:
        return self.history


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
    redis.values[last_price_key("binance", "um", "ETHUSDT")] = json.dumps(
        {
            "exchange": "binance",
            "market": "um",
            "symbol": "ETHUSDT",
            "price": "101.5",
            "quantity": "0.2",
            "event_time": 2001,
            "trade_time": 2000,
            "trade_id": 10,
        }
    )
    redis.values[mark_price_key("binance", "um", "ETHUSDT")] = json.dumps(
        {
            "exchange": "binance",
            "market": "um",
            "symbol": "ETHUSDT",
            "mark_price": "101.4",
            "index_price": "101.3",
            "funding_rate": "0.0001",
            "next_funding_time": 3000,
            "event_time": 2001,
        }
    )
    redis.zsets[kline_index_key(*target)] = [b"1000", b"2000"]
    redis.hashes[kline_data_key(*target)] = {
        "1000": json.dumps(kline_payload(1000, "100")),
        "2000": json.dumps(kline_payload(2000, "101")),
    }

    history_reader = FakeIndicatorHistoryReader(
        (
            indicator_snapshot(500, {"rsi_14": "40", "macd_hist": "-0.2"}),
            indicator_snapshot(800, {"rsi_14": "45", "macd_hist": "-0.1"}),
        )
    )

    snapshot = await AsyncMarketDataReader(
        redis,
        kline_limit=2,
        indicator_history_reader=history_reader,
    ).read_snapshot(*target)

    assert snapshot.indicator.values["rsi_14"] == "32"
    assert snapshot.health.is_ok()
    assert [kline.open_time for kline in snapshot.klines] == [1000, 2000]
    assert snapshot.last_price is not None
    assert snapshot.last_price.price == "101.5"
    assert snapshot.mark_price is not None
    assert snapshot.mark_price.mark_price == "101.4"
    assert snapshot.window is not None
    assert snapshot.window.sample_count == 2
    assert [item.open_time for item in snapshot.indicator_history] == [500, 800, 1000]
    assert snapshot.indicator_window is not None
    assert snapshot.indicator_window.values["macd_hist"].direction == "rising"


def test_reader_reports_missing_indicator() -> None:
    asyncio.run(run_reader_reports_missing_indicator())


async def run_reader_reports_missing_indicator() -> None:
    redis = FakeRedis()

    with pytest.raises(MarketDataNotReadyError):
        await AsyncMarketDataReader(redis).read_snapshot("binance", "um", "ETHUSDT", "1m")


def test_market_data_keys_match_go_shape() -> None:
    assert indicator_key("binance", "um", "ETHUSDT", "1m") == "bn:um:ind:ETHUSDT:1m"
    assert data_health_key("binance", "um", "ETHUSDT", "1m") == "bn:um:health:ETHUSDT:1m"
    assert last_price_key("binance", "um", "ETHUSDT") == "bn:um:lp:ETHUSDT"
    assert mark_price_key("binance", "um", "ETHUSDT") == "bn:um:mp:ETHUSDT"
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


def indicator_snapshot(open_time: int, values: dict[str, str]) -> IndicatorSnapshot:
    return IndicatorSnapshot(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        interval="1m",
        open_time=open_time,
        close_time=open_time + 59999,
        values=values,
        signals={"data_quality": "ok"},
        updated_at=open_time + 60000,
    )
