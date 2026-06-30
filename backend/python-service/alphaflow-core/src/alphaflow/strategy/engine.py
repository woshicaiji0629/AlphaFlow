from collections.abc import Iterable, Mapping, Sequence
from dataclasses import replace
from typing import Protocol

from alphaflow.strategy.models import (
    MarketSnapshot,
    PositionState,
    Signal,
    SignalSide,
    StrategyDecision,
    StrategyResult,
)
from alphaflow.strategy.position import PositionManager
from alphaflow.strategy.rules import RuleStrategy, TrendMomentumStrategy


class Strategy(Protocol):
    name: str

    def evaluate(self, snapshot: MarketSnapshot) -> StrategyResult: ...


class StrategyEngine:
    def __init__(
        self,
        strategies: Sequence[Strategy] | None = None,
        position_manager: PositionManager | None = None,
    ) -> None:
        self._strategies = tuple(strategies or (RuleStrategy(), TrendMomentumStrategy()))
        self._position_manager = position_manager or PositionManager()

    @property
    def strategy_names(self) -> tuple[str, ...]:
        return tuple(strategy.name for strategy in self._strategies)

    def evaluate(
        self,
        snapshot: MarketSnapshot,
        positions: Mapping[str, PositionState] | None = None,
    ) -> StrategyDecision:
        position_by_strategy = positions or {}
        results = tuple(
            self._evaluate_strategy(snapshot, strategy, position_by_strategy.get(strategy.name))
            for strategy in self._strategies
        )
        return StrategyDecision(results=results, position_plan=primary_plan(results))

    def evaluate_many(self, snapshots: Iterable[MarketSnapshot]) -> list[StrategyDecision]:
        return [self.evaluate(snapshot) for snapshot in snapshots]

    def _evaluate_strategy(
        self,
        snapshot: MarketSnapshot,
        strategy: Strategy,
        position: PositionState | None,
    ) -> StrategyResult:
        result = strategy.evaluate(snapshot)
        plan = self._position_manager.plan(
            strategy.name,
            result.signal,
            position,
            current_price=current_price(snapshot),
            exit_rules=result.exit_rules,
        )
        return replace(result, position_plan=plan)


def primary_signal(results: Sequence[StrategyResult]) -> Signal | None:
    actionable = [result.signal for result in results if result.signal.side != SignalSide.HOLD]
    if not actionable:
        return None
    return max(actionable, key=lambda signal: signal.confidence)


def primary_plan(results: Sequence[StrategyResult]):
    plans = [result.position_plan for result in results if result.position_plan is not None]
    if not plans:
        return None
    return plans[0]


def current_price(snapshot: MarketSnapshot) -> str:
    if snapshot.last_price is not None and snapshot.last_price.price:
        return snapshot.last_price.price
    if snapshot.mark_price is not None and snapshot.mark_price.mark_price:
        return snapshot.mark_price.mark_price
    if snapshot.klines:
        return snapshot.klines[-1].close
    return ""
