# 架构

本文档记录 AlphaFlow 当前架构和预期服务边界。这里需要区分“当前已经存在的实现”和“未来计划模块”。

## 当前阶段

AlphaFlow 当前处于行情数据基础设施和策略框架原型阶段。

已实现：

- Go `market-data` 服务，用于交易所行情采集。
- Redis 缓存和服务交接层。
- ClickHouse 历史存储，用于已闭合 K 线和指标快照。
- 派生 K 线聚合，用于补齐交易所未直接提供的周期，例如 `10m`。
- 基于已闭合 K 线的技术指标计算。
- Python `alphaflow-core` 策略框架原型。
- 多策略信号评估和行情研判输出。
- 基于 Redis 的活跃策略仓位存储。
- 基于 PostgreSQL 的已平仓仓位历史。
- 用于策略上下文的 K 线窗口分析和指标窗口分析。

尚未作为生产模块实现：

- 回测服务。
- 管理 API。
- 执行服务。
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
      pkg/                          # 共享 logger、Redis、HTTP、常量包
  docs/                             # 项目架构和服务说明
```

每个服务都应维护自己的配置、测试、运行入口和依赖管理方式。

## 服务边界

Python 适合承担：

- 策略研究和信号生成。
- 策略编排和仓位决策逻辑。
- 回测和报表。
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
- 未来执行服务、实时风控和流式网关服务。

## 当前行情数据流

```text
交易所 REST/WebSocket
  -> backend/go-service/market-data
  -> Redis + ClickHouse
  -> Python 策略/回测/API 工作流
```

Go `market-data` 服务当前包含以下内部职责：

- `collector`：交易所 REST 初始化、WebSocket 同步、重连循环、轮询数据同步。
- `aggregator`：为交易所未直接提供的周期生成派生 K 线。
- `indicator`：基于已闭合 K 线计算技术指标。
- `store`：Redis 实时读写边界、ClickHouse 已闭合 K 线和指标历史写入、基于 Redis 的 ClickHouse 重试队列。
- `exchange`：支持交易所的 REST/WebSocket 适配器。

详细服务行为见 [market-data.md](market-data.md)。

## 当前策略流程

Python `alphaflow-core` 服务从 Redis 读取当前行情状态，并可选地从 ClickHouse 读取历史指标上下文。它会对每个目标交易对执行所有已配置策略，并通过仓位管理器应用仓位决策。

```text
Redis 最新指标、健康状态、最近 K 线、最新成交价、标记价格
ClickHouse 最近指标快照
  -> MarketSnapshot
  -> K 线窗口分析 + 指标窗口分析
  -> StrategyEngine
  -> Redis 活跃仓位状态
  -> PostgreSQL 已平仓仓位历史
```

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
- 服务间交接。
- 最新 K 线和当前状态访问。
- 最近爆仓数据保留。
- 最新指标快照存储。
- 活跃策略仓位存储。

Redis 不是最终的长期历史行情存储。ClickHouse 负责存储已闭合 K 线和指标历史，供分析类消费者使用。

## ClickHouse 职责

ClickHouse 当前用于：

- 已闭合 K 线历史。
- 指标快照历史。
- 未来研究、回测、报表和 API 工作流的分析读取。
- Python 策略框架读取最近指标历史。

ClickHouse 写入失败会通过 Redis pending 和 processing 队列补偿，因此临时 ClickHouse 故障不会直接破坏实时 Redis 路径。

## PostgreSQL 职责

PostgreSQL 当前用于：

- 已平仓策略仓位历史。
- 分批退出历史。
- 策略表现分析输入，例如毛收益、手续费、净收益和累计净收益。

配置 PostgreSQL DSN 后，Python 策略服务会在启动时创建所需的已平仓仓位表。

## 未来 Go 服务

潜在未来 Go 服务：

```text
backend/go-service/
  market-data/          # 已实现：交易所行情采集
  kline-aggregator/     # 如果聚合逻辑超出 market-data 边界，可考虑拆出
  execution/            # 未来订单下发和订单状态同步
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
- `market-data` 在启用 ClickHouse 时写入已闭合 K 线和指标快照。
- 实时策略消费者从 Redis 读取最新状态，并可从 ClickHouse 读取最近指标历史。
- 历史研究、回测、API 消费者应从 ClickHouse 读取历史数据。
- 活跃策略仓位存储在 Redis。
- 已平仓策略仓位存储在 PostgreSQL。
- 派生 `10m` K 线在 Go 内部由更小周期生成。
- 指标计算只使用已闭合 K 线。
- 指标计算会跳过同一个已闭合 K 线的重复工作，并输出基础数据质量字段。
- 最新指标快照以 Redis string JSON 保存，并在 ClickHouse 中保留历史。
- Python 策略决策同时使用最新指标值，以及最近 K 线和指标的窗口分析。

## 开放问题

- 派生 `10m` K 线是否始终使用 `5m`，还是在部分交易所和场景使用 `1m`。
- 何时引入执行服务和实时风控服务。
- 指标参数是否需要运行时配置。
- 随着 Python 侧增长，如何拆分策略编排、回测、管理 API 和报表。
- 如何将仓位保证金、杠杆、手续费率和策略参数改为运行时配置。
