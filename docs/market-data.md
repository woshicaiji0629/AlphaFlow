# 行情数据服务

本文档总结当前 Go `market-data` 服务，便于后续阅读者不必先扫完整包结构也能理解实现。

## 位置

```text
backend/go-service/market-data/
```

入口：

```text
backend/go-service/market-data/cmd/market-data/main.go
```

本地配置：

```text
backend/go-service/configs/market-data.local.toml
```

## 职责

该服务当前负责：

- 交易所 REST 初始化。
- 交易所 WebSocket 行情同步。
- WebSocket 重连和 REST 补偿。
- WebSocket 指数退避重连。
- 按代码级最大 stream 数对 WebSocket stream 分片。
- 使用 WebSocket handler 事件队列隔离 store 写入背压。
- 隔离 WebSocket 单条消息解码和派发错误。
- 将最新成交价、标记价格、盘口 ticker、持仓量、爆仓数据和 K 线写入 Redis。
- 将已闭合 K 线写入 ClickHouse。
- 通过 NATS JetStream pending 队列补偿 ClickHouse 写入失败。
- 为部分缺失周期生成派生 K 线。
- 基于已闭合 K 线计算技术指标。
- 基于底层指标序列生成窗口聚合特征。
- 将已收盘窗口特征和当前 K 线实时特征写入 Redis hash。
- 将已收盘窗口特征和当前未收盘 K 线实时指标发布到 NATS JetStream market snapshot bus。
- 对 K 线新鲜度、最近 K 线缺口和指标滞后进行行情健康检查。
- 跟踪市场可用状态。

它当前不负责：

- 交易策略执行。
- 订单下发。
- 实时风控检查。
- 对外 API 服务。

Go `strategy-engine` 启动时会用 Redis 特征 hash 恢复初始态，启动后主要消费该服务发布的 NATS market snapshot 更新内存态。Python `alphaflow-core` 保留为旧策略原型参考。

## 本地存储服务

本地 Docker Compose 提供：

- Redis：最新状态、实时缓存、低延迟当前态。
- NATS JetStream：队列层，本地文件存储目录为 `data/nats`。
- ClickHouse：已闭合 K 线历史。
- PostgreSQL：本地基础设施保留项，主要兼容旧 Python 原型路径。

Go 服务会先将实时状态写入 Redis。启用 ClickHouse 后，已闭合 K 线会写入 ClickHouse。ClickHouse 写入失败的记录会持久化到 NATS JetStream 重试队列，并由后台 worker 重试。

ClickHouse 访问实现位于 `pkg/clickhousemarket`。`market-data/internal/store` 只保留服务级入口和 NATS pending 队列边界，实际 ClickHouse 表初始化、批量写入和 K 线历史读取都由公共包承担。

Redis 只承担缓存和当前状态，不承担队列。`market-data` 仍会写入指标和窗口 Redis hash，用于策略引擎启动恢复、故障恢复、观测和兼容路径。策略实时主路径使用 NATS JetStream market snapshot bus。`market-data` 内部自产自销队列使用代码级约定，不暴露 stream/subject/durable 命名配置；配置只保留 NATS URL、ack wait、最大投递次数、batch 和 worker 开关等运行参数。

## 包结构

```text
market-data/
  cmd/market-data/           # 进程入口
  configs/                   # 本地 TOML 配置
  internal/app/              # 运行时组装和 goroutine 编排
  internal/collector/        # REST 启动、WebSocket 同步、轮询任务
  internal/aggregator/       # 派生 K 线聚合
  internal/indicator/        # 指标 runner，负责调度计算和写入结果
  internal/health/           # K 线和指标健康检查
  internal/store/            # Redis 和 ClickHouse 持久化边界
  internal/model/            # 内部数据模型和 Redis key 工具
  internal/exchange/         # 交易所接口和适配器
```

`market-data/internal` 的核心包按运行职责拆分，避免单文件同时承载编排、IO、缓存和计算逻辑：

- `internal/app`：`app.go` 保留进程入口编排；`runtime.go` 负责 Redis、store、collector、aggregator、indicator runner、health runner 和 market snapshot publisher 的组装；`collectors.go` 和 `rules.go` 分别承载交易所 collector 构造和规则生成。
- `internal/collector`：`collector.go` 保留类型、配置和 `Run` 主循环；`websocket.go`、`polling.go`、`backfill.go`、`event_queue.go`、`handlers.go`、`status.go` 分别承载 WebSocket、轮询、补偿、事件队列、事件处理和状态写入。
- `internal/indicator`：`runner.go` 保留 runner 类型和主计算流程；`runner_queue.go` 负责 NATS 指标任务队列消费；`window_cache.go` 负责 K 线窗口缓存、增量推进和 realtime 临时窗口；`snapshot.go` 负责 closed/realtime market snapshot 发布和指标窗口快照复用。
- `internal/store`：`market_store.go` 保留 `MarketStore` 聚合入口；`market_kline.go`、`market_latest.go`、`market_indicator.go`、`market_status.go`、`market_clickhouse.go` 按数据域拆分 Redis/ClickHouse 边界；`redis_store.go` 保留 Redis store 类型和构造，具体 Redis 写入拆到 `redis_kline.go`、`redis_latest.go`、`redis_indicator.go`、`redis_status.go` 和 `redis_liquidation.go`。

共享 Go 包位于：

```text
backend/go-service/pkg/
  exchangeclient/            # 交易所 REST K 线请求客户端
  clickhousemarket/          # ClickHouse K 线历史读写
  indicatorcalc/             # 纯技术指标计算，不依赖 market-data 存储
  indicatorwindow/           # 纯指标窗口分析和语义特征生成
  marketbus/                 # market-data 到 strategy-engine 的 NATS market snapshot 协议
  marketmodel/               # 可被 market-data 和回测服务复用的数据模型
```

`market-data/internal` 仍是实时服务边界。当前内部包通过 alias 或薄封装复用公共包：

- `internal/exchange/*/rest_client.go` 代理到 `pkg/exchangeclient/*`，避免 REST K 线解析逻辑只存在于实时服务内部。
- `internal/store/clickhouse_store.go` 代理到 `pkg/clickhousemarket`，保留原有 store 构造入口。
- `internal/model` 复用 `pkg/marketmodel` 的数据结构，并继续提供 Redis key 工具和服务内辅助函数。

## 运行时组装

`internal/app` 会构建：

- 一个 Redis-backed store，并可选启用 ClickHouse 历史写入。
- 每个已启用交易所一个 collector。
- 一个 K 线聚合器。
- 一个指标 runner。
- 一个指标窗口聚合器，随指标 runner 输出窗口特征。
- 一个可选的 market snapshot publisher，用于把已收盘和未收盘指标快照发布到 NATS JetStream。
- 一个行情健康检查 runner。
- 一个 ClickHouse pending 重试 worker。
- 一个可选的 K 线 backfill 进程内 worker。
- 一个可选的指标冷启动任务进程内 worker。

Collector 运行在重启循环中。聚合器和指标 runner 按固定扫描间隔运行。Context cancellation 会停止所有长时间运行的循环。

当 `MarketBus.Enabled=true` 时，`internal/app/runtime.go` 会创建 `marketbus.NATSPublisher`，并把 publisher 和默认 TTL 传入 indicator runner。Indicator runner 在 closed K 线计算完成后发布 `market.snapshot.closed`，在当前未收盘 K 线实时计算完成后发布 `market.snapshot.realtime`。如果 publisher 构造失败，服务启动失败并关闭已创建的 store 资源。

公共包不直接持有运行时 goroutine、Redis 连接或 WebSocket 连接。它们只提供可复用的纯计算、REST 请求解析、ClickHouse 历史访问和模型定义，避免未来回填或回测服务耦合到 `market-data` 的运行时。

## 交易所

当前已存在适配器包：

- `binance`
- `gate`
- `bitget`
- `bybit`

这些交易所的 REST K 线客户端实现已下沉到 `pkg/exchangeclient`。`market-data/internal/exchange` 继续负责实时服务适配，包括 WebSocket 连接、订阅、消息解码和服务级接口拼装。

默认本地配置：

- Binance 启用，交易对 `ETHUSDT`。
- Gate 启用，交易对 `ETH_USDT`。
- Bitget 关闭。
- Bybit 关闭。

支持的周期列表当前是代码级常量：

- Binance：`1m`、`3m`、`5m`、`15m`、`30m`、`1h`、`2h`、`4h`
- Gate：`1m`、`5m`、`15m`、`30m`、`1h`、`4h`
- Bitget：`1m`、`5m`、`15m`、`30m`、`1h`、`4h`
- Bybit：`1m`、`3m`、`5m`、`15m`、`30m`、`1h`、`2h`、`4h`

## 派生 K 线

聚合器会从已有小周期生成缺失的大周期。

当前规则：

- Binance：`5m -> 10m`
- Bybit：`5m -> 10m`
- Gate：`1m -> 3m`、`5m -> 10m`、`1h -> 2h`
- Bitget：`1m -> 3m`、`5m -> 10m`、`1h -> 2h`

只有当所有源 K 线都存在、已闭合且连续时，聚合器才会写入派生 K 线。

## 指标

指标 runner 读取最近已闭合 K 线，并按交易所、市场、交易对和周期写入最新底层指标快照。指标 runner 本身只负责调度和存储边界，实际技术指标计算由 `pkg/indicatorcalc` 提供。

当前指标集合包括：

- 均线类：SMA、EMA、WMA、HMA、VWMA、DEMA、TEMA、KAMA、Alligator。
- 动量和震荡类：RSI、MACD、KDJ、Stochastic、Stoch RSI、SKDJ、CCI、Williams %R、ROC。
- 波动率和趋势类：ATR、NATR、ADX、DI、Bollinger Bands、Donchian、Supertrend、AlphaTrend、PSAR。
- 成交量和资金流类：成交量均线、OBV、VWAP、滚动 VWAP、MFI、CMF、accumulation/distribution、PVT。
- 价格行为和结构类：K 线形态、Heikin Ashi、支撑阻力、Fibonacci、pivot points、Ichimoku、smart money signals。
- 派生 K 线特征：涨跌幅、振幅、实体比例、影线比例、成交量比例。

指标计算当前只使用已闭合 K 线。Runner 会记录每个交易所、市场、交易对和周期最近已计算指标的 open time，并跳过同一个已闭合 K 线的重复计算。

当前 runner 已按 K 线预热、指标计算、窗口分析三层拆分：

- K 线预热层准备连续已闭合 K 线窗口，默认按 `250` 根指标预热 K 线、`50` 个窗口指标点和 `10` 根 buffer 保留，共 `310` 根。
- 指标计算层只根据 K 线窗口生成底层指标 snapshot。冷启动且没有指标缓存时，最多顺序计算最近 `50` 个指标点；缓存对齐后只补缺失的闭合指标，稳态新收盘一根 K 线只计算一根指标。
- 窗口分析层只读取最近底层指标 snapshot 并调用 `pkg/indicatorwindow`，不再从 K 线回放补算历史指标。
- 闭合指标、指标历史和窗口快照写 Redis 时使用一次合并 pipeline，减少 Redis 往返。
- 连续 K 线推进时缓存最近 `60` 个指标 snapshot，用于窗口分析和 buffer；Redis 也保留同样数量的指标历史，用于服务重启后恢复 recent 指标缓存。
- 扫描型冷启动任务进入 NATS JetStream 内部队列，再由 `market-data` 进程内 worker 消费执行，避免一次扫描直接占满主循环。
- worker 数按 CPU 自适应并设置上限，避免固定 8 worker 在多 symbol/multi interval 场景下过早成为瓶颈。
- `CalculationWindow` 在 runner 长期窗口和窗口快照递推路径启用基础指标流式状态，连续 `Append` 时复用 SMA、EMA、RSI、ATR、ADX/DI、MACD、OBV、VWAP、WaveTrend 和部分 MoneyFlow 中间状态。
- 当前未收盘 K 线 realtime 计算使用缓存窗口 clone 后追加临时 K 线，避免每个 open tick 都从 K 线字符串重建完整计算窗口；重复 open time 的替换场景仍保留 rebuild 语义。
- `pkg/indicatorcalc` 中的部分高成本指标已改为复用流式状态或更紧凑的滚动计算：AI Source 内部 EMA、HMA/DEMA/TEMA/EMA 族、ADX/DI、WaveTrend、OBV/PVT/AD line 派生字段和 VFI compact 路径。旧批量函数保留为 fallback 和测试基准。

最近一次本地冷启动指标压测中，`symbols=5`、`tasks=140`、`lookback=310`、模拟 Redis 写延迟 `0ms` 时，单轮约 `40.0s`，吞吐约 `3.5 tasks/s`。该结果包含首次 recent 指标预热成本；缓存对齐后的稳态路径只补新增闭合指标。后续需要用 top500 长时间全链路压测观察指标队列积压、Redis ops 和实时采集延迟。

当前 realtime 临时窗口 benchmark：

```text
go test ./market-data/internal/indicator -run '^$' -bench BenchmarkWindowWithTemporaryKlineRealtime -benchmem
```

早期本地基线约 `84ms/op`、`6.7MB/op`、`4830 allocs/op`。在固定 warmup、窗口/指标拆分和部分指标流式化后，最近几轮本地结果通常落在约 `35-55ms/op`、`6.7MB/op`、`4842 allocs/op` 区间。该结果受本机 CPU 波动影响，只作为优化方向参考；`CalculateWindow` 的完整特征计算仍是 realtime CPU 的主要优化方向。

每个指标快照包含基础数据质量字段：

- `values.sample_count`：参与计算的已闭合 K 线数量。
- `values.required_count`：当前均线周期配置期望的最小样本数。
- `signals.data_quality`：`ok`、`insufficient`、`gap`、`invalid_ohlc` 或 `zero_volume`。
- `signals.data_quality_reason`：当质量状态不是 `ok` 时的可选细节。

最新底层指标快照和最近 `60` 个指标历史缓存存储在 Redis，并通过 NATS market snapshot 发布给 `strategy-engine`。指标历史缓存只用于服务恢复、窗口分析和观测，不作为长期事实数据；指标不再写入 ClickHouse。需要更长历史指标时，从 ClickHouse K 线按需重算。

在底层指标快照之上，Go `pkg/indicatorwindow` 会生成窗口聚合特征。它会把最近一段已闭合指标序列压缩成策略更容易消费的语义字段，例如：

- 数值字段的最新值、前值、变化、斜率、连续上升/下降次数和区间位置。
- 信号字段的最新状态、前值、是否变化、稳定持续根数和距上次变化根数。
- 面向策略的语义特征，例如 `pump_window_signal`、`dump_window_signal`、`trend_valid`、`ma_ribbon_state`、`macd_window_bias`、`volume_window_state`。

在线 Go `strategy-engine` 启动时从 Redis 读取 Go 已聚合的窗口特征恢复初始态，启动后主要消费 NATS market snapshot。旧 Python 策略框架仍可从 Redis 读取这些特征，不再要求实时路径每次从 ClickHouse 拉取几十到几百根 K 线并本地计算指标。

旧兼容路径只应依赖 Redis 最新指标和最近 K 线。需要更长历史时，应读取 ClickHouse K 线并按需计算指标。

## 行情健康检查

健康检查 runner 会定期检查每个已配置交易所、市场、交易对和周期。它只把最新健康快照写入 Redis；健康快照是运行状态，不写入 ClickHouse。

当前检查：

- 没有已闭合 K 线时，K 线状态为 `missing`。
- 最新已闭合 K 线的 open time 早于两个周期长度时，K 线状态为 `stale`。
- 最近已闭合 K 线窗口不完整或不连续时，K 线状态为 `gap`。
- 最新已闭合 K 线新鲜且最近窗口连续时，K 线状态为 `ok`。
- 没有指标 cursor 时，指标状态为 `missing`。
- 最新指标 open time 落后于最新已闭合 K 线 open time 时，指标状态为 `stale`。
- 指标 cursor 已追上最新已闭合 K 线时，指标状态为 `ok`。
- 当市场被标记为不可用时，两类状态都为 `skipped`。

当前代码级默认值：

- 健康检查间隔为 10 秒。
- 最近 K 线缺口检查 lookback 为 5 根已闭合 K 线。

## ClickHouse 表

ClickHouse 用于持久化分析历史，不用于实时状态交接。

当前表：

- `market_klines`：已闭合 K 线历史，以交易所、市场、交易对、周期和 open time 作为逻辑键。

`market_klines` 使用 `ReplacingMergeTree(updated_at_ms)`，让 ClickHouse merge 时可以对同一逻辑行的重复写入去重。价格和成交量字段以字符串存储，以便第一版实现不强制 decimal scale，同时保留交易所原始精度。

ClickHouse 写入失败不会直接破坏 Redis 实时路径。失败记录会写入 NATS JetStream：

```text
stream: ALPHAFLOW_MARKET_PENDING
subject: market.clickhouse.pending.kline
dead_letter_subject: market.clickhouse.pending.kline.dead
```

后台重试 worker 使用 durable consumer 拉取记录，写入 ClickHouse，成功后 Ack。失败消息由 JetStream 按 `pending_ack_wait` 重投递，超过最大投递次数后进入 dead-letter subject。

K 线 backfill 也支持异步任务：

```text
stream: ALPHAFLOW_MARKET_BACKFILL
subject: market.kline.backfill
dead_letter_subject: market.kline.backfill.dead
```

`market-data-admin backfill --async` 会提交任务到 NATS JetStream。默认部署中由 `market-data` 进程内 worker 消费执行，不单独拆独立服务进程。

指标扫描任务也使用 NATS JetStream：

```text
stream: ALPHAFLOW_MARKET_INDICATOR
subject: market.indicator.calculate
dead_letter_subject: market.indicator.calculate.dead
```

这些名称是 `market-data` 内部约定，不作为配置项暴露。`indicator_queue` 只保留 ack wait、最大投递次数、积压上限、worker batch、max wait 和 worker 数量等运行参数。实时 K 线 handler 仍直接计算最新 K 线指标；队列主要承接扫描型冷启动/补齐任务。

策略决策不直接依赖队列是否为空。可决策条件以 Redis health/cursor 为准：`kline_status == ok`、`indicator_status == ok`，并且 `last_indicator_open_time` 已追上对应窗口 open time。`strategy-engine` 读取 Redis snapshot 时会读取 `health` key；如果指标还在异步队列中导致 health 为 `missing` 或 `stale`，reader 会返回错误，策略不会拿到可决策 snapshot。

本地可用 `queue-status` 查看队列积压：

```sh
make queue-status
```

可用 `market-health` 同时查看 Redis health/cursor 和 NATS queue lag：

```sh
make market-health ARGS='--exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m'
```

`market-health` 输出的 `DECISION_READY=true` 只表示指定交易对和周期的 Redis health 已满足策略 reader 的基本门槛：K 线和指标状态均为 `ok`，且传入 `--window-open-time` 时指标 cursor 没有落后该窗口。它不是完整的 ClickHouse 历史完整性验收；历史缺口仍使用 `stats`、`check` 和 `backfill` 处理。

`pkg/clickhousemarket` 同时提供历史读取接口：

- `RangeKlines`：按交易所、市场、交易对、周期和 open time 范围读取 K 线历史。

该接口面向后续历史回填、回放验证和回测服务。当前 Go 在线策略路径启动时可从 Redis 特征 hash 恢复，启动后优先消费 NATS market snapshot，不依赖每次从 ClickHouse 拉取历史。旧 Python 原型仍可直接读取 Redis 特征 hash。

## Redis Keys

Redis key 形态：

```text
{exchange_code}:{market}:{type}:{symbol}:{extra}
```

已知交易所 code：

- `bn` = Binance
- 其他交易所目前使用原始 exchange 名称，除非后续添加特定映射。

常见类型：

- `k` = K 线命名空间
- `lp` = 最新成交价
- `mp` = 标记价格
- `bt` = book ticker
- `oi` = open interest
- `liq` = 爆仓 sorted set
- `ind` = 最新指标快照
- `ind:last` = 最新已计算指标 open time
- `indwin` = 已收盘窗口特征 hash
- `indwin:latest` = 批量刷入路径使用的最新窗口特征 hash
- `indwin:last` = 最新已写入窗口特征 open time
- `indrt` = 当前 K 线实时指标特征 hash
- `health` = 最新 K 线和指标健康快照
- `ws` = 交易所 WebSocket 连接健康状态

示例：

```text
bn:um:k:ETHUSDT:1m
bn:um:lp:ETHUSDT
bn:um:mp:ETHUSDT
bn:um:bt:ETHUSDT
bn:um:oi:ETHUSDT
bn:um:liq:ETHUSDT
bn:um:ind:ETHUSDT:1m
bn:um:ind:last:ETHUSDT:1m
bn:um:indwin:ETHUSDT:3m
bn:um:indwin:latest:ETHUSDT:3m
bn:um:indwin:last:ETHUSDT:3m
bn:um:indrt:ETHUSDT:3m
bn:um:health:ETHUSDT:1m
bn:um:ws
strategy:position:binance:um:ETHUSDT:rule
```

K 线使用 Redis hash 加 sorted-set index：

```text
{base}:data   # hash field = 毫秒级 open time，value = K-line JSON
{base}:idx    # sorted set member/score = 毫秒级 open time
```

爆仓数据使用 Redis sorted set。Score 是毫秒级 event time。最新状态、指标快照、指标计算 cursor 和 WebSocket 健康状态使用 Redis string。

健康快照也使用带最新状态 TTL 的 Redis string JSON。

### 指标特征 Hash

`indwin` 和 `indrt` 是当前策略优先消费的数据层。

`indwin` 表示上一根已收盘 K 线对应的窗口分析结果。它来自最近一段底层指标序列，适合回答“趋势是否延续”“均线是否发散”“MACD 是否跟随”“是否存在拉盘/砸盘特征”等问题。

`indrt` 表示当前未收盘 K 线的实时指标表现。它保存当前 K 线基础信息和实时指标结果，适合策略判断“数据是否还在更新”“当前价格和指标是否还有效”。

两类 hash 使用统一字段前缀：

```text
meta:*     # 元数据、时间版本、TTL/freshness 配置
value:*    # 数值字段
signal:*   # 状态和枚举字段
kline:*    # 当前 K 线字段，仅 indrt 使用
```

重要 `meta:*` 字段：

| 字段 | 含义 |
| --- | --- |
| `meta:snapshot_type` | `window` 或 `realtime`。 |
| `meta:exchange` | 交易所。 |
| `meta:market` | 市场类型。 |
| `meta:symbol` | 交易对。 |
| `meta:interval` | 周期。 |
| `meta:open_time` | 指标快照 open time。 |
| `meta:close_time` | 指标快照 close time。 |
| `meta:bar_open_time` | 用于 freshness 判断的 K 线 open time。 |
| `meta:bar_close_time` | 用于 freshness 判断的 K 线 close time。 |
| `meta:bar_interval_ms` | 周期毫秒数。 |
| `meta:bar_seq` | `bar_open_time / bar_interval_ms` 得到的时间序号。 |
| `meta:age_limit_ms` | 消费者允许的最大数据年龄。 |
| `meta:ttl_seconds` | Redis key TTL 秒数。 |
| `meta:updated_at` | 写入更新时间。 |

Python reader 会按当前时间计算期望的 `bar_seq`：

- `indrt` 必须是当前周期。
- `indwin` 必须是上一个已收盘周期。
- 两者 `bar_interval_ms` 必须一致。
- `updated_at` 不能超过各自 `age_limit_ms`。
- `indrt` 中 `kline:is_closed` 必须是 `false`。

这套校验用于避免策略在行情停止更新、特征没有刷新或周期错位时继续交易。

旧 Python 策略原型会把窗口聚合字段解码为 `IndicatorWindowAnalysis`：

- `value:{key}_win_latest`、`value:{key}_win_previous` 等会合并成一个数值窗口分析。
- `signal:{key}_win_latest`、`signal:{key}_win_stable_count` 等会合并成一个信号窗口分析。
- 直接字段，例如 `signal:pump_window_signal`、`signal:ma_ribbon_state`、`value:pump_window_score`，会作为策略语义特征直接暴露。

旧 Python 策略原型的活跃仓位也使用 Redis string JSON。当前 key 形态：

```text
strategy:position:{exchange}:{market}:{symbol}:{strategy_name}
```

这些 key 由旧 Python 策略原型负责，不属于 Go market-data 服务，也不是当前 Go 主路径的仓位 key。

WebSocket 健康记录包括连接状态、最近启动和停止时间、最近错误、重连次数、连续失败次数。它们用于监控和排障，不是持久化历史。

启用 stream 分片后，每个 shard 会写入自己的健康 key：

```text
bn:um:ws:0
bn:um:ws:1
```

每个 shard 状态包含 `shard`、`stream_count` 和 `connection_count`。

## Redis 写入削减

Redis 路径经过优化，减少大交易对数量下的重复维护和最新状态写入。

- K 线数据以 `HASH + ZSET index` 写入，因此更新同一个 open time 只会替换一个 hash field，而不是重写完整 sorted-set member payload。
- K 线 trim 和 TTL 维护由进程内 `lcache.FreqCall` 控制，因此每个 K 线命名空间只会低频执行 trim/expire，而不是每次写入都执行。
- 爆仓 trim 和 TTL 维护也使用 `lcache.FreqCall`；爆仓事件仍立即追加，但列表维护不会每条事件重复执行。
- WebSocket 状态写入会在短本地 TTL 内跳过相同 JSON payload。这既保持 Redis 状态 key 新鲜，也避免稳定连接期间重复 `SET`。
- 最新成交价、标记价格、book ticker、open interest 和最新指标快照会在短本地 TTL 内跳过相同 JSON payload。这只抑制 Redis 最新状态重复写入，不影响 ClickHouse K 线历史写入。
- 指标 open-time cursor 写入故意不使用 latest-payload 缓存，因为它参与指标幂等控制。
- 指标历史缓存以 `HASH + ZSET index` 写入，保留最近 `60` 个指标 snapshot，并由低频 trim/expire 维护裁剪和 TTL。
- 窗口特征和实时特征使用 Redis hash 写入，一次刷新只覆盖当前交易对、周期和特征字段，不做时序存储。

K 线和指标 recent 缓存同时保留 Redis 侧历史，用于进程重启后恢复最近窗口；latest-payload 去重和维护节流仍是进程内缓存，重启后会重新建立。

## 保留策略

当前代码级默认值：

- 每个 K 线 key 保留 `310` 条，即 `250` 根指标预热 K 线、`50` 个窗口指标点和 `10` 根 buffer。
- K 线 TTL 为 7 天。
- 每个指标历史 key 保留 `60` 条，即 `50` 个窗口指标点和 `10` 个 buffer。
- 每个爆仓 key 保留 200 条。
- 爆仓 TTL 为 24 小时。
- 最新成交价、标记价格、book ticker 和指标 TTL 为 24 小时。
- 指标窗口特征和实时特征 TTL 跟随 latest TTL，当前为 24 小时。
- 行情健康状态 TTL 为 24 小时。
- 轮询状态，例如 open interest，TTL 为 24 小时。
- ClickHouse 重试队列默认最多保留 100000 条 pending 记录，由 NATS JetStream stream limit 控制。

这些值暂未支持 TOML 配置。

## 本地运行手册

启动 Redis：

```sh
make redis-up
```

启动 ClickHouse：

```sh
make clickhouse-up
```

### ClickHouse K 线维护 CLI

`market-data-admin` 是离线数据维护命令，用于查看、校验、回填和删除 ClickHouse 中的已闭合 K 线历史。它每次执行一个任务后退出，不作为服务常驻。

运行入口：

```sh
make go-market-data-admin ARGS='stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000'
```

开发期可以使用 `make go-market-data-admin` 直接 `go run`。需要稳定执行、手动复用或放进定时任务时，先编译本地二进制：

```sh
make go-market-data-build
```

Go 二进制统一输出到 `backend/go-service/bin/`：

```text
backend/go-service/bin/market-data
backend/go-service/bin/market-data-admin
backend/go-service/bin/market-data-symbols
backend/go-service/bin/market-data-loadtest
backend/go-service/bin/market-data-indicator-loadtest
```

编译后可以直接执行：

```sh
backend/go-service/bin/market-data-admin --config backend/go-service/configs/market-data.local.toml stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000
```

清理本地编译产物：

```sh
make go-market-data-clean
```

所有时间参数使用分钟精度：

```text
YYYYMMDDHHmm
```

时间范围统一按 K 线 open time 解释，并采用左闭右开语义：

```text
start <= open_time < end
```

例如 `--start 202605010000 --end 202607010000` 表示北京时间 2026-05-01 00:00 到 2026-07-01 00:00 之前的所有 K 线，不包括 2026-07-01 这一天开盘的 K 线。

#### stats

`stats` 是推荐的总览命令。它把完整性和物理重复情况汇总到一张表：

```sh
make go-market-data-admin ARGS='stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000'
```

输出字段：

- `EXPECTED`：该时间段理论上应该有多少根 K 线。
- `LOGICAL_ROWS`：按 `open_time_ms` 去重后的逻辑 K 线数。
- `PHYSICAL_ROWS`：ClickHouse 原始物理行数。
- `DUPLICATE_ROWS`：`PHYSICAL_ROWS - LOGICAL_ROWS`。
- `MISSING`：缺失 K 线数量。
- `COMPLETE`：是否完整。
- `DUP_RATIO`：物理重复行占比。
- `FIRST_OPEN` / `LAST_OPEN`：该段数据实际首尾 open time。

默认只输出汇总。需要缺失明细时加：

```sh
make go-market-data-admin ARGS='stats --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000 --show-missing --max-missing-report 50'
```

#### inventory

`inventory` 用于查看库里存了什么数据。无时间范围时按已有数据分组展示：

```sh
make go-market-data-admin ARGS='inventory --exchange binance --market um --symbol ETHUSDT'
```

带 `--start` 和 `--end` 时会切到范围视图，展示每个周期的 `EXPECTED`、`LOGICAL_ROWS`、`PHYSICAL_ROWS`、`DUPLICATE_ROWS`、`MISSING` 和 `COMPLETE`：

```sh
make go-market-data-admin ARGS='inventory --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000'
```

#### check

`check` 用于严格校验 K 线完整性。它支持单周期或多周期，缺一根也会报告 open time：

```sh
make go-market-data-admin ARGS='check --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000 --max-missing-report 20'
```

该命令会继续检查后续周期，不会因为某个周期缺失就提前退出。回填命令内部使用同一套完整性检查，但回填结束后如果仍缺 K 线会返回失败。

#### duplicates

ClickHouse `market_klines` 使用 `ReplacingMergeTree(updated_at_ms)`，它允许同一逻辑 K 线存在多个物理版本，查询加 `FINAL` 或按 `open_time_ms` 去重后才是逻辑视图。`duplicates` 读取原始表，查看物理重复：

```sh
make go-market-data-admin ARGS='duplicates --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000'
```

输出包含：

- `SUMMARY`：每个周期的逻辑行、物理行、重复行、重复 open time 数和最大版本数。
- `DETAIL`：具体哪些 open time 有多个版本。

#### backfill

`backfill` 用于补齐历史 K 线：

```sh
make go-market-data-admin ARGS='backfill --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000 --warmup-bars 300 --concurrency 2'
```

也可以只提交异步任务到 NATS JetStream：

```sh
make go-market-data-admin ARGS='backfill --async --exchange binance --symbol ETHUSDT --intervals 1m,3m,5m --start 202605010000 --end 202607010000'
```

异步任务默认由 `market-data` 进程内的 backfill worker 消费执行，开启项在 `backfill_queue.worker_enabled`：

```sh
make go-market-data-run
```

也可以用 `market-data-admin backfill-worker` 手工消费队列做排查或补偿，不作为默认部署形态。stream、subject、durable 和 dead-letter subject 由 `market-data` 代码内部约定；`backfill_queue` 只保留 ack wait、最大投递次数、积压上限和进程内 worker 开关等运行参数。执行成功后 Ack；失败时等待 JetStream 重投递；达到最大投递次数后写入 dead-letter subject 并 Ack 原任务。当前第一版不提供任务状态查询 API。

#### queue-status

`queue-status` 是只读观测命令，用于查看当前 NATS JetStream stream 和 durable consumer 的积压：

```sh
make queue-status
```

当前覆盖：

- `ALPHAFLOW_MARKET_INDICATOR` / `market-data-indicator-worker`
- `ALPHAFLOW_MARKET_PENDING` / `market-data-clickhouse-pending`
- `ALPHAFLOW_MARKET_BACKFILL` / `market-data-backfill-worker`
- `ALPHAFLOW_STRATEGY` / `position-engine`

输出中的 `missing_stream` 或 `missing_consumer` 通常表示对应服务尚未启动或尚未创建该队列；这本身不是数据丢失结论。

#### market-health

`market-health` 是策略决策前的只读观测命令，用于把 Redis health/cursor 和 NATS queue lag 放在同一次输出里：

```sh
make market-health ARGS='--exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m'
```

核心字段：

- `KLINE`：Redis health 中的 `kline_status`。
- `INDICATOR`：Redis health 中的 `indicator_status`。
- `LAST_KLINE`：最新已闭合 K 线 open time。
- `LAST_INDICATOR`：最新已计算指标 open time。
- `READY`：该周期是否满足可读 snapshot 的基本 health 条件。
- `DECISION_READY`：所有请求周期都 ready 时为 `true`。

可选传入 `--window-open-time`，用于检查 `last_indicator_open_time` 是否追上某个策略窗口 open time。

Docker 下可以用短命令跑同一类 K 线维护任务；它们使用 `jobs` profile，只会按需启动 ClickHouse：

```sh
make kline-check
make kline-backfill
make kline-delete-dryrun
```

短命令默认读取任务配置：

```text
backend/go-service/configs/tasks/kline-default.toml
backend/go-service/configs/tasks/kline-delete-default.toml
```

配置文件用于保存交易所、市场、交易对、周期、时间范围和 `warmup_bars`。需要临时覆盖某个字段时继续使用 CLI 参数，例如：

```sh
make kline-backfill ARGS='--start 202606010000 --end 202607010000'
```

默认模式是 `skip-existing`：

- 先查询 ClickHouse 已有 open time。
- 只为缺失区间生成远端请求。
- 请求结果写入前按 `exchange/market/symbol/interval/open_time` 去重。
- 写入后重新校验完整性。

`--warmup-bars` 用于回测指标预热。它会按每个周期自己的长度把实际补数起点前移，例如 `--warmup-bars 300` 对 `1m` 前移 300 分钟，对 `4h` 前移 1200 小时。日志会同时输出 `requested_start`、`effective_start` 和 `warmup_bars`，回测正式统计仍应从原始 `start` 开始。

`backtest-engine` 的 `[data].warmup_bars` 应与历史补数脚本的 `--warmup-bars` 保持一致。回测读取历史 K 线时会从 `effective_start = start_time - warmup_bars * interval` 开始读取，并分别校验 warm-up 区间和正式交易区间；任一段不完整都会拒绝启动回测。

`check` 和 `delete` 也支持同样的 `--warmup-bars` 语义：

```sh
make go-market-data-admin ARGS='check --exchange binance --market um --symbol ETHUSDT --intervals 1m,5m,4h --start 202605010000 --end 202607010000 --warmup-bars 300'
```

校验时会把范围拆成两段分别检查：

- `warm-up`：`effective_start <= open_time < start`
- `trading`：`start <= open_time < end`

两段都完整才算整体完整。缺失明细会带上 `phase=warmup` 或 `phase=trading`，方便区分是预热数据不足，还是正式交易区间 K 线缺失。

如果数据已经完整，原生周期会显示 `fetch_jobs=0 fetched=0 written=0`，不会再扫远端。需要重写已有逻辑行时可以使用：

```sh
make go-market-data-admin ARGS='backfill --exchange binance --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000 --mode overwrite'
```

`overwrite` 本质仍是插入新版本，ClickHouse 通过 `ReplacingMergeTree(updated_at_ms)` 在逻辑查询和后续 merge 中保留较新的版本。

部分交易所不提供的周期会按已有规则从源周期聚合，例如 Binance `10m` 来自完整的 `5m` 数据。该行为只针对显式传入的周期，不会自动把所有周期都改成基础周期拉取。

#### delete

`delete` 用于删除某段历史 K 线。默认只预览将删除的行数：

```sh
make go-market-data-admin ARGS='delete --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000'
```

真实删除必须显式加 `--confirm`：

```sh
make go-market-data-admin ARGS='delete --exchange binance --market um --symbol ETHUSDT --interval 1m --start 202605010000 --end 202607010000 --confirm'
```

删除只针对 `market_klines`。指标是 Redis 缓存和运行时状态，不再写入 ClickHouse，也不由数据维护 CLI 管理。

如果这些 K 线用于回测，删除前需要确认是否包含 warm-up 区间。回测补数通常使用 `effective_start = start - warmup_bars * interval`，删除时应明确区分正式回测 `start` 和实际补数 `effective_start`，避免删掉后续回测第一根 K 线所需的指标预热数据。

```sh
make go-market-data-admin ARGS='delete --exchange binance --market um --symbol ETHUSDT --interval 4h --start 202605010000 --end 202607010000 --warmup-bars 300'
```

不带 `--confirm` 时仍是 dry-run。日志会输出 `requested_start`、`effective_start`、`warmup_bars` 和预计删除行数。

启动用于策略仓位历史的 PostgreSQL：

```sh
make postgres-up
```

本地运行 market-data：

```sh
make go-market-data-run
```

使用 Docker Compose 启动 Redis、NATS JetStream、ClickHouse、PostgreSQL 和 market-data：

```sh
make stack-up
```

运行旧 Python 策略原型：

```sh
make py-run
```

常用 Python 策略环境变量：

```text
ALPHAFLOW_REDIS_URL=redis://localhost:6380/0
ALPHAFLOW_POSTGRES_DSN=postgresql://alphaflow:alphaflow@localhost:5432/alphaflow
ALPHAFLOW_STRATEGY_EXCHANGE=binance
ALPHAFLOW_STRATEGY_MARKET=um
ALPHAFLOW_STRATEGY_SYMBOL=ETHUSDT
ALPHAFLOW_STRATEGY_KLINE_INTERVAL=1m
ALPHAFLOW_STRATEGY_INTERVAL_SECONDS=10
```

查看 market-data Docker 日志：

```sh
make market-data-logs
```

打开 Redis CLI：

```sh
make redis-cli
```

打开 ClickHouse client：

```sh
make clickhouse-client
```

运行 Go 测试：

```sh
make go-market-data-test
```

运行 collector 事件队列负载测试：

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-loadtest -symbols=50 -duration=30s -rate=5000 -store-latency=1ms
```

该负载测试不会连接真实交易所、Redis 或 ClickHouse。它会用模拟行情事件和假的 store 延迟驱动 collector handler，以便在增加更多真实交易对前检查队列长度、latest-event 丢弃和 worker 吞吐。

运行指标 runner 负载测试：

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-indicator-loadtest -symbols=500 -lookback=310 -runs=2
```

指标负载测试会使用服务当前周期集合和假的 K 线/store 数据模拟四个交易所。它会衡量指标 runner 吞吐和模拟的 Redis 指标写入次数，不会写入真实存储。

运行实时全链路压力测试：

```sh
docker compose exec redis redis-cli -p 6380 FLUSHDB
docker compose exec redis redis-cli -p 6380 CONFIG RESETSTAT
docker compose exec clickhouse clickhouse-client --query "TRUNCATE TABLE IF EXISTS alphaflow.market_klines"

docker compose up -d --build market-data
```

采集 Redis 压力指标：

```sh
docker compose exec redis redis-cli -p 6380 DBSIZE
docker compose exec redis redis-cli -p 6380 INFO stats
docker compose exec redis redis-cli -p 6380 INFO commandstats
docker compose exec redis redis-cli -p 6380 INFO memory
docker compose exec redis redis-cli -p 6380 LATENCY LATEST
```

采集 ClickHouse K 线写入状态：

```sh
docker compose exec clickhouse clickhouse-client --query "SELECT count() FROM alphaflow.market_klines"
docker compose exec clickhouse clickhouse-client --query "SELECT table, sum(rows) FROM system.parts WHERE database = 'alphaflow' AND active GROUP BY table ORDER BY table"
```

最近一次使用 `live-top500.toml` 的本地实时运行观测，大约五分钟窗口：

- Redis keys：34233。
- ClickHouse `market_klines`：44634。
- NATS ClickHouse pending subject 积压：0。
- `CONFIG RESETSTAT` 后 Redis 总命令数：552168。
- Redis 运行中 ops：约 4102 ops/s。
- Redis rejected connections：0。
- Redis evicted keys：0。
- Redis `LATENCY LATEST`：无事件。
- 运行后 Redis 内存：约 38 MiB。

这些数字只是近期本地观测，不是容量保证。它们取决于启用的交易所、交易对数量、交易所消息速率、本地机器资源和当前 WebSocket 连接设置。

## 配置说明

`backend/go-service/configs/market-data.local.toml` 当前控制：

- 启用的交易所。
- ClickHouse 地址、数据库、凭证、超时和重试设置。
- NATS 地址、market snapshot 发布、ClickHouse pending 队列、backfill 异步任务队列和 indicator 扫描任务队列。
- 交易对。
- 日志服务名、级别、格式、输出和文件轮转。

WebSocket 重连延迟、stream shard 大小、事件队列大小和事件 worker 数量是代码级运行默认值，不是 TOML 配置。

WebSocket 适配器使用共享的 4 MiB 读取限制。Collector stream 列表会按代码级每连接最大 stream 数拆分；每个 shard 运行自己的连接和重连 backoff。连接读取失败和订阅写入失败仍会触发重连；单条消息解码或派发失败会带上交易所和消息大小记录日志，然后跳过。

WebSocket handler 在返回前会把校验后的事件入队或合并。后台 collector worker 会将队列事件写入 Redis，以及可选的 ClickHouse-backed store 路径。K 线和爆仓事件在队列满时会等待队列容量。最新状态事件，例如最新成交价、标记价格、book ticker 和 open interest，会按事件类型和交易对合并，然后定期 flush，因此 Redis 能收到最新状态，但不会被迫写入每一个中间更新。

回填只会对真实 REST K 线请求限流。读取 Redis 最新 open time、跳过没有已闭合窗口的交易对等本地检查不会延迟。限流是代码级、按 collector 生效，因此大交易对列表恢复更慢，但可以避免启动时产生数千个交易所 REST 请求。

仍在代码中定义的运行值包括：

- REST 启动限制。
- 交易所 base URL。
- 交易所周期列表。
- 标记价格轮询间隔。
- Open interest 轮询间隔。
- 保留长度和 TTL。
- 聚合扫描间隔。
- 指标 lookback 周期。

当前 `[market_bus]` 配置控制 `market-data -> strategy-engine` 的特征发布：

```toml
[market_bus]
enabled = true
stream = "ALPHAFLOW_MARKET"
closed_subject = "market.snapshot.closed"
realtime_subject = "market.snapshot.realtime"
default_ttl = "30s"
```

已收盘 K 线指标和窗口特征发布到 `market.snapshot.closed`；当前未收盘 K 线实时指标发布到 `market.snapshot.realtime`。Redis 写入当前仍保留同步路径，用于恢复、观测和兼容；“已收盘异步刷 Redis、未收盘定期刷 Redis”仍是后续优化方向，不是当前实现。

## 当前限制

- Redis 不是持久化历史数据存储。
- 指标参数和指标组暂未支持运行时配置。
- 指标当前只使用 K 线 OHLCV 数据；open interest、爆仓、标记价格溢价和订单簿不平衡暂未参与指标计算。
- market snapshot bus 的 top500 长时间吞吐、积压和端到端延迟仍需要压测确认。
- 服务不提供 HTTP API。
- 当服务职责移动或新增生产模块时，应同步更新本地 README 和架构文档。

## 后续可改进项

- 将指标扫描间隔、lookback 周期和指标组改为可配置。
- 增加 Redis 指标缓存观测和清理工具。
- 增加数据质量信号，例如缺口检测、数据陈旧、数据不足。
- 基于 open interest、爆仓、标记价格和 book ticker 增加派生指标。
- 确定持久化历史行情数据存储方案。
