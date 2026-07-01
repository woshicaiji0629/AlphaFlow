from collections.abc import Iterable, Sequence

from alphaflow.strategy.models import (
    Signal,
    SignalSide,
    StrategyContext,
    StrategyDecision,
    StrategyResult,
)
from alphaflow.strategy.strategies import Strategy
from alphaflow.strategy.strategies import default_strategies as registered_strategies


class StrategyEngine:
    def __init__(self, strategies: Sequence[Strategy] | None = None) -> None:
        self._strategies = tuple(strategies or default_strategies())

    @property
    def strategies(self) -> tuple[Strategy, ...]:
        return self._strategies

    @property
    def strategy_names(self) -> tuple[str, ...]:
        return tuple(strategy.name for strategy in self._strategies)

    def evaluate(self, contexts: Iterable[StrategyContext]) -> StrategyDecision:
        strategy_by_name = {strategy.name: strategy for strategy in self._strategies}
        results = tuple(
            strategy_by_name[context.strategy_name].evaluate(context)
            for context in contexts
            if context.strategy_name in strategy_by_name
        )
        return StrategyDecision(results=results, position_plan=primary_plan(results))


def default_strategies() -> tuple[Strategy, ...]:
    return tuple(registered_strategies())


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
