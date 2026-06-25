# 架构规划

本文档用于记录 AlphaFlow 当前阶段的后端规划。随着系统设计逐步清晰，本文档会持续调整。

## 当前目录结构

```text
AlphaFlow/
  frontend/                         # 未来的 React + TypeScript 前端
  backend/
    python-service/                 # Python 服务，每个服务独立维护依赖
      alphaflow-core/               # 当前 Python 服务，使用 uv 管理
    go-service/                     # Go 服务，统一维护一个 Go module
      pkg/                          # Go 服务通用基础包
```

每个服务都应该独立维护自己的依赖、配置、测试和运行入口。

## 服务边界

Python 主要负责业务编排、策略研究、数据分析和模型相关工作流。

Go 主要负责长期运行的实时基础设施、交易所连接、低延迟 IO 和交易执行相关服务。

## Python 服务

Python 适合承担：

- 策略研究和信号生成。
- 回测和报告生成。
- AI 和机器学习相关流程。
- 面向前端的管理 API。
- 任务编排和批处理任务。
- 数据分析、统计和探索。
- 风控规则配置、审计和报表。

未来可能拆分的 Python 服务：

```text
backend/python-service/
  alphaflow-core/       # 核心业务编排和未来 API 层
  research/             # 策略研究和实验
  backtest/             # 回测服务
  model-service/        # AI 和模型信号服务
  reporting/            # 报表和分析
```

## Go 服务

Go 适合承担：

- 行情数据采集。
- WebSocket 连接管理。
- REST 补发和实时同步。
- K 线聚合和派生行情数据。
- 订单执行和交易所 API 适配。
- 实时风控检查。
- 实时推送网关。
- 对取消、超时和重连要求较高的长期运行 worker。

未来可能拆分的 Go 服务：

```text
backend/go-service/
  market-data/          # 交易所行情采集
  kline-aggregator/     # 10m 等派生周期聚合
  execution/            # 下单、撤单和订单状态同步
  realtime-risk/        # 低延迟实时风控
  stream-gateway/       # 面向前端或服务的 WebSocket/SSE 推送
```

## 第一阶段

第一阶段优先建设 Go 行情采集服务：

```text
Binance USD-M Futures
  -> backend/go-service/market-data
  -> Redis
  -> backend/python-service/alphaflow-core
```

`market-data` 服务初期负责：

- Binance USD-M 永续合约 K 线 REST 初始化。
- WebSocket K 线实时同步。
- WebSocket 最新成交价和标记价格同步。
- WebSocket 最优买卖价同步。
- WebSocket 强平单同步。
- REST 当前持仓量定时同步。
- 程序重启或 WebSocket 重连后的 REST 补发。
- 使用稳定的内部 K 线结构写入 Redis。

第一版默认采集 `ETHUSDT`，原生周期为 `1m`、`3m`、`5m`、`15m`、`30m`、`1h`、`2h`、`4h`。`10m` 不是 Binance 原生周期，后续通过派生聚合服务实现。

Go 行情服务使用本地 TOML 配置文件维护币种、周期、Redis 和 Binance 连接配置：

```text
backend/go-service/market-data/configs/local.toml
```

交易所接入通过 `market-data/internal/exchange` 定义统一 REST/WebSocket 适配边界。Binance 和 Gate 适配器实现同一组接口，collector 不直接绑定具体交易所实现。

Python 服务初期从 Redis 消费行情数据，重点放在策略实验、回测入口，以及后续管理 API。

## 数据流

第一阶段推荐数据流：

```text
Binance REST/WebSocket
  -> Go market-data
  -> Redis
  -> Python strategy/backtest/API workflows
```

后续可能的交易流程：

```text
Python strategy
  -> Go execution
  -> Binance order API
  -> Go execution state sync
  -> Redis or durable database
  -> Python API and frontend
```

## 存储规划

Redis 初期用于：

- 实时行情缓存。
- 服务之间的数据交换层。
- 最新 K 线和当前状态的快速访问存储。

Redis 不应作为最终的长期历史行情存储。后续可以评估 ClickHouse、TimescaleDB、PostgreSQL 或对象存储等更适合长期保存和分析的方案。

推荐的 Redis key 结构：

```text
{exchange_code}:{market}:{type}:{symbol}:{extra}
```

示例：

```text
bn:um:k:ETHUSDT:1m
bn:um:lp:ETHUSDT
bn:um:mp:ETHUSDT
bn:um:bt:ETHUSDT
bn:um:oi:ETHUSDT
bn:um:liq:ETHUSDT
```

当前简写约定：

- `bn` = Binance
- `um` = USD-M Futures
- `k` = K 线
- `lp` = 最新成交价
- `mp` = 标记价格
- `bt` = 买一卖一
- `oi` = 当前持仓量
- `liq` = 强平单

建议使用 sorted set：

- `score` 为 K 线 open time，单位毫秒。
- `member` 为序列化后的 K 线数据。

实时价格使用普通 string JSON 保存最新值：

```text
bn:um:lp:{symbol}
bn:um:mp:{symbol}
bn:um:bt:{symbol}
bn:um:oi:{symbol}
```

强平单使用 sorted set 保存最近 N 条：

```text
bn:um:liq:{symbol}
```

缓存治理约定：

- K 线 sorted set 按条数保留最近 `kline_limit` 条。
- 强平单 sorted set 按条数保留最近 `liquidation_limit` 条。
- 最新成交价、标记价格、买一卖一等最新状态 key 使用 `latest_ttl`。
- 当前持仓量等 REST 轮询状态 key 使用 `polling_ttl`。

运行稳定性约定：

- 服务通过 Go 公共 `pkg/logger` 初始化结构化日志，支持 stdout/stderr/file 输出、source 字段和日志滚动切割。
- 服务通过 Go 公共 `pkg/redisclient` 初始化 Redis 连接、连接池、Ping 检查和关闭。
- 收到 SIGINT/SIGTERM 时通过 context 触发优雅退出。
- WebSocket 断线后按 `reconnect_delay` 重连。
- 每次 WebSocket 建连前都会先执行 REST K 线补偿。
- REST 轮询类任务单次失败只记录错误，不直接退出服务。

## 待决策事项

- 派生 `10m` K 线应基于 `5m` 还是 `1m` 数据生成。
- 策略只使用已闭合 K 线，还是也需要使用实时未闭合 K 线。
- 长期历史行情使用哪种持久化存储。
- 何时引入订单执行和实时风控服务。
