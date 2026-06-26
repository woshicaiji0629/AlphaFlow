from collections.abc import Iterable

from alphaflow.strategy.models import MarketSnapshot, Signal
from alphaflow.strategy.rules import RuleStrategy


class StrategyEngine:
    def __init__(self, strategy: RuleStrategy | None = None) -> None:
        self._strategy = strategy or RuleStrategy()

    def evaluate(self, snapshot: MarketSnapshot) -> Signal:
        return self._strategy.evaluate(snapshot)

    def evaluate_many(self, snapshots: Iterable[MarketSnapshot]) -> list[Signal]:
        return [self.evaluate(snapshot) for snapshot in snapshots]
