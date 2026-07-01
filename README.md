# AlphaFlow

AlphaFlow 是一个面向合约交易研究的智能交易系统项目。当前重点不是生产下单，而是先把行情采集、K 线聚合、指标计算、策略研判和模拟仓位管理这条链路打通。

当前系统由 Go 行情基础设施和 Python 策略框架组成：

- Go `market-data` 负责交易所行情采集、派生 K 线、指标计算、Redis 实时状态和 ClickHouse 历史写入。
- Python `alphaflow-core` 负责读取行情快照、做 K 线和指标窗口分析、执行可插拔策略、管理策略仓位原型。
- Redis 用于实时缓存和活跃仓位。
- ClickHouse 用于已闭合 K 线和指标历史。
- PostgreSQL 用于已平仓策略仓位历史。

这个项目仍处于基础设施和策略原型阶段。真实交易所下单、账户级风控、回测服务、管理 API 和前端还不是生产模块。

## 当前状态

已实现：

- 多交易所行情适配：Binance、Gate、Bitget、Bybit。
- Go `market-data` 行情服务。
- Redis 实时行情缓存和服务交接。
- ClickHouse 已闭合 K 线和指标历史。
- PostgreSQL 已平仓策略仓位历史。
- 派生 K 线聚合，例如 `10m`、`3m`、`2h` 等交易所缺失周期。
- 基于已闭合 K 线的技术指标计算。
- 动态指标快照模型：`values` 存数值，`signals` 存状态，新增指标通常不需要改 schema。
- 指标窗口分析：自动分析历史指标的方向、斜率、变化、连续上升/下降和状态稳定性。
- Python 可插拔策略引擎。
- 独立策略目录，一个策略一个文件。
- Supertrend 策略原型：3 分钟信号入场，多周期 5/10/15/30 分钟辅助决策，结合 EMA、MACD、ADX/DI、成交量等过滤信号。
- 一锤子买卖的仓位原型：每个策略目标只维护一个方向仓位，不做复杂仓位管理。

近期新增的指标能力包括：

- Supertrend 多参数预设。
- AlphaTrend。
- TV 风格 ADX/DI。
- LazyBear Squeeze Momentum。
- LazyBear WaveTrend。
- 快速 MACD 7/19/9。
- Dynamic Swing Anchored VWAP。
- Chandelier Exit。
- Smart Money / 市场结构。
- Livermore 关键点。
- 多种均线、K 线形态、资金流和价量确认指标。

进行中：

- 优化 Supertrend 策略的信号质量筛选。
- 调整多周期决策，降低大周期滞后对反转行情的误伤。
- 用指标窗口判断趋势，而不是只看单点快照。
- 梳理哪些指标进入策略，哪些只保留在指标库。

尚未完成：

- 回测服务。
- 真实交易所下单执行。
- 账户级实时风控。
- 管理 API。
- 前端。
- 参数化策略配置。
- 指标参数运行时配置。

## 项目结构

```text
AlphaFlow/
  backend/
    go-service/
      market-data/                 # Go 行情采集、聚合、指标计算服务
      pkg/                         # Go 共享 logger、Redis client、HTTP client、常量
    python-service/
      alphaflow-core/              # Python 策略框架，使用 uv 管理依赖
  docs/
    architecture.md                # 架构边界和阶段规划
    market-data.md                 # 当前行情服务说明
    indicators.md                  # 指标字段、分类和策略使用建议
  frontend/                        # 预留给未来前端
  data/                            # 本地运行数据，包括 Redis、ClickHouse、PostgreSQL 数据
  logs/                            # 本地服务日志
```

## 优先阅读

- [docs/architecture.md](docs/architecture.md) 说明服务边界、当前架构和计划模块。
- [docs/market-data.md](docs/market-data.md) 说明已实现的 Go 行情服务、Redis key、指标、本地运行命令和已知限制。
- [docs/indicators.md](docs/indicators.md) 说明当前指标字段、分类、用途和策略使用建议。
- [Makefile](Makefile) 是主要的本地命令入口。

## 核心能力

### 行情基础设施

Go `market-data` 服务负责：

- REST 初始化和 WebSocket 实时同步。
- WebSocket 重连和 REST 补偿。
- 最新成交价、标记价格、盘口 ticker、持仓量、爆仓数据和 K 线写入 Redis。
- 已闭合 K 线和指标历史写入 ClickHouse。
- ClickHouse 写入失败时通过 Redis 队列补偿。
- 交易所缺失周期的派生 K 线聚合。
- K 线和指标健康检查。

### 指标系统

指标计算只使用已闭合 K 线。指标快照按动态 map 存储：

- `values`：数值型指标。
- `signals`：枚举型状态。

当前指标覆盖：

- 均线和趋势结构。
- MACD 和快速 MACD。
- RSI、KDJ、Stochastic、CCI、Williams %R、ROC、WaveTrend。
- ATR、ADX/DI、Bollinger、Donchian、Squeeze Momentum。
- Supertrend、AlphaTrend、PSAR、Chandelier。
- VWAP、滚动 VWAP、Dynamic Swing Anchored VWAP。
- MFI、CMF、OBV、PVT、价量确认。
- 支撑阻力、Fibonacci、Pivot、Ichimoku。
- Smart Money、结构事件、K 线形态、Heikin Ashi、Livermore。

### 策略框架

Python `alphaflow-core` 当前负责：

- 从 Redis 读取最新行情、指标、健康状态和最近 K 线。
- 从 ClickHouse 读取最近指标历史。
- 构造 `MarketSnapshot`。
- 执行 K 线窗口分析和指标窗口分析。
- 运行所有已注册策略。
- 维护 Redis 活跃仓位。
- 将已平仓仓位写入 PostgreSQL。

策略引擎只负责编排和执行策略。具体信号、评分、入场、出场和仓位计划由策略自己定义。

## 本地命令

启动 Redis：

```sh
make redis-up
```

启动 PostgreSQL：

```sh
make postgres-up
```

本地运行 Go 行情服务：

```sh
make go-market-data-run
```

使用 Docker Compose 启动 Redis、ClickHouse、PostgreSQL 和 market-data：

```sh
make stack-up
```

本地运行 Python 策略服务：

```sh
make py-run
```

运行 Go 测试：

```sh
make go-market-data-test
```

运行 Go 行情采集负载测试：

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-loadtest -symbols=50 -duration=30s -rate=5000 -store-latency=1ms
```

运行 Python 检查：

```sh
make py-check
```

运行所有可用检查：

```sh
make check
```

## 当前数据流

```text
交易所 REST/WebSocket
  -> Go market-data collector
  -> Redis + ClickHouse
  -> Python 策略框架
  -> Redis 活跃仓位 + PostgreSQL 已平仓仓位
```

在 `market-data` 内部，K 线还会经过派生聚合和指标计算：

```text
原始 K 线
  -> 派生 K 线聚合
  -> 指标运行器
  -> Redis 最新指标快照 + ClickHouse 指标历史
```

在 `alphaflow-core` 内部，策略研判同时使用当前状态和历史窗口：

```text
Redis 最新行情 + 最近 K 线
ClickHouse 最近指标历史
  -> K 线窗口分析 + 指标窗口分析
  -> 策略决策
  -> Redis 活跃仓位
  -> PostgreSQL 已平仓历史
```

## 配置

本地 Go 行情服务配置文件：

```text
backend/go-service/market-data/configs/local.toml
```

当前配置控制启用的交易所、交易对、ClickHouse 连接和重试设置、日志等。部分运行参数仍是代码级常量，例如 WebSocket 运行保护、指标扫描间隔、保留长度和交易所周期列表。

Python 策略服务读取以下环境变量：

```text
ALPHAFLOW_REDIS_URL
ALPHAFLOW_POSTGRES_DSN
ALPHAFLOW_CLICKHOUSE_HTTP_URL
ALPHAFLOW_CLICKHOUSE_USERNAME
ALPHAFLOW_CLICKHOUSE_PASSWORD
ALPHAFLOW_STRATEGY_EXCHANGE
ALPHAFLOW_STRATEGY_MARKET
ALPHAFLOW_STRATEGY_SYMBOL
ALPHAFLOW_STRATEGY_KLINE_INTERVAL
ALPHAFLOW_STRATEGY_INTERVAL_SECONDS
```

如果配置了 `ALPHAFLOW_POSTGRES_DSN`，策略服务启动时会初始化已平仓仓位表。如果配置了 `ALPHAFLOW_CLICKHOUSE_HTTP_URL`，策略服务会从 ClickHouse 读取最近指标历史，用于指标窗口分析。

## 当前策略方向

当前主策略原型是 Supertrend 策略：

1. 3 分钟 Supertrend 首先出信号。
2. EMA 窗口确认方向，并排除均线缠绕横盘。
3. MACD 或 WaveTrend 确认短线动能。
4. ADX/DI、AlphaTrend、成交量、VWAP 作为可靠性过滤。
5. 5/10/15/30 分钟多周期参与评分，不作为绝对硬拦截。
6. 出场参考 Supertrend 反向、EMA/MACD 转弱、WaveTrend 动能衰减、VWAP 失守和 Chandelier/结构位。

策略设计原则：

- 先解决信号质量，再考虑真实执行。
- 一个策略一个文件，策略逻辑可插拔。
- 策略引擎不关心具体指标细节，只负责编排。
- 通过窗口看趋势，不依赖单点快照。
- 不做复杂仓位管理，当前保持单次入场和单策略单仓位。

## 进度规划

### P0：基础链路稳定

- 稳定交易所行情采集、派生 K 线和指标 runner。
- 保证 Redis 最新状态和 ClickHouse 历史写入可靠。
- 完善指标文档和字段命名稳定性。
- 继续补关键指标测试。

### P1：策略原型打磨

- 优化 Supertrend 策略评分。
- 明确各指标职责：触发、确认、过滤、出场。
- 调整多周期权重，避免大周期滞后误伤早期反转。
- 增加策略运行日志和信号解释能力。
- 将策略参数逐步配置化。

### P2：回测和评估

- 基于 ClickHouse 历史 K 线和指标快照建立回测入口。
- 统计胜率、盈亏比、最大回撤、连续亏损和信号质量。
- 对比不同指标组合和参数。
- 建立可重复的策略评估报告。

### P3：执行和风控

- 设计执行服务边界。
- 增加账户级风险控制。
- 支持真实交易所下单前的模拟执行层。
- 引入订单状态同步和异常恢复。

### P4：管理 API 和前端

- 提供策略配置、运行状态、仓位历史和指标查看 API。
- 建立前端管理台。
- 支持策略启停、参数查看和结果分析。

## 重要说明

- Redis 用于实时行情状态、短窗口缓存、服务交接和活跃策略仓位。
- ClickHouse 用于已闭合 K 线和指标历史。
- PostgreSQL 用于已平仓策略仓位和策略表现分析。
- 当前策略框架是信号生成和模拟持仓管理原型。真实交易所下单执行、回测、账户级风控、API 工作流和前端还不是生产模块。
- 文档应始终和实际实现保持一致。未来想法需要明确标记为计划项，不要写成当前行为。
