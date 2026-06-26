from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum
from types import MappingProxyType


class SignalSide(StrEnum):
    BUY = "buy"
    SELL = "sell"
    HOLD = "hold"


@dataclass(frozen=True)
class IndicatorSnapshot:
    exchange: str
    market: str
    symbol: str
    interval: str
    open_time: int
    close_time: int
    values: Mapping[str, str]
    signals: Mapping[str, str] = MappingProxyType({})
    updated_at: int = 0


@dataclass(frozen=True)
class DataHealth:
    exchange: str
    market: str
    symbol: str
    interval: str
    kline_status: str
    indicator_status: str
    last_kline_open_time: int = 0
    last_indicator_open_time: int = 0
    reason: str = ""
    updated_at: int = 0

    def is_ok(self) -> bool:
        return self.kline_status == "ok" and self.indicator_status == "ok"


@dataclass(frozen=True)
class Kline:
    exchange: str
    market: str
    symbol: str
    interval: str
    open_time: int
    close_time: int
    open: str
    high: str
    low: str
    close: str
    volume: str
    quote_volume: str = ""
    trade_count: int = 0
    taker_buy_volume: str = ""
    taker_buy_quote_volume: str = ""
    is_closed: bool = False
    event_time: int = 0


@dataclass(frozen=True)
class MarketSnapshot:
    indicator: IndicatorSnapshot
    health: DataHealth
    klines: tuple[Kline, ...] = ()


@dataclass(frozen=True)
class Signal:
    exchange: str
    market: str
    symbol: str
    interval: str
    side: SignalSide
    score: float
    confidence: float
    reason: str
    open_time: int
    updated_at: int
