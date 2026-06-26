from __future__ import annotations

import asyncio
import os

from alphaflow.logging import LoggingConfig, setup_logging
from alphaflow.strategy.runner import StrategyTarget, build_default_runner


def main() -> None:
    setup_logging(LoggingConfig())
    redis_url = os.getenv("ALPHAFLOW_REDIS_URL", "redis://localhost:6380/0")
    interval_seconds = float(os.getenv("ALPHAFLOW_STRATEGY_INTERVAL_SECONDS", "10"))
    targets = [
        StrategyTarget(
            exchange=os.getenv("ALPHAFLOW_STRATEGY_EXCHANGE", "binance"),
            market=os.getenv("ALPHAFLOW_STRATEGY_MARKET", "um"),
            symbol=os.getenv("ALPHAFLOW_STRATEGY_SYMBOL", "ETHUSDT"),
            interval=os.getenv("ALPHAFLOW_STRATEGY_KLINE_INTERVAL", "1m"),
        )
    ]
    runner = build_default_runner(redis_url)
    asyncio.run(runner.run_forever(targets, interval_seconds))


if __name__ == "__main__":
    main()
