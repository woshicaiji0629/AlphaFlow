from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum
from types import MappingProxyType


class SignalSide(StrEnum):
    BUY = "buy"
    SELL = "sell"
    HOLD = "hold"


class PositionSide(StrEnum):
    LONG = "long"
    SHORT = "short"
    FLAT = "flat"


class PositionAction(StrEnum):
    OPEN_LONG = "open_long"
    OPEN_SHORT = "open_short"
    INCREASE_LONG = "increase_long"
    INCREASE_SHORT = "increase_short"
    CLOSE_LONG = "close_long"
    CLOSE_SHORT = "close_short"
    HOLD = "hold"


class ExitReasonType(StrEnum):
    STRATEGY = "strategy"
    TAKE_PROFIT = "take_profit"
    STOP_LOSS = "stop_loss"
    TRAILING_STOP = "trailing_stop"
    PARTIAL_EXIT = "partial_exit"


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
class LastPrice:
    exchange: str
    market: str
    symbol: str
    price: str
    quantity: str = ""
    event_time: int = 0
    trade_time: int = 0
    trade_id: int = 0


@dataclass(frozen=True)
class MarkPrice:
    exchange: str
    market: str
    symbol: str
    mark_price: str
    index_price: str = ""
    funding_rate: str = ""
    next_funding_time: int = 0
    event_time: int = 0


@dataclass(frozen=True)
class MarketSnapshot:
    indicator: IndicatorSnapshot
    health: DataHealth
    klines: tuple[Kline, ...] = ()
    indicator_history: tuple[IndicatorSnapshot, ...] = ()
    indicator_window: IndicatorWindowAnalysis | None = None
    last_price: LastPrice | None = None
    mark_price: MarkPrice | None = None
    window: WindowAnalysis | None = None


@dataclass(frozen=True)
class IndicatorSeriesAnalysis:
    latest: float = 0.0
    previous: float = 0.0
    change: float = 0.0
    change_pct: float = 0.0
    slope: float = 0.0
    direction: str = "unknown"
    rising_count: int = 0
    falling_count: int = 0
    minimum: float = 0.0
    maximum: float = 0.0
    range_position_pct: float = 0.0


@dataclass(frozen=True)
class SignalSeriesAnalysis:
    latest: str = ""
    previous: str = ""
    changed: bool = False
    stable_count: int = 0
    last_changed_ago: int = 0


@dataclass(frozen=True)
class IndicatorWindowAnalysis:
    sample_count: int
    values: Mapping[str, IndicatorSeriesAnalysis] = MappingProxyType({})
    signals: Mapping[str, SignalSeriesAnalysis] = MappingProxyType({})


@dataclass(frozen=True)
class WindowAnalysis:
    sample_count: int
    lookback: int
    close_change_pct: float = 0.0
    recent_change_pct: float = 0.0
    high: str = ""
    low: str = ""
    range_position_pct: float = 0.0
    close_slope_pct: float = 0.0
    volume_ratio: float = 0.0
    trend: str = "unknown"
    momentum: str = "unknown"
    volume_state: str = "unknown"


@dataclass(frozen=True)
class MarketAnalysis:
    summary: str
    trend: str = ""
    momentum: str = ""
    volatility: str = ""
    volume: str = ""
    risk: str = ""
    key_levels: Mapping[str, str] = MappingProxyType({})


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


@dataclass(frozen=True)
class ExitRule:
    rule_type: ExitReasonType
    reason: str
    trigger_price: str = ""
    size_pct: float = 1.0
    metadata: Mapping[str, str] = MappingProxyType({})


@dataclass(frozen=True)
class StrategyResult:
    strategy_name: str
    signal: Signal
    analysis: MarketAnalysis
    exit_rules: tuple[ExitRule, ...] = ()
    position_plan: PositionPlan | None = None


@dataclass(frozen=True)
class PositionState:
    exchange: str
    market: str
    symbol: str
    strategy_name: str
    position_id: str = ""
    side: PositionSide = PositionSide.FLAT
    size: float = 0.0
    initial_size: float = 0.0
    entry_price: str = ""
    highest_price: str = ""
    lowest_price: str = ""
    exit_rules: tuple[ExitRule, ...] = ()
    entry_time: int = 0
    entry_reason: str = ""
    updated_at: int = 0

    def is_flat(self) -> bool:
        return self.side == PositionSide.FLAT or self.size <= 0


@dataclass(frozen=True)
class PositionPlan:
    action: PositionAction
    target_side: PositionSide
    target_size: float
    reason: str
    exit_size: float = 0.0
    exit_reason_type: ExitReasonType = ExitReasonType.STRATEGY
    exit_rules: tuple[ExitRule, ...] = ()
    triggered_exit_rule: ExitRule | None = None


@dataclass(frozen=True)
class StrategyDecision:
    results: tuple[StrategyResult, ...]
    position_plan: PositionPlan | None = None


@dataclass(frozen=True)
class ClosedPosition:
    position_id: str
    exchange: str
    market: str
    symbol: str
    strategy_name: str
    side: PositionSide
    size: float
    initial_size: float
    entry_price: str
    exit_price: str
    exit_rules: tuple[ExitRule, ...]
    triggered_exit_rule: ExitRule | None
    entry_time: int
    exit_time: int
    entry_reason: str
    exit_reason: str
    exit_reason_type: ExitReasonType
    realized_pnl: float
    realized_pnl_pct: float
    margin: float = 0.0
    leverage: float = 1.0
    fee: float = 0.0
    net_pnl: float = 0.0
    net_pnl_pct: float = 0.0
    remaining_size_after_exit: float = 0.0
    is_final_exit: bool = True
    total_realized_pnl: float = 0.0
    total_fee: float = 0.0
    total_net_pnl: float = 0.0
    total_net_pnl_pct: float = 0.0
