# AlphaFlow

AlphaFlow is an intelligent trading system project. The current implementation focuses on real-time market data collection, derived K-line aggregation, technical indicator calculation, and Redis-based handoff to later strategy, backtest, and API workflows.

## Current Status

- `backend/go-service/market-data` is the active service.
- Redis is used as the current real-time data cache and service handoff layer.
- Binance and Gate are enabled in the local config by default. Bitget and Bybit adapters exist but are disabled by default.
- Python currently contains a minimal `alphaflow-core` service scaffold managed by `uv`.
- Frontend is reserved for a future React + TypeScript application.

## Project Layout

```text
AlphaFlow/
  backend/
    go-service/
      market-data/                 # Go market data collector, aggregator, indicator runner
      pkg/                         # Shared Go logger, Redis client, HTTP client, constants
    python-service/
      alphaflow-core/              # Python service scaffold managed by uv
  docs/
    architecture.md                # Architecture boundaries and stage planning
    market-data.md                 # Current market-data service notes
  frontend/                        # Reserved for future frontend work
  data/                            # Local runtime data, including Redis data
  logs/                            # Local service logs
```

## Read This First

- [docs/architecture.md](docs/architecture.md) explains service boundaries, current architecture, and planned modules.
- [docs/market-data.md](docs/market-data.md) explains the implemented Go market-data service, Redis keys, indicators, local run commands, and known limits.
- [Makefile](Makefile) is the main local command entry point.

## Local Commands

Start Redis:

```sh
make redis-up
```

Run the Go market-data service locally:

```sh
make go-market-data-run
```

Start Redis and market-data with Docker Compose:

```sh
make stack-up
```

Run Go tests:

```sh
make go-market-data-test
```

Run Python checks:

```sh
make py-check
```

Run all available checks:

```sh
make check
```

## Current Data Flow

```text
Exchange REST/WebSocket
  -> Go market-data collector
  -> Redis
  -> Python strategy/backtest/API workflows
```

Inside `market-data`, K-lines can also flow through derived aggregation and indicator calculation:

```text
Raw K-lines
  -> derived K-line aggregation
  -> indicator runner
  -> latest indicator snapshot in Redis
```

## Configuration

The local Go market-data config lives at:

```text
backend/go-service/market-data/configs/local.toml
```

The config currently controls enabled exchanges, symbols, logging, and WebSocket reconnect delay. Several runtime values such as indicator scan interval, retention limits, and exchange interval lists are still code-level constants.

## Important Notes

- Redis is not intended to be the final long-term historical market data store.
- Strategy, backtest, API, execution, risk, and frontend workflows are planned but not implemented as production modules yet.
- Keep documentation aligned with actual implementation. Mark future ideas as planning items instead of current behavior.
