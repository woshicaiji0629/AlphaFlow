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
- WebSocket reconnect with exponential backoff.
- WebSocket stream sharding by code-level max streams per connection.
- WebSocket handler event queue for store-write backpressure isolation.
- WebSocket single-message decode/dispatch error isolation.
- Latest price, mark price, book ticker, open interest, liquidation, and K-line writes to Redis.
- Closed K-line and indicator history writes to ClickHouse.
- ClickHouse retry compensation through Redis pending queues.
- Derived K-line aggregation for selected missing intervals.
- Technical indicator calculation from closed K-lines.
- Market availability status tracking.

It does not currently handle:

- Trading strategy execution.
- Order placement.
- Real-time risk checks.
- Public API serving.

## Local Storage Services

Local Docker Compose provides:

- Redis for latest state, real-time cache, and low-latency service handoff.
- ClickHouse for closed K-line and indicator time-series history.

The Go service writes real-time state to Redis first. When ClickHouse is enabled, closed K-lines and indicator snapshots are also written to ClickHouse. Failed ClickHouse writes are persisted to a Redis retry queue and retried by a background worker.

## Package Map

```text
market-data/
  cmd/market-data/           # Process entry point
  configs/                   # Local TOML config
  internal/app/              # Runtime assembly and goroutine orchestration
  internal/collector/        # REST bootstrap, WebSocket sync, polling tasks
  internal/aggregator/       # Derived K-line aggregation
  internal/indicator/        # Technical indicator calculations and runner
  internal/store/            # Redis and ClickHouse persistence boundary
  internal/model/            # Internal data models and Redis key helpers
  internal/exchange/         # Exchange interfaces and adapters
```

Shared Go packages live under:

```text
backend/go-service/pkg/
```

## Runtime Assembly

`internal/app` builds:

- One Redis-backed store with optional ClickHouse history writes.
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

Indicator calculation currently uses closed K-lines only. The runner tracks the latest calculated indicator open time per exchange, market, symbol, and interval, and skips repeated calculation for the same closed K-line.

Each indicator snapshot includes basic data quality fields:

- `values.sample_count`: number of closed K-lines used.
- `values.required_count`: minimum sample count expected by the configured moving-average periods.
- `signals.data_quality`: `ok`, `insufficient`, `gap`, `invalid_ohlc`, or `zero_volume`.
- `signals.data_quality_reason`: optional detail for non-`ok` quality states.

Latest snapshots are stored in Redis; when ClickHouse is enabled, each snapshot is also retained as historical series data.

## ClickHouse Tables

ClickHouse is used for durable analytical history, not for real-time state handoff.

Current tables:

- `market_klines`: closed K-line history keyed by exchange, market, symbol, interval, and open time.
- `indicator_snapshots`: indicator snapshot history keyed by exchange, market, symbol, interval, and open time.

Both tables use `ReplacingMergeTree(updated_at_ms)` so repeated writes for the same logical row can be deduplicated by ClickHouse merges. Price and volume fields are stored as strings to preserve exchange precision without forcing a decimal scale in the first implementation.

ClickHouse write failures do not directly break the Redis real-time path. Failed records are written to Redis queues:

```text
market-data:clickhouse:pending
market-data:clickhouse:processing
```

The background retry worker moves records from `pending` to `processing`, writes ClickHouse, and removes them after success. Records left in `processing` are recovered back to `pending` when the worker starts.

## Redis Keys

Redis key shape:

```text
{exchange_code}:{market}:{type}:{symbol}:{extra}
```

Known exchange code:

- `bn` = Binance
- Other exchange names currently use their raw exchange name unless a specific mapping is added.

Common types:

- `k` = K-line namespace
- `lp` = latest price
- `mp` = mark price
- `bt` = book ticker
- `oi` = open interest
- `liq` = liquidation sorted set
- `ind` = latest indicator snapshot
- `ind:last` = latest calculated indicator open time
- `ws` = exchange WebSocket connection health

Examples:

```text
bn:um:k:ETHUSDT:1m
bn:um:lp:ETHUSDT
bn:um:mp:ETHUSDT
bn:um:bt:ETHUSDT
bn:um:oi:ETHUSDT
bn:um:liq:ETHUSDT
bn:um:ind:ETHUSDT:1m
bn:um:ind:last:ETHUSDT:1m
bn:um:ws
```

K-lines use a Redis hash plus a sorted-set index:

```text
{base}:data   # hash field = open time in milliseconds, value = K-line JSON
{base}:idx    # sorted set member/score = open time in milliseconds
```

Liquidations use Redis sorted sets. The score is the event time in milliseconds. Latest state, indicator snapshots, indicator calculation cursors, and WebSocket health use Redis string values.

WebSocket health records include connection state, last start/stop timestamps, last error, reconnect count, and consecutive failure count. They are operational state for monitoring and troubleshooting, not durable history.

When stream sharding is enabled, each shard writes its own health key:

```text
bn:um:ws:0
bn:um:ws:1
```

Each shard status includes `shard`, `stream_count`, and `connection_count`.

## Redis Write Reduction

The Redis path is optimized to reduce repeated maintenance and latest-state writes under large symbol sets.

- K-line data is written as `HASH + ZSET index` so updating an existing open time replaces one hash field instead of rewriting a full sorted-set member payload.
- K-line trim and TTL maintenance are guarded by an in-memory `lcache.FreqCall`, so each K-line namespace performs trim/expire work at a low frequency instead of on every write.
- Liquidation trim and TTL maintenance also use `lcache.FreqCall`; liquidation events are still appended immediately, but list maintenance is not repeated for every event.
- WebSocket status writes skip identical JSON payloads for a short local TTL. This keeps Redis status keys fresh while avoiding repeated identical `SET` calls during stable connections.
- Latest last price, mark price, book ticker, open interest, and latest indicator snapshots skip identical JSON payloads for a short local TTL. This only suppresses repeated Redis latest-state writes; it does not change ClickHouse historical writes.
- Indicator open-time cursor writes are intentionally not de-duplicated by the latest-payload cache because they participate in indicator idempotency.

These caches are process-local. After a restart, Redis is repopulated by live exchange data and normal backfill/retry flows.

## Retention

Current code-level defaults:

- K-lines retain 500 entries per key.
- K-line TTL is 7 days.
- Liquidations retain 200 entries per key.
- Liquidation TTL is 24 hours.
- Latest price, mark price, book ticker, and indicator TTL is 24 hours.
- Polling state such as open interest TTL is 24 hours.
- ClickHouse retry queue retains up to 100000 pending records by default.

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

Run the collector event queue load test:

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-loadtest -symbols=50 -duration=30s -rate=5000 -store-latency=1ms
```

The load test does not connect to real exchanges, Redis, or ClickHouse. It drives collector handlers with simulated market events and a fake store latency so queue size, latest-event drops, and worker throughput can be checked before adding more live symbols.

Run the indicator runner load test:

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-indicator-loadtest -symbols=500 -lookback=200 -runs=2
```

The indicator load test simulates four exchanges with the service's current interval sets and fake K-line/store data. It measures indicator runner throughput and simulated Redis/ClickHouse write counts without writing real storage.

Run a live full-chain pressure test:

```sh
docker compose exec redis redis-cli -p 6380 FLUSHDB
docker compose exec redis redis-cli -p 6380 CONFIG RESETSTAT
docker compose exec clickhouse clickhouse-client --query "TRUNCATE TABLE IF EXISTS alphaflow.market_klines"
docker compose exec clickhouse clickhouse-client --query "TRUNCATE TABLE IF EXISTS alphaflow.indicator_snapshots"

cd backend/go-service
GO111MODULE=on go run ./market-data/cmd/market-data -config market-data/configs/live-top500.toml
```

Collect Redis pressure metrics:

```sh
docker compose exec redis redis-cli -p 6380 DBSIZE
docker compose exec redis redis-cli -p 6380 INFO stats
docker compose exec redis redis-cli -p 6380 INFO commandstats
docker compose exec redis redis-cli -p 6380 INFO memory
docker compose exec redis redis-cli -p 6380 LATENCY LATEST
docker compose exec redis redis-cli -p 6380 LLEN market-data:clickhouse:pending
docker compose exec redis redis-cli -p 6380 LLEN market-data:clickhouse:processing
```

Collect ClickHouse write metrics:

```sh
docker compose exec clickhouse clickhouse-client --query "SELECT count() FROM alphaflow.market_klines"
docker compose exec clickhouse clickhouse-client --query "SELECT count() FROM alphaflow.indicator_snapshots"
docker compose exec clickhouse clickhouse-client --query "SELECT table, sum(rows) FROM system.parts WHERE database = 'alphaflow' AND active GROUP BY table ORDER BY table"
```

Recent observed live run with `live-top500.toml` over a roughly five-minute window:

- Redis keys: 34233.
- ClickHouse `market_klines`: 44634.
- ClickHouse `indicator_snapshots`: 13537.
- ClickHouse pending queue: 0.
- ClickHouse processing queue: 0.
- Redis total commands after `CONFIG RESETSTAT`: 552168.
- Redis mid-run ops: about 4102 ops/s.
- Redis rejected connections: 0.
- Redis evicted keys: 0.
- Redis `LATENCY LATEST`: no events.
- Redis memory after the run: about 38 MiB.

These numbers are a recent local observation, not a capacity guarantee. They depend on enabled exchanges, symbol count, exchange message rate, local machine resources, and current WebSocket connection settings.

## Configuration Notes

`configs/local.toml` currently controls:

- Enabled exchanges.
- ClickHouse address, database, credentials, timeout, and retry settings.
- Symbols.
- Logging service name, level, format, output, file rotation.

WebSocket reconnect delay, stream shard size, event queue size, and event worker count are code-level operational defaults, not TOML configuration.

WebSocket adapters use a shared 4 MiB read limit. Collector stream lists are split by a code-level max streams per connection; each shard runs its own connection and reconnect backoff. Connection read failures and subscription write failures still trigger reconnects; single-message decode or dispatch failures are logged with exchange and message size and then skipped.

WebSocket handlers enqueue or coalesce validated events before returning. Background collector workers write queued events to Redis and optional ClickHouse-backed store paths. K-line and liquidation events wait for queue capacity when the queue is full. Latest-state events such as last price, mark price, book ticker, and open interest are coalesced by event type and symbol, then flushed periodically so Redis receives the newest state without being forced to write every intermediate update.

Backfill only throttles actual REST K-line fetch requests. Local checks such as reading the latest Redis open time and skipping symbols without a closed window are not delayed. The throttle is code-level and per collector, so large symbol lists recover more slowly but avoid starting with a burst of thousands of exchange REST calls.

Runtime values still defined in code include:

- REST bootstrap limit.
- Exchange base URLs.
- Exchange interval lists.
- Mark price polling interval.
- Open interest polling interval.
- Retention limits and TTLs.
- Aggregation scan interval.
- Indicator lookback periods.

## Current Limitations

- Redis is not a durable historical data store.
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
