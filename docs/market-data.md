# Market Data Service

This document summarizes the current Go `market-data` service so future readers can understand the implementation without scanning every package first.

## Location

```text
backend/go-service/market-data/
```

Entry point:

```text
backend/go-service/market-data/cmd/market-data/main.go
```

Local config:

```text
backend/go-service/market-data/configs/local.toml
```

## Responsibilities

The service currently handles:

- Exchange REST initialization.
- Exchange WebSocket market data sync.
- WebSocket reconnect and REST compensation.
- Latest price, mark price, book ticker, open interest, liquidation, and K-line writes to Redis.
- Derived K-line aggregation for selected missing intervals.
- Technical indicator calculation from closed K-lines.
- Market availability status tracking.

It does not currently handle:

- Long-term historical storage.
- Trading strategy execution.
- Order placement.
- Real-time risk checks.
- Public API serving.

## Local Storage Services

Local Docker Compose provides:

- Redis for latest state, real-time cache, and low-latency service handoff.
- ClickHouse for future K-line, indicator, and market event time-series history.

ClickHouse is currently available as infrastructure only. The Go service still writes market data and indicator snapshots to Redis; ClickHouse table schema and write paths should be added in a separate change.

## Package Map

```text
market-data/
  cmd/market-data/           # Process entry point
  configs/                   # Local TOML config
  internal/app/              # Runtime assembly and goroutine orchestration
  internal/collector/        # REST bootstrap, WebSocket sync, polling tasks
  internal/aggregator/       # Derived K-line aggregation
  internal/indicator/        # Technical indicator calculations and runner
  internal/store/            # Redis persistence boundary
  internal/model/            # Internal data models and Redis key helpers
  internal/exchange/         # Exchange interfaces and adapters
```

Shared Go packages live under:

```text
backend/go-service/pkg/
```

## Runtime Assembly

`internal/app` builds:

- One Redis-backed store.
- One collector per enabled exchange.
- One K-line aggregator.
- One indicator runner.

Collectors run in restart loops. The aggregator and indicator runner run on fixed scan intervals. Context cancellation stops all long-running loops.

## Exchanges

Adapter packages currently exist for:

- `binance`
- `gate`
- `bitget`
- `bybit`

Default local config:

- Binance enabled with `ETHUSDT`.
- Gate enabled with `ETH_USDT`.
- Bitget disabled.
- Bybit disabled.

Supported interval lists are currently code-level constants:

- Binance: `1m`, `3m`, `5m`, `15m`, `30m`, `1h`, `2h`, `4h`
- Gate: `1m`, `5m`, `15m`, `30m`, `1h`, `4h`
- Bitget: `1m`, `5m`, `15m`, `30m`, `1h`, `4h`
- Bybit: `1m`, `3m`, `5m`, `15m`, `30m`, `1h`, `2h`, `4h`

## Derived K-Lines

The aggregator creates missing intervals from existing smaller intervals.

Current rules:

- Binance: `5m -> 10m`
- Bybit: `5m -> 10m`
- Gate: `1m -> 3m`, `5m -> 10m`, `1h -> 2h`
- Bitget: `1m -> 3m`, `5m -> 10m`, `1h -> 2h`

Aggregation only writes a derived K-line when all required source K-lines are present, closed, and contiguous.

## Indicators

The indicator runner reads recent closed K-lines and writes the latest indicator snapshot per exchange, market, symbol, and interval.

The current indicator set includes:

- Moving averages: SMA, EMA, WMA, HMA, VWMA, DEMA, TEMA, KAMA, Alligator.
- Momentum and oscillators: RSI, MACD, KDJ, Stochastic, Stoch RSI, SKDJ, CCI, Williams %R, ROC.
- Volatility and trend: ATR, NATR, ADX, DI, Bollinger Bands, Donchian, Supertrend, AlphaTrend, PSAR.
- Volume and money flow: volume moving averages, OBV, VWAP, rolling VWAP, MFI, CMF, accumulation/distribution, PVT.
- Price action and structure: candle patterns, Heikin Ashi, support/resistance, Fibonacci, pivot points, Ichimoku, smart money signals.
- Derived candle features: change percent, amplitude percent, body ratio, shadow ratios, volume ratios.

Indicator calculation currently uses closed K-lines only. Latest snapshots are stored in Redis and are not yet retained as historical series.

## Redis Keys

Redis key shape:

```text
{exchange_code}:{market}:{type}:{symbol}:{extra}
```

Known exchange code:

- `bn` = Binance
- Other exchange names currently use their raw exchange name unless a specific mapping is added.

Common types:

- `k` = K-line sorted set
- `lp` = latest price
- `mp` = mark price
- `bt` = book ticker
- `oi` = open interest
- `liq` = liquidation sorted set
- `ind` = latest indicator snapshot

Examples:

```text
bn:um:k:ETHUSDT:1m
bn:um:lp:ETHUSDT
bn:um:mp:ETHUSDT
bn:um:bt:ETHUSDT
bn:um:oi:ETHUSDT
bn:um:liq:ETHUSDT
bn:um:ind:ETHUSDT:1m
```

K-lines and liquidations use Redis sorted sets. The score is the event/open time in milliseconds. Latest state and indicator snapshots use Redis string JSON values.

## Retention

Current code-level defaults:

- K-lines retain 500 entries per key.
- K-line TTL is 7 days.
- Liquidations retain 200 entries per key.
- Liquidation TTL is 24 hours.
- Latest price, mark price, book ticker, and indicator TTL is 24 hours.
- Polling state such as open interest TTL is 24 hours.

These values are not yet TOML-configurable.

## Local Runbook

Start Redis:

```sh
make redis-up
```

Start ClickHouse:

```sh
make clickhouse-up
```

Run market-data locally:

```sh
make go-market-data-run
```

Start Redis, ClickHouse, and market-data with Docker Compose:

```sh
make stack-up
```

Tail market-data Docker logs:

```sh
make market-data-logs
```

Open Redis CLI:

```sh
make redis-cli
```

Open ClickHouse client:

```sh
make clickhouse-client
```

Run Go tests:

```sh
make go-market-data-test
```

## Configuration Notes

`configs/local.toml` currently controls:

- Enabled exchanges.
- Symbols.
- Logging service name, level, format, output, file rotation.
- WebSocket reconnect delay.

Runtime values still defined in code include:

- REST bootstrap limit.
- Exchange base URLs.
- Exchange interval lists.
- Mark price polling interval.
- Open interest polling interval.
- Retention limits and TTLs.
- Aggregation scan interval.
- Indicator scan interval.
- Indicator lookback periods.

## Current Limitations

- Redis is not a durable historical data store.
- Indicator snapshots only store the latest values.
- Indicator parameters and groups are not yet runtime-configurable.
- Indicators currently use K-line OHLCV data only; open interest, liquidation, mark price premium, and order book imbalance are not yet part of indicator calculation.
- The service does not expose an HTTP API.
- The local README and architecture docs should be updated when service responsibilities move or new production modules are introduced.

## Useful Next Improvements

- Make indicator scan interval, lookback periods, and indicator groups configurable.
- Add optional indicator history retention.
- Add data quality signals such as gap detection, stale data, and insufficient data.
- Add derived indicators from open interest, liquidation, mark price, and book ticker data.
- Decide the durable historical market data store.
