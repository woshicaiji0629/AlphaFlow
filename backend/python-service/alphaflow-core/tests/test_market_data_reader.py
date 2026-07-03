import asyncio
import json
from collections.abc import Sequence

import pytest

from alphaflow.market_data import (
    data_health_key,
    indicator_key,
    indicator_realtime_key,
    indicator_window_key,
    kline_data_key,
    kline_index_key,
    last_price_key,
    mark_price_key,
)
from alphaflow.market_data.reader import AsyncMarketDataReader, MarketDataNotReadyError


class FakeRedis:
    def __init__(self) -> None:
        self.values: dict[str, bytes | str] = {}
        self.zsets: dict[str, list[bytes | str]] = {}
        self.hashes: dict[str, dict[bytes | str, bytes | str]] = {}
        self.closed = False

    async def get(self, name: str) -> bytes | str | None:
        return self.values.get(name)

    async def hgetall(self, name: str) -> dict[bytes | str, bytes | str]:
        return self.hashes.get(name, {})

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

    snapshot = await AsyncMarketDataReader(redis, kline_limit=2).read_snapshot(*target)

    assert snapshot.indicator.values["rsi_14"] == "32"
    assert snapshot.health.is_ok()
    assert [kline.open_time for kline in snapshot.klines] == [1000, 2000]
    assert snapshot.last_price is not None
    assert snapshot.last_price.price == "101.5"
    assert snapshot.mark_price is not None
    assert snapshot.mark_price.mark_price == "101.4"
    assert snapshot.window is not None
    assert snapshot.window.sample_count == 2
    assert [item.open_time for item in snapshot.indicator_history] == [1000]
    assert snapshot.indicator_window is not None
    assert snapshot.indicator_window.sample_count == 1
    assert "macd_hist" not in snapshot.indicator_window.values
    assert snapshot.indicator_window.signals["data_quality"].latest == "ok"


def test_reader_reports_missing_indicator() -> None:
    asyncio.run(run_reader_reports_missing_indicator())


async def run_reader_reports_missing_indicator() -> None:
    redis = FakeRedis()

    with pytest.raises(MarketDataNotReadyError):
        await AsyncMarketDataReader(redis).read_snapshot("binance", "um", "ETHUSDT", "1m")


def test_reader_decodes_feature_hash_snapshot() -> None:
    asyncio.run(run_reader_decodes_feature_hash_snapshot())


async def run_reader_decodes_feature_hash_snapshot() -> None:
    redis = FakeRedis()
    target = ("binance", "um", "ETHUSDT", "15m")
    redis.hashes[indicator_window_key(*target)] = {
        "meta:snapshot_type": "window",
        "meta:exchange": "binance",
        "meta:market": "um",
        "meta:symbol": "ETHUSDT",
        "meta:interval": "15m",
        "meta:open_time": "900000",
        "meta:close_time": "1799999",
        "meta:bar_interval_ms": "900000",
        "meta:bar_seq": "1",
        "meta:age_limit_ms": "1800000",
        "meta:updated_at": "1800500",
        "value:window_sample_count": "20",
        "value:ema7_win_latest": "101",
        "value:ema7_win_previous": "100",
        "value:ema7_win_change": "1",
        "value:ema7_win_slope": "0.5",
        "value:pump_window_score": "88",
        "signal:ema7_win_direction": "rising",
        "signal:supertrend_direction_win_latest": "up",
        "signal:supertrend_direction_win_stable_count": "3",
        "signal:pump_window_signal": "true",
    }
    redis.hashes[indicator_realtime_key(*target)] = {
        "meta:snapshot_type": "realtime",
        "meta:exchange": "binance",
        "meta:market": "um",
        "meta:symbol": "ETHUSDT",
        "meta:interval": "15m",
        "meta:open_time": "1800000",
        "meta:close_time": "2699999",
        "meta:bar_interval_ms": "900000",
        "meta:bar_seq": "2",
        "meta:age_limit_ms": "30000",
        "meta:updated_at": "1810000",
        "kline:open_time": "1800000",
        "kline:close_time": "2699999",
        "kline:open": "101",
        "kline:high": "103",
        "kline:low": "100",
        "kline:close": "102",
        "kline:volume": "12",
        "kline:quote_volume": "1224",
        "kline:trade_count": "10",
        "kline:is_closed": "false",
        "value:rsi14": "58",
        "signal:ema_alignment": "bull",
    }

    snapshot = await AsyncMarketDataReader(redis, now_ms=lambda: 1810000).read_snapshot(*target)

    assert snapshot.health.is_ok()
    assert snapshot.freshness is not None
    assert snapshot.freshness.valid
    assert snapshot.freshness.window_bar_seq == 1
    assert snapshot.freshness.realtime_bar_seq == 2
    assert snapshot.indicator.open_time == 1800000
    assert snapshot.indicator.values["close"] == "102"
    assert snapshot.indicator.values["rsi14"] == "58"
    assert snapshot.klines[0].close == "102"
    assert snapshot.indicator_window is not None
    assert snapshot.indicator_window.sample_count == 20
    assert snapshot.indicator_window.values["ema7"].latest == 101
    assert snapshot.indicator_window.values["ema7"].direction == "rising"
    assert snapshot.indicator_window.values["pump_window_score"].latest == 88
    assert snapshot.indicator_window.signals["supertrend_direction"].latest == "up"
    assert snapshot.indicator_window.signals["supertrend_direction"].stable_count == 3
    assert snapshot.indicator_window.signals["pump_window_signal"].latest == "true"


def test_reader_rejects_stale_realtime_feature_hash() -> None:
    asyncio.run(run_reader_rejects_stale_realtime_feature_hash())


async def run_reader_rejects_stale_realtime_feature_hash() -> None:
    redis = FakeRedis()
    target = ("binance", "um", "ETHUSDT", "3m")
    redis.hashes[indicator_window_key(*target)] = {
        "meta:bar_interval_ms": "180000",
        "meta:bar_seq": "9",
        "meta:age_limit_ms": "360000",
        "meta:updated_at": "1800000",
    }
    redis.hashes[indicator_realtime_key(*target)] = {
        "meta:bar_interval_ms": "180000",
        "meta:bar_seq": "10",
        "meta:age_limit_ms": "15000",
        "meta:updated_at": "1800000",
        "kline:is_closed": "false",
    }

    with pytest.raises(MarketDataNotReadyError, match="realtime hash is stale"):
        await AsyncMarketDataReader(redis, now_ms=lambda: 1816000).read_snapshot(*target)


def test_market_data_keys_match_go_shape() -> None:
    assert indicator_key("binance", "um", "ETHUSDT", "1m") == "bn:um:ind:ETHUSDT:1m"
    assert indicator_window_key("binance", "um", "ETHUSDT", "1m") == "bn:um:indwin:ETHUSDT:1m"
    assert indicator_realtime_key("binance", "um", "ETHUSDT", "1m") == "bn:um:indrt:ETHUSDT:1m"
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
