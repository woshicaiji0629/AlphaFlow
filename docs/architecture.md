# Architecture

This document records the current AlphaFlow architecture and the intended service boundaries. It should describe what exists today separately from planned modules.

## Current Stage

AlphaFlow is currently in the market-data foundation stage.

Implemented:

- Go `market-data` service for exchange market data collection.
- Redis cache and handoff layer.
- Derived K-line aggregation for missing intervals such as `10m`.
- Technical indicator calculation from closed K-lines.
- Minimal Python `alphaflow-core` scaffold.

Not implemented as production modules yet:

- Strategy orchestration.
- Backtest service.
- Management API.
- Execution service.
- Real-time risk service.
- Frontend.
- Durable long-term market data storage.

## Repository Structure

```text
AlphaFlow/
  frontend/                         # Reserved for future React + TypeScript frontend
  backend/
    python-service/                 # Python services, each with its own dependencies
      alphaflow-core/               # Current Python scaffold managed by uv
    go-service/                     # Go services under one Go module
      market-data/                  # Current active market data service
      pkg/                          # Shared logger, Redis, HTTP, constants packages
  docs/                             # Project architecture and service notes
```

Each service should keep its own configuration, tests, runtime entry point, and dependency management style.

## Service Boundaries

Python is intended for:

- Strategy research and signal generation.
- Backtesting and reports.
- AI and machine learning workflows.
- Future management API.
- Task orchestration and batch jobs.
- Data analysis and exploration.
- Risk configuration, audit, and reporting workflows.

Go is intended for:

- Long-running real-time infrastructure.
- Exchange REST/WebSocket connections.
- Low-latency IO.
- Market data collection.
- K-line aggregation and derived market data.
- Future execution, real-time risk, and stream gateway services.

## Current Market-Data Flow

```text
Exchange REST/WebSocket
  -> backend/go-service/market-data
  -> Redis
  -> future Python strategy/backtest/API workflows
```

The Go `market-data` service currently contains these internal responsibilities:

- `collector`: exchange REST initialization, WebSocket sync, reconnect loop, polling data sync.
- `aggregator`: derived K-line generation for intervals not provided natively by an exchange.
- `indicator`: technical indicator calculation from closed K-lines.
- `store`: Redis read/write boundary for K-lines, prices, open interest, liquidations, market status, and indicator snapshots.
- `exchange`: REST/WebSocket adapters for supported exchanges.

For detailed service behavior, see [market-data.md](market-data.md).

## Exchanges

The current adapter set includes:

- Binance USD-M futures.
- Gate USDT futures.
- Bitget USDT futures.
- Bybit linear futures.

The local config currently enables Binance and Gate, and disables Bitget and Bybit by default.

## Redis Role

Redis is currently used for:

- Real-time market data cache.
- Service-to-service handoff.
- Latest K-line and current state access.
- Recent liquidation retention.
- Latest indicator snapshot storage.

Redis is not intended to be the final long-term historical market data store. Future long-term storage options can include ClickHouse, TimescaleDB, PostgreSQL, object storage, or another fit-for-purpose system.

## Future Go Services

Potential future Go services:

```text
backend/go-service/
  market-data/          # Implemented: exchange market data collection
  kline-aggregator/     # Potential extraction if aggregation grows beyond market-data
  execution/            # Future order placement and order state sync
  realtime-risk/        # Future low-latency real-time risk checks
  stream-gateway/       # Future WebSocket/SSE push gateway
```

Aggregation and indicators currently live inside `market-data`. Extract them only when there is a clear operational or ownership reason.

## Future Python Services

Potential future Python services:

```text
backend/python-service/
  alphaflow-core/       # Current scaffold; future orchestration/API entry point
  research/             # Strategy research and experiments
  backtest/             # Backtest service
  model-service/        # AI/model signal service
  reporting/            # Reports and analysis
```

## Runtime Reliability Conventions

- Services use structured logging through `backend/go-service/pkg/logger`.
- Redis access is centralized through `backend/go-service/pkg/redisclient` and service-level store implementations.
- Long-running Go services should accept context cancellation and exit gracefully on SIGINT/SIGTERM.
- WebSocket collectors reconnect after `reconnect_delay`.
- Collector startup and WebSocket reconnects should perform REST compensation before real-time sync.
- Polling task failures should be logged without stopping the whole service unless the failure makes the service unusable.

## Current Decisions

- `market-data` writes stable internal market data models to Redis.
- Strategy and API consumers should read from Redis for the current stage.
- Derived `10m` K-lines are generated inside Go from smaller source intervals.
- Indicator calculation uses closed K-lines only.
- Latest indicator snapshots are stored as Redis string JSON values.

## Open Decisions

- Whether derived `10m` K-lines should always use `5m`, or use `1m` for some exchanges and use cases.
- Which durable store should hold long-term historical market data.
- How much indicator history should be retained in Redis or durable storage.
- Which service owns strategy signal generation.
- When to introduce execution and real-time risk services.
- Whether indicator parameters should become runtime config.
