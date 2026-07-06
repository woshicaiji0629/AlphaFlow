# 架构

本文档记录 AlphaFlow 当前架构和预期服务边界。这里需要区分“当前已经存在的实现”和“未来计划模块”。

## 当前阶段

AlphaFlow 当前处于行情数据基础设施、Go 策略引擎、回测和 paper 仓位路由打通阶段。

已实现：

- Go `market-data` 服务，用于交易所行情采集。
- Go 公共包层，用于承载可被实时服务、历史回填和未来回测复用的模型、交易所 REST 客户端、ClickHouse 历史读写、指标计算和窗口分析。
- Redis 缓存和当前状态层。
- NATS JetStream 队列层。
- ClickHouse 历史存储，用于已闭合 K 线。
- 派生 K 线聚合，用于补齐交易所未直接提供的周期，例如 `10m`。
- 基于已闭合 K 线的技术指标计算。
- 基于底层指标序列的 Go 指标窗口聚合。
- Redis 特征 hash，用于策略启动恢复、故障恢复、观测和兼容路径。
- NATS JetStream market snapshot bus，用于 `market-data -> strategy-engine` 的实时特征同步。
- Go `strategy-engine` 在线策略服务。
- Go `backtest-engine` 回测入口、滚动 snapshot、模拟成交和结果持久化。
- Go `position-engine` 仓位/执行路由服务，当前已接入 paper route。
- 策略决策通过 NATS JetStream 从 `strategy-engine` 进入 `position-engine`。
- 基于 Redis 的当前活跃 paper 仓位存储。
- 基于 ClickHouse 的策略事件、回测交易明细和回测摘要。

尚未作为生产模块实现：

- 管理 API。
- 真实交易所下单。
- 账户级实时风控服务。
- 前端。

## 仓库结构

```text
AlphaFlow/
  frontend/                         # 预留给未来 React + TypeScript 前端
  backend/
    python-service/                 # Python 服务，每个服务维护自己的依赖
      alphaflow-core/               # 当前 Python 策略框架，使用 uv 管理
    go-service/                     # Go 服务，位于同一个 Go module 下
      market-data/                  # 当前活跃的行情数据服务
      strategy-engine/              # 在线策略引擎
      backtest-engine/              # 回测引擎
      position-engine/              # 仓位/执行路由服务
      pkg/                          # 共享 logger、Redis、HTTP、常量、市场模型、交易所客户端、ClickHouse 历史和纯计算包
  docs/                             # 项目架构和服务说明
```

每个服务都应维护自己的配置、测试、运行入口和依赖管理方式。

## 服务边界

Python 适合承担：

- 策略研究和信号生成。
- 研究型回测和报表探索。
- AI 和机器学习工作流。
- 未来管理 API。
- 任务编排和批处理任务。
- 数据分析和探索。
- 风控配置、审计和报表工作流。

Go 适合承担：

- 长时间运行的实时基础设施。
- 交易所 REST/WebSocket 连接。
- 低延迟 IO。
- 行情数据采集。
- K 线聚合和派生行情数据。
- 指标计算和指标窗口聚合。纯计算能力位于 `backend/go-service/pkg/indicatorcalc` 和 `backend/go-service/pkg/indicatorwindow`，不依赖 `market-data` 的存储、runner 或 Redis/ClickHouse。
- 可复用的历史数据基础能力位于 `backend/go-service/pkg/exchangeclient` 和 `backend/go-service/pkg/clickhousemarket`，用于后续历史回填和回测服务。
- 面向实时策略的低延迟特征发布。
- 在线策略引擎、回测引擎、仓位路由、未来执行服务、实时风控和流式网关服务。

## 当前行情数据流

```text
交易所 REST/WebSocket
  -> backend/go-service/market-data
  -> Redis 实时状态 + ClickHouse 底层历史
  -> NATS JetStream 内部补偿队列
  -> Redis 指标窗口特征（恢复缓存 / 观测 / 兼容）
  -> NATS JetStream market snapshot
  -> Go strategy-engine / backtest-engine / 未来 API 工作流
```

Go `market-data` 服务当前包含以下内部职责：

- `collector`：交易所 REST 初始化、WebSocket 同步、重连循环、轮询数据同步。
- `aggregator`：为交易所未直接提供的周期生成派生 K 线。
- `indicator`：实时指标 runner，负责从存储读取 K 线、调用公共计算包、写入 Redis 缓存并发布 market snapshot。
- `pkg/indicatorcalc`：基于已闭合 K 线计算技术指标的纯计算包。
- `pkg/indicatorwindow`：基于最近指标序列生成窗口特征和策略语义字段的纯计算包。
- `pkg/marketbus`：`market-data` 向 `strategy-engine` 发布已收盘窗口特征和当前未收盘 K 线实时指标的 NATS JetStream 协议。
- `pkg/exchangeclient`：交易所 REST K 线请求客户端，可被实时采集和历史回填复用。
- `pkg/clickhousemarket`：ClickHouse K 线历史读写。
- `pkg/marketmodel`：跨实时服务、历史回填和回测服务复用的 K 线、指标快照和持仓量模型。
- `store`：Redis 实时读写边界、ClickHouse 已闭合 K 线写入、基于 NATS JetStream 的 ClickHouse 内部重试队列。
- `exchange`：支持交易所的 REST/WebSocket 适配器。

当前 `market-data/internal` 只保留服务级编排和适配边界：

- `internal/exchange/*/rest_client.go` 对 `pkg/exchangeclient/*` 做兼容封装，WebSocket 适配仍留在 `market-data` 内部。
- `internal/store/clickhouse_store.go` 委托 `pkg/clickhousemarket`，保留原有构造函数形态。
- `internal/model` 通过 type alias 复用 `pkg/marketmodel`，并保留 Redis key、交易所缩写和服务内辅助函数。

这样可以避免未来历史回填、回测或分析服务导入 `market-data/internal`，同时保持当前实时服务的目录边界稳定。

详细服务行为见 [market-data.md](market-data.md)。

## 当前策略流程

在线策略主路径已经迁移到 Go。`strategy-engine` 启动时从 Redis 读取 Go 已聚合的特征快照作为恢复初始态；启动后消费 NATS JetStream market snapshot，更新进程内市场态，对每个目标交易对执行已配置策略，并把策略决策发布到 NATS JetStream。`position-engine` 长驻消费决策消息，当前接入 paper route，并把当前仓位写入 Redis、事件写入 ClickHouse。

```text
NATS JetStream market.snapshot.closed / market.snapshot.realtime
  -> strategy-engine 内存市场态
  -> strategy.Snapshot
  -> strategy.Engine
  -> NATS JetStream strategy.decision
  -> position-engine
  -> Redis paper 当前仓位 + ClickHouse strategy_events
```

- Redis `indwin` / `indrt` / health 在策略引擎启动时用于恢复初始态；如果恢复数据缺失或过期，策略引擎会等待 NATS market snapshot 补齐。
- `strategy-engine` 会校验 market snapshot 的 `created_at`、`expires_at`、open time 和 updated at，旧消息不会覆盖更新的内存态。
- 当行情输入缺失或过期时，在线策略进入降级状态：拒绝新开仓，保留平仓、减仓和止损等退出路径。

Python `alphaflow-core` 保留为旧策略原型参考，不作为新策略架构的主路径。

已实现的策略框架行为：

- 一个 `StrategyEngine` 可以注册多个策略。
- 每个策略返回 `StrategyResult`，包含信号、分数、置信度、行情研判、仓位计划和退出规则。
- 每个交易所、市场、交易对、策略组合只能有一个活跃仓位。
- 一个策略仓位只能是空仓、多仓或空仓方向之一；同一策略目标不允许多空双开。
- 开仓必须有策略信号和理由。
- 平仓必须有理由和退出原因类型。
- 当前退出原因包括策略平仓、止盈、止损、移动止损和分批退出。
- 分批退出会更新 Redis 中的活跃仓位，直到仓位完全平掉。
- 完全平仓后，仓位会写入 PostgreSQL，用于后续统计。
- 已平仓仓位包含毛收益、手续费、净收益，以及跨分批退出的累计结果。

当前硬编码的仓位核算默认值：

- 保证金：`100` USDT。
- 杠杆：`100x`。
- 手续费率：每侧名义价值 `0.0006`。

这些默认值目前对所有策略共用。生产使用前应改成配置项。

## 交易所

当前适配器集合包括：

- Binance USD-M futures。
- Gate USDT futures。
- Bitget USDT futures。
- Bybit linear futures。

本地配置默认启用 Binance 和 Gate，默认关闭 Bitget 和 Bybit。

## Redis 职责

Redis 当前用于：

- 实时行情缓存。
- 最新 K 线和当前状态访问。
- 最近爆仓数据保留。
- 最新指标快照存储。
- 已收盘指标窗口特征存储。
- 当前 K 线实时指标特征存储。
- `strategy-engine` 启动恢复和故障恢复缓存。
- 当前活跃 paper 仓位存储。

Redis 不是队列，也不是最终的长期历史行情存储。ClickHouse 负责存储已闭合 K 线，供分析类消费者、回测和指标重算使用。指标和窗口特征都属于可重算的二级数据。在线策略启动后主要消费 NATS market snapshot 并维护内存态，Redis 特征 hash 主要用于恢复、观测和兼容。

## NATS JetStream 职责

NATS JetStream 当前用于：

- 服务间策略决策通信：`strategy-engine -> position-engine`。
- 服务间行情快照通信：`market-data -> strategy-engine`，默认 stream 为 `ALPHAFLOW_MARKET`，subject 为 `market.snapshot.closed` / `market.snapshot.realtime`。
- `market-data` 内部 ClickHouse pending 重试队列。
- `market-data` 内部 K 线 backfill 异步任务队列。

跨服务通信需要明确协议和配置；服务内自产自销队列由对应服务代码内部约定 stream、subject、durable 和 dead-letter 名称，配置只保留 URL、ack wait、最大投递次数、batch 等运行参数。本地 Docker Compose 使用 `data/nats` 作为 JetStream 文件存储目录。

## ClickHouse 职责

ClickHouse 当前用于：

- 已闭合 K 线历史。
- 未来研究、回测、报表和 API 工作流的分析读取。
- 指标和窗口聚合口径调整后的重新计算和问题追溯。

ClickHouse 写入失败会通过 NATS JetStream pending 队列补偿，因此临时 ClickHouse 故障不会直接破坏实时 Redis 路径。

## PostgreSQL 职责

PostgreSQL 当前只作为本地基础设施保留，主要兼容旧 Python 原型路径。Go 主路径的 K 线、策略事件、回测交易明细和回测摘要当前落在 ClickHouse。

## 未来 Go 服务

潜在未来 Go 服务：

```text
backend/go-service/
  market-data/          # 已实现：交易所行情采集
  kline-aggregator/     # 如果聚合逻辑超出 market-data 边界，可考虑拆出
  strategy-engine/      # 已实现：在线策略引擎
  backtest-engine/      # 已实现：回测引擎
  position-engine/      # 已实现：paper 仓位/执行路由；未来扩展 testnet/live/notify
  order-executor/       # 未来真实订单下发和订单状态同步
  realtime-risk/        # 未来低延迟实时风控
  stream-gateway/       # 未来 WebSocket/SSE 推送网关
```

当前聚合和指标逻辑仍在 `market-data` 内部。只有当出现明确的运维或职责边界原因时，才考虑拆分。

## 未来 Python 服务

潜在未来 Python 服务：

```text
backend/python-service/
  alphaflow-core/       # 当前策略框架；未来可能承担编排/API 入口
  research/             # 策略研究和实验
  backtest/             # 回测服务
  model-service/        # AI/模型信号服务
  reporting/            # 报表和分析
```

`alphaflow-core` 已不再只是脚手架。它当前包含第一版策略框架实现。未来服务拆分仍是开放问题，应在策略、回测和 API 职责增长后再重新评估。

## 运行可靠性约定

- 服务通过 `backend/go-service/pkg/logger` 使用结构化日志。
- Redis 访问集中在 `backend/go-service/pkg/redisclient` 和服务级 store 实现中。
- 长时间运行的 Go 服务应接受 context cancellation，并在 SIGINT/SIGTERM 时优雅退出。
- WebSocket collector 会在 `reconnect_delay` 后重连。
- Collector 启动和 WebSocket 重连前应执行 REST 补偿，再进入实时同步。
- WebSocket 读取和订阅失败会触发重连；单条消息派发失败只记录日志并跳过。
- 轮询任务失败应记录日志，不应停止整个服务，除非该失败使服务不可用。

## 当前决策

- `market-data` 将实时行情模型写入 Redis。
- `market-data` 在启用 ClickHouse 时只写入已闭合 K 线。
- `market-data` 在 Go 内部基于底层指标序列生成窗口特征。
- Go 在线策略消费者启动时从 Redis `indwin` 和 `indrt` 恢复，启动后优先消费 NATS market snapshot 并维护内存态。
- Python 侧不再要求每次策略评估都读取几十到几百根历史指标。
- 历史研究、回测、API 消费者应从 ClickHouse 读取 K 线历史，并按需计算指标。
- 当前 paper 策略仓位存储在 Redis。
- 策略事件、回测交易明细和回测摘要存储在 ClickHouse。
- 派生 `10m` K 线在 Go 内部由更小周期生成。
- 指标计算只使用已闭合 K 线。
- 指标计算会跳过同一个已闭合 K 线的重复工作，并输出基础数据质量字段；闭合指标 Redis 写入合并为单次 pipeline，连续 K 线的指标窗口 snapshot 会增量缓存。
- 最新底层指标快照以 Redis string JSON 保存；指标历史不再写入 ClickHouse。
- 指标窗口特征和当前实时指标特征以 Redis hash 保存，并携带时间版本信息。
- Python 策略决策使用特征 freshness 判断数据是否还可用。

## 开放问题

- 派生 `10m` K 线是否始终使用 `5m`，还是在部分交易所和场景使用 `1m`。
- 何时引入执行服务和实时风控服务。
- 指标参数是否需要运行时配置。
- 随着 Python 侧增长，如何拆分策略编排、回测、管理 API 和报表。
- 如何将仓位保证金、杠杆、手续费率和策略参数改为运行时配置。
- Redis 特征层是否需要增加回放、审计或快照导出工具。
- 特征字段命名是否需要形成稳定版本化协议。
