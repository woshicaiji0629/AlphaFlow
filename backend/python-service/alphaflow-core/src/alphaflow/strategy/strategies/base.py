from collections.abc import Sequence
from typing import Protocol

from alphaflow.strategy.models import StrategyContext, StrategyResult, StrategyTarget


class Strategy(Protocol):
    name: str

    def required_intervals(self, target: StrategyTarget) -> tuple[str, ...]: ...

    def evaluate(self, context: StrategyContext) -> StrategyResult: ...


def default_strategies() -> Sequence[Strategy]:
    from alphaflow.strategy.strategies.supertrend import SupertrendStrategy

    return (SupertrendStrategy(),)
