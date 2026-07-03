# AlphaFlow

AlphaFlow 是一个面向合约交易研究的智能交易系统项目。当前重点不是生产下单，而是先把行情采集、K 线聚合、指标计算、窗口特征、策略研判和模拟仓位管理这条链路打通。

当前系统由 Go 行情基础设施和 Python 策略框架组成：

- Go `market-data` 负责交易所行情采集、派生 K 线、调用公共指标计算和窗口分析模块、Redis 实时状态和 ClickHouse K 线历史写入。
- Go `pkg/` 提供可复用基础能力，包括交易所 REST K 线客户端、ClickHouse K 线读写、公共市场模型、纯指标计算和指标窗口分析。
- Python `alphaflow-core` 负责读取 Redis 特征快照、执行可插拔策略、管理策略仓位原型。
- Redis 用于实时缓存和活跃仓位。
- ClickHouse 用于已闭合 K 线历史；指标是由 K 线计算出的 Redis 缓存和运行时状态。
- PostgreSQL 用于已平仓策略仓位历史。

这个项目仍处于基础设施和策略原型阶段。真实交易所下单、账户级风控、回测服务、管理 API 和前端还不是生产模块。

## 当前状态

已实现：

- 多交易所行情适配：Binance、Gate、Bitget、Bybit。
- Go `market-data` 行情服务。
- Redis 实时行情缓存和服务交接。
- ClickHouse 已闭合 K 线历史。
- PostgreSQL 已平仓策略仓位历史。
- 可复用 Go 公共包：`exchangeclient`、`clickhousemarket`、`marketmodel`、`indicatorcalc` 和 `indicatorwindow`。
- 派生 K 线聚合，例如 `10m`、`3m`、`2h` 等交易所缺失周期。
- 基于已闭合 K 线的技术指标计算。
- 动态指标快照模型：`values` 存数值，`signals` 存状态，新增指标通常不需要改 schema。
- Go 指标窗口聚合：自动分析历史指标的方向、斜率、变化、连续上升/下降、状态稳定性，并输出可直接给策略消费的语义特征。
- Redis 特征层：按交易所、市场、交易对和周期保存已收盘窗口特征 hash，以及当前未收盘 K 线实时指标 hash。
- Python 可插拔策略引擎。
- 独立策略目录，一个策略一个文件。
- Supertrend 策略原型：3 分钟信号入场，多周期 5/10/15/30 分钟辅助决策，消费 Go 聚合后的 `pump/dump`、趋势、均线发散、MACD、成交量和多周期特征。
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

- 校准 Supertrend 策略的语义特征权重和阈值。
- 用真实行情样本验证 `pump/dump`、均线发散、MACD 和成交量过滤是否能降低假信号。
- 梳理更多策略，并保持一个策略一个文件、一个策略一份说明文档。
- 补齐 Redis 特征层的回放和观测工具。

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
      market-data/                 # Go 行情采集、聚合、指标 runner 服务
      pkg/                         # Go 共享包，包括交易所 REST、ClickHouse 历史存储、纯指标计算、窗口分析、logger 等
    python-service/
      alphaflow-core/              # Python 策略框架，使用 uv 管理依赖
  docs/
    architecture.md                # 架构边界和阶段规划
    market-data.md                 # 当前行情服务说明
    indicators.md                  # 指标字段、分类和策略使用建议
    strategies/                    # 每个策略的入场、出场和风控说明
  frontend/                        # 预留给未来前端
  data/                            # 本地运行数据，包括 Redis、ClickHouse、PostgreSQL 数据
  logs/                            # 本地服务日志
```

## 优先阅读

- [docs/architecture.md](docs/architecture.md) 说明服务边界、当前架构和计划模块。
- [docs/market-data.md](docs/market-data.md) 说明已实现的 Go 行情服务、Redis key、指标、本地运行命令和已知限制。
- [docs/indicators.md](docs/indicators.md) 说明当前指标字段、分类、用途和策略使用建议。
- [docs/strategies/](docs/strategies/) 说明每个策略的入场、出场、过滤条件和待优化项。
- [Makefile](Makefile) 是主要的本地命令入口。

## 核心能力

### 行情基础设施

Go `market-data` 服务负责：

- REST 初始化和 WebSocket 实时同步。
- WebSocket 重连和 REST 补偿。
- 调用 `pkg/exchangeclient` 提供的交易所 REST K 线客户端。
- 最新成交价、标记价格、盘口 ticker、持仓量、爆仓数据和 K 线写入 Redis。
- 已闭合 K 线写入 ClickHouse。
- 通过 `pkg/clickhousemarket` 写入 ClickHouse，并复用其历史读取能力作为后续回填和回测基础。
- ClickHouse 写入失败时通过 Redis 队列补偿。
- 交易所缺失周期的派生 K 线聚合。
- K 线和指标运行健康检查。

### 指标系统

指标计算只使用已闭合 K 线。纯计算实现位于 `pkg/indicatorcalc`，`market-data/internal/indicator` 只负责 runner 调度、存储读取、结果写入和窗口状态缓存。底层指标快照按动态 map 存储：

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

在底层指标之上，Go `pkg/indicatorwindow` 会生成窗口特征。窗口特征不是新的长期历史源，而是基于底层指标序列聚合出来的二级数据。它可以随口径变化重新计算，策略优先消费这层语义化结果。

`pkg/marketmodel` 提供 K 线、指标快照、实时指标快照、窗口指标快照和持仓量等公共模型。`market-data/internal/model` 通过 type alias 复用这些模型，同时保留 Redis key 生成等服务内工具函数，避免回测和未来历史回填服务直接依赖 `market-data/internal`。

### 策略框架

Python `alphaflow-core` 当前负责：

- 从 Redis 读取窗口特征 hash 和当前未收盘 K 线实时特征 hash。
- 在 Redis 特征 hash 不存在时，兼容旧路径读取最新指标、健康状态和最近 K 线；更长历史应从 ClickHouse K 线按需计算。
- 构造 `MarketSnapshot`。
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

查看和维护 ClickHouse K 线历史：

```sh
make go-market-data-admin ARGS='stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000'
```

`market-data-admin` 是一次性 CLI，不作为服务常驻。它只维护 ClickHouse 里的已闭合 K 线历史，指标不再写入 ClickHouse，也不由该工具维护。时间参数使用 `YYYYMMDDHHmm`，范围语义统一为左闭右开：`start <= open_time < end`。

常用命令：

- `inventory`：查看库里有什么数据，以及逻辑行、物理行、重复行和首尾 open time。
- `stats`：按交易所、交易对、周期和时间段输出完整性总览。
- `check`：严格校验某段时间是否缺 K 线，可输出缺失 open time。
- `duplicates`：查看 ClickHouse 物理重复版本。
- `backfill`：只回填缺失 K 线；默认 `skip-existing`，可安全重复执行。
- `delete`：删除某段 K 线历史；默认 dry-run，必须传 `--confirm` 才会真实删除。

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
  -> Redis 实时状态 + ClickHouse 底层历史
  -> Go 指标计算
  -> Go 指标窗口聚合
  -> Redis 特征 hash
  -> Python 策略框架
  -> Redis 活跃仓位 + PostgreSQL 已平仓仓位
```

在 `market-data` 内部，K 线还会经过派生聚合和指标计算：

```text
原始 K 线
  -> 派生 K 线聚合
  -> 指标运行器
  -> Redis 最新指标快照
  -> 指标窗口聚合
  -> Redis 已收盘窗口特征 + 当前 K 线实时特征
```

在 `alphaflow-core` 内部，策略研判优先消费 Redis 特征层：

```text
Redis 已收盘窗口特征 indwin
Redis 当前 K 线实时特征 indrt
  -> MarketSnapshot
  -> 策略决策
  -> Redis 活跃仓位
  -> PostgreSQL 已平仓历史
```

ClickHouse 只保存闭合 K 线历史，作为研究、回测、重新计算指标和问题追溯的事实数据。指标历史不再持久化到 ClickHouse，实时策略路径优先读取 Redis 中的最新指标和窗口特征缓存。

## Redis 存储设计

Redis 当前同时承载实时状态、策略交接和活跃仓位。关键约定如下：

- 底层 K 线：`{exchange_code}:{market}:k:{symbol}:{interval}:data` 使用 hash 保存 K 线 JSON，field 是 open time；`:idx` 使用 sorted set 保存 open time 索引。
- 最新指标快照：`{exchange_code}:{market}:ind:{symbol}:{interval}` 使用 string JSON，保留底层 `values/signals`。
- 已收盘窗口特征：`{exchange_code}:{market}:indwin:{symbol}:{interval}` 使用一个大 hash，保存上一根已收盘 K 线对应的窗口分析结果。
- 当前实时特征：`{exchange_code}:{market}:indrt:{symbol}:{interval}` 使用一个大 hash，保存当前未收盘 K 线的基础信息和实时指标表现。
- 活跃策略仓位：`strategy:position:{exchange}:{market}:{symbol}:{strategy_name}` 使用 string JSON，由 Python 策略服务维护。

`indwin` 和 `indrt` 的 hash 字段按前缀分组：

- `meta:*`：版本和时间信息，例如 `snapshot_type`、`bar_open_time`、`bar_close_time`、`bar_interval_ms`、`bar_seq`、`updated_at`、`age_limit_ms`。
- `value:*`：数值特征，例如窗口分数、斜率、最新值、变化量。
- `signal:*`：枚举特征，例如趋势方向、均线状态、MACD 偏向、假信号风险。
- `kline:*`：仅实时 hash 使用，保存当前 K 线 open/high/low/close/volume 和 `is_closed`。

Python reader 会校验：

- `indwin` 是否对应上一个已收盘周期。
- `indrt` 是否对应当前未收盘周期。
- 两份 hash 的周期长度是否一致。
- `updated_at` 是否超过各自 `age_limit_ms`。
- 当前 K 线是否仍未收盘。

校验失败时，策略不会继续使用过期特征。

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
ALPHAFLOW_STRATEGY_EXCHANGE
ALPHAFLOW_STRATEGY_MARKET
ALPHAFLOW_STRATEGY_SYMBOL
ALPHAFLOW_STRATEGY_KLINE_INTERVAL
ALPHAFLOW_STRATEGY_INTERVAL_SECONDS
```

如果配置了 `ALPHAFLOW_POSTGRES_DSN`，策略服务启动时会初始化已平仓仓位表。当前实时策略路径读取 Redis 特征 hash 和实时缓存；历史研究和回测从 ClickHouse K 线临时计算指标。

## 当前策略方向

当前主策略原型是 Supertrend 策略：

1. 3 分钟 `pump_window_signal` 或 `dump_window_signal` 首先触发。
2. `*_window_fake_risk` 必须不能高风险。
3. 趋势特征确认方向，包括 Supertrend、AlphaTrend、趋势有效性和价格推进状态。
4. 均线带必须有方向，排除缠绕横盘。
5. MACD 窗口偏向和质量必须跟随方向。
6. 成交量和价量确认用于提高可靠性。
7. 5/10/15/30 分钟多周期参与确认；5m 和 10m 同时反向会硬阻断。
8. 出场参考反向 `pump/dump`、趋势/均线/MACD 反向确认、5m/10m 阻断，以及止盈止损规则。

详细规则见 [docs/strategies/supertrend.md](docs/strategies/supertrend.md)。

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

- 基于 ClickHouse 历史 K 线建立回测入口，指标在回测时按需计算。
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
- ClickHouse 用于已闭合 K 线历史。
- PostgreSQL 用于已平仓策略仓位和策略表现分析。
- 当前策略框架是信号生成和模拟持仓管理原型。真实交易所下单执行、回测、账户级风控、API 工作流和前端还不是生产模块。
- 文档应始终和实际实现保持一致。未来想法需要明确标记为计划项，不要写成当前行为。
