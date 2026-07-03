from __future__ import annotations

import asyncio
import logging
import uuid
from collections.abc import Sequence
from dataclasses import replace
from typing import Protocol

from alphaflow.market_data.reader import AsyncMarketDataReader, MarketDataNotReadyError
from alphaflow.strategy.engine import StrategyEngine
from alphaflow.strategy.models import (
    ClosedPosition,
    ExitReasonType,
    ExitRule,
    MarketSnapshot,
    PositionAction,
    PositionPlan,
    PositionSide,
    PositionState,
    StrategyContext,
    StrategyDecision,
    StrategyResult,
    StrategyTarget,
)
from alphaflow.strategy.position_store import (
    PositionHistoryStore,
    PositionStore,
    PostgresPositionHistoryStore,
    RedisPositionStore,
)

DEFAULT_POSITION_MARGIN = 100.0
DEFAULT_LEVERAGE = 100.0
DEFAULT_FEE_RATE = 0.0006


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
        position_store: PositionStore | None = None,
        history_store: PositionHistoryStore | None = None,
        logger: logging.Logger | None = None,
    ) -> None:
        self._reader = reader
        self._engine = engine or StrategyEngine()
        self._position_store = position_store
        self._history_store = history_store
        self._logger = logger or logging.getLogger(__name__)
        self._history_initialized = False

    async def run_once(self, targets: Sequence[StrategyTarget]) -> list[StrategyDecision]:
        await self.initialize()
        decisions: list[StrategyDecision] = []
        for target in targets:
            contexts = await self.read_contexts(target)
            if not contexts:
                continue
            decision = self._engine.evaluate(contexts)
            await self.apply_decision(contexts, decision)
            decisions.append(decision)
            self.log_decision(decision)
        return decisions

    async def read_contexts(
        self,
        target: StrategyTarget,
    ) -> list[StrategyContext]:
        contexts: list[StrategyContext] = []
        positions = await self.read_positions(target)
        for strategy in self._engine.strategies:
            intervals = strategy.required_intervals(target)
            snapshots = await self.read_snapshots(target, intervals)
            entry_snapshot = snapshots.get(target.interval)
            if entry_snapshot is None:
                continue
            position = positions.get(strategy.name)
            if position is not None:
                position = await self.refresh_position(entry_snapshot, position)
            contexts.append(
                StrategyContext(
                    strategy_name=strategy.name,
                    target=target,
                    snapshots=snapshots,
                    position=position,
                )
            )
        return contexts

    async def read_snapshots(
        self,
        target: StrategyTarget,
        intervals: Sequence[str],
    ) -> dict[str, MarketSnapshot]:
        snapshots: dict[str, MarketSnapshot] = {}
        for interval in intervals:
            try:
                snapshots[interval] = await self._reader.read_snapshot(
                    target.exchange,
                    target.market,
                    target.symbol,
                    interval,
                )
            except MarketDataNotReadyError as exc:
                message = (
                    "market data not ready"
                    if interval == target.interval
                    else "strategy context not ready"
                )
                self._logger.warning(
                    message,
                    extra={
                        "exchange": target.exchange,
                        "market": target.market,
                        "symbol": target.symbol,
                        "interval": interval,
                        "error": str(exc),
                    },
                )
                continue
        return snapshots

    async def initialize(self) -> None:
        if self._history_store is None or self._history_initialized:
            return
        await self._history_store.initialize()
        self._history_initialized = True

    async def read_positions(self, target: StrategyTarget) -> dict[str, PositionState]:
        if self._position_store is None:
            return {}
        positions: dict[str, PositionState] = {}
        for strategy_name in self._engine.strategy_names:
            position = await self._position_store.get_active_position(
                target.exchange,
                target.market,
                target.symbol,
                strategy_name,
            )
            if position is not None and not position.is_flat():
                positions[strategy_name] = position
        return positions

    async def refresh_positions(
        self,
        snapshot: MarketSnapshot,
        positions: dict[str, PositionState],
    ) -> dict[str, PositionState]:
        if self._position_store is None:
            return positions
        refreshed: dict[str, PositionState] = {}
        price = current_price(snapshot)
        for strategy_name, position in positions.items():
            updated = refresh_position_extremes(position, price)
            refreshed[strategy_name] = updated
            if updated != position:
                await self._position_store.save_active_position(updated)
        return refreshed

    async def refresh_position(
        self,
        snapshot: MarketSnapshot,
        position: PositionState,
    ) -> PositionState:
        if self._position_store is None:
            return position
        updated = refresh_position_extremes(position, current_price(snapshot))
        if updated != position:
            await self._position_store.save_active_position(updated)
        return updated

    async def apply_decision(
        self,
        contexts: Sequence[StrategyContext],
        decision: StrategyDecision,
    ) -> None:
        if self._position_store is None:
            return
        context_by_strategy = {context.strategy_name: context for context in contexts}
        for result in decision.results:
            context = context_by_strategy.get(result.strategy_name)
            if context is None:
                continue
            snapshot = context.snapshots[context.target.interval]
            plan = result.position_plan
            if plan is None:
                continue
            if plan.action in {PositionAction.OPEN_LONG, PositionAction.OPEN_SHORT}:
                await self._position_store.save_active_position(open_position(snapshot, result))
                continue
            if plan.action in {PositionAction.CLOSE_LONG, PositionAction.CLOSE_SHORT}:
                active = await self._position_store.get_active_position(
                    result.signal.exchange,
                    result.signal.market,
                    result.signal.symbol,
                    result.strategy_name,
                )
                if active is None:
                    continue
                if self._history_store is None:
                    self._logger.warning(
                        "position close skipped because history store is not configured",
                        extra={
                            "exchange": active.exchange,
                            "market": active.market,
                            "symbol": active.symbol,
                            "strategy": active.strategy_name,
                        },
                    )
                    continue
                await self.apply_close_plan(snapshot, active, plan)

    async def apply_close_plan(
        self,
        snapshot: MarketSnapshot,
        active: PositionState,
        plan: PositionPlan,
    ) -> None:
        if self._position_store is None or self._history_store is None:
            return
        closed = close_position(
            snapshot,
            active,
            plan.reason,
            plan.exit_reason_type,
            plan.triggered_exit_rule,
            plan.exit_size,
        )
        remaining = remaining_position(
            active,
            closed.size,
            current_price(snapshot),
            plan.triggered_exit_rule,
        )
        closed = await self.with_position_totals(closed, remaining)
        await self._history_store.save_closed_position(closed)
        if remaining is None:
            await self._position_store.clear_active_position(active)
            return
        await self._position_store.save_active_position(remaining)

    async def with_position_totals(
        self,
        closed: ClosedPosition,
        remaining: PositionState | None,
    ) -> ClosedPosition:
        if self._history_store is None:
            return closed
        previous = await self._history_store.list_closed_positions(closed.position_id)
        total_realized = closed.realized_pnl + sum(item.realized_pnl for item in previous)
        total_fee = closed.fee + sum(item.fee for item in previous)
        total_net = closed.net_pnl + sum(item.net_pnl for item in previous)
        total_net_pct = total_net / DEFAULT_POSITION_MARGIN * 100
        return replace(
            closed,
            remaining_size_after_exit=remaining.size if remaining is not None else 0.0,
            is_final_exit=remaining is None,
            total_realized_pnl=total_realized,
            total_fee=total_fee,
            total_net_pnl=total_net,
            total_net_pnl_pct=total_net_pct,
        )

    async def run_forever(
        self,
        targets: Sequence[StrategyTarget],
        interval_seconds: float,
    ) -> None:
        while True:
            await self.run_once(targets)
            await asyncio.sleep(interval_seconds)

    def log_decision(self, decision: StrategyDecision) -> None:
        for result in decision.results:
            self.log_result(result, decision)

    def log_result(self, result: StrategyResult, decision: StrategyDecision) -> None:
        signal = result.signal
        plan = decision.position_plan
        self._logger.info(
            "strategy signal",
            extra={
                "strategy": result.strategy_name,
                "exchange": signal.exchange,
                "market": signal.market,
                "symbol": signal.symbol,
                "interval": signal.interval,
                "side": signal.side.value,
                "score": signal.score,
                "confidence": signal.confidence,
                "reason": signal.reason,
                "analysis": result.analysis.summary,
                "position_action": plan.action.value if plan is not None else "",
                "target_side": plan.target_side.value if plan is not None else "",
                "target_size": plan.target_size if plan is not None else 0.0,
                "open_time": signal.open_time,
                "updated_at": signal.updated_at,
            },
        )


def build_default_runner(
    redis_url: str,
    postgres_dsn: str = "",
) -> StrategyRunner:
    history_store = PostgresPositionHistoryStore(postgres_dsn) if postgres_dsn else None
    return StrategyRunner(
        AsyncMarketDataReader.from_url(redis_url),
        position_store=RedisPositionStore.from_url(redis_url),
        history_store=history_store,
    )


def open_position(snapshot: MarketSnapshot, result: StrategyResult) -> PositionState:
    plan = result.position_plan
    if plan is None:
        raise ValueError("position plan is required to open a position")
    side = PositionSide.LONG if plan.action == PositionAction.OPEN_LONG else PositionSide.SHORT
    return PositionState(
        exchange=result.signal.exchange,
        market=result.signal.market,
        symbol=result.signal.symbol,
        strategy_name=result.strategy_name,
        position_id=uuid.uuid4().hex,
        side=side,
        size=plan.target_size,
        initial_size=plan.target_size,
        entry_price=current_price(snapshot),
        highest_price=current_price(snapshot),
        lowest_price=current_price(snapshot),
        exit_rules=plan.exit_rules,
        entry_time=result.signal.updated_at or result.signal.open_time,
        entry_reason=plan.reason,
        updated_at=result.signal.updated_at,
    )


def close_position(
    snapshot: MarketSnapshot,
    position: PositionState,
    exit_reason: str,
    exit_reason_type: ExitReasonType,
    triggered_exit_rule: ExitRule | None,
    exit_size: float,
) -> ClosedPosition:
    exit_price = current_price(snapshot)
    closed_size = exit_size if 0 < exit_size < position.size else position.size
    realized_pnl, realized_pnl_pct = calculate_pnl(position, exit_price, closed_size)
    exit_time = snapshot.indicator.updated_at or snapshot.indicator.close_time
    margin = DEFAULT_POSITION_MARGIN * size_ratio(position, closed_size)
    fee = calculate_fee(position.entry_price, exit_price, margin)
    net_pnl = realized_pnl - fee
    net_pnl_pct = net_pnl / margin * 100 if margin else 0.0
    return ClosedPosition(
        position_id=position.position_id,
        exchange=position.exchange,
        market=position.market,
        symbol=position.symbol,
        strategy_name=position.strategy_name,
        side=position.side,
        size=closed_size,
        initial_size=position.initial_size,
        entry_price=position.entry_price,
        exit_price=exit_price,
        exit_rules=position.exit_rules,
        triggered_exit_rule=triggered_exit_rule,
        entry_time=position.entry_time,
        exit_time=exit_time,
        entry_reason=position.entry_reason,
        exit_reason=exit_reason,
        exit_reason_type=exit_reason_type,
        realized_pnl=realized_pnl,
        realized_pnl_pct=realized_pnl_pct,
        margin=margin,
        leverage=DEFAULT_LEVERAGE,
        fee=fee,
        net_pnl=net_pnl,
        net_pnl_pct=net_pnl_pct,
    )


def current_price(snapshot: MarketSnapshot) -> str:
    if snapshot.last_price is not None and snapshot.last_price.price:
        return snapshot.last_price.price
    if snapshot.mark_price is not None and snapshot.mark_price.mark_price:
        return snapshot.mark_price.mark_price
    if snapshot.klines:
        return snapshot.klines[-1].close
    return "0"


def calculate_pnl(
    position: PositionState,
    exit_price: str,
    closed_size: float,
) -> tuple[float, float]:
    try:
        entry = float(position.entry_price)
        exit_value = float(exit_price)
    except ValueError:
        return 0.0, 0.0
    if entry == 0:
        return 0.0, 0.0
    margin = DEFAULT_POSITION_MARGIN * size_ratio(position, closed_size)
    notional = margin * DEFAULT_LEVERAGE
    if position.side == PositionSide.LONG:
        pnl = notional * (exit_value - entry) / entry
    elif position.side == PositionSide.SHORT:
        pnl = notional * (entry - exit_value) / entry
    else:
        return 0.0, 0.0
    pnl_pct = pnl / margin * 100 if margin else 0.0
    return pnl, pnl_pct


def size_ratio(position: PositionState, closed_size: float) -> float:
    base_size = position.initial_size if position.initial_size > 0 else position.size
    if base_size <= 0:
        return 0.0
    return max(0.0, min(1.0, closed_size / base_size))


def calculate_fee(entry_price: str, exit_price: str, margin: float) -> float:
    try:
        entry = float(entry_price)
        exit_value = float(exit_price)
    except ValueError:
        return 0.0
    if entry <= 0 or exit_value <= 0:
        return 0.0
    entry_notional = margin * DEFAULT_LEVERAGE
    exit_notional = entry_notional * exit_value / entry
    return (entry_notional + exit_notional) * DEFAULT_FEE_RATE


def remaining_position(
    position: PositionState,
    closed_size: float,
    price: str,
    triggered_exit_rule: ExitRule | None,
) -> PositionState | None:
    remaining_size = round(position.size - closed_size, 6)
    if remaining_size <= 0:
        return None
    remaining_rules = tuple(rule for rule in position.exit_rules if rule != triggered_exit_rule)
    if triggered_exit_rule is None:
        remaining_rules = position.exit_rules
    return refresh_position_extremes(
        replace(position, size=remaining_size, exit_rules=remaining_rules),
        price,
    )


def refresh_position_extremes(position: PositionState, price: str) -> PositionState:
    value = optional_float(price)
    if value is None:
        return position
    highest = max(optional_float(position.highest_price) or value, value)
    lowest = min(optional_float(position.lowest_price) or value, value)
    return replace(
        position,
        highest_price=format_price(highest),
        lowest_price=format_price(lowest),
        exit_rules=tuple(
            refresh_exit_rule(position.side, rule, highest, lowest) for rule in position.exit_rules
        ),
    )


def refresh_exit_rule(
    side: PositionSide,
    rule: ExitRule,
    highest: float,
    lowest: float,
) -> ExitRule:
    if rule.rule_type != ExitReasonType.TRAILING_STOP:
        return rule
    reference = highest if side == PositionSide.LONG else lowest
    metadata = dict(rule.metadata)
    metadata["reference_price"] = format_price(reference)
    return replace(rule, metadata=metadata)


def optional_float(value: str) -> float | None:
    if value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


def format_price(value: float) -> str:
    return f"{value:.12g}"
