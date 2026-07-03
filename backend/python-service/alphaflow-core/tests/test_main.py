from collections.abc import Sequence

import alphaflow.main as main_module
from alphaflow.strategy.runner import StrategyTarget


class FakeRunner:
    def __init__(self) -> None:
        self.targets: Sequence[StrategyTarget] = []
        self.interval_seconds = 0.0

    async def run_forever(
        self,
        targets: Sequence[StrategyTarget],
        interval_seconds: float,
    ) -> None:
        self.targets = targets
        self.interval_seconds = interval_seconds


def test_main_builds_default_runner(monkeypatch) -> None:  # type: ignore[no-untyped-def]
    fake = FakeRunner()
    captured: dict[str, str] = {}

    monkeypatch.setattr(main_module, "setup_logging", lambda _: None)
    monkeypatch.setattr(
        main_module,
        "build_default_runner",
        lambda redis_url, postgres_dsn: (
            captured.update(
                {
                    "redis_url": redis_url,
                    "postgres_dsn": postgres_dsn,
                }
            )
            or fake
        ),
    )

    main_module.main()

    assert fake.interval_seconds == 10
    assert captured["postgres_dsn"] == ""
    assert fake.targets == [
        StrategyTarget(exchange="binance", market="um", symbol="ETHUSDT", interval="3m")
    ]
