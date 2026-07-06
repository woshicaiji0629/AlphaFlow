# AlphaFlow

AlphaFlow 是一个面向合约交易研究的智能交易系统项目。当前重点不是生产实盘下单，而是先把行情采集、K 线聚合、指标计算、窗口特征、策略执行、模拟仓位、回测和分析事件这条链路打通。

当前系统以 Go 为主：

- Go `market-data` 负责交易所行情采集、派生 K 线、公共指标计算、窗口分析、Redis 实时状态和 ClickHouse K 线历史写入。
- Go `pkg/` 提供可复用基础能力，包括交易所 REST K 线客户端、ClickHouse K 线读写、公共市场模型、纯指标计算、指标窗口分析、策略模型、仓位管理和执行协议。
- Go `strategy-engine` 当前已实现在线策略服务，用于读取 Redis 特征快照、按配置加载策略、运行策略集合，并将策略决策发布到 NATS JetStream。
- Go `backtest-engine` 当前已实现独立回测初版链路，包括历史 K 线读取、滚动快照、策略执行、模拟成交、交易明细和摘要持久化。
- Go `position-engine` 当前已实现独立仓位/执行路由服务，可长驻消费 NATS JetStream 策略决策、处理 paper 路由，并滚动扫描 paper 持仓的退出规则。
- Python `alphaflow-core` 是旧策略原型框架，保留为参考，不作为新策略架构的主路径。
- Redis 用于实时行情、指标特征和当前活跃仓位缓存，不再承担队列职责。
- NATS JetStream 用于队列和服务间通信，本地运行时使用 `data/nats` 文件存储持久化。
- ClickHouse 用于已闭合 K 线历史、策略事件历史、回测交易明细和回测摘要。

这个项目仍处于基础设施和策略引擎建设阶段。真实交易所下单、账户级风控、完整回测报告、管理 API 和前端还不是生产模块。

## 当前状态

已实现：

- 多交易所行情适配：Binance、Gate、Bitget、Bybit。
- Go `market-data` 行情服务。
- Redis 实时行情缓存和当前状态。
- NATS JetStream 队列层：服务间策略决策队列，以及 `market-data` 内部 ClickHouse pending 和异步 K 线 backfill 队列。
- ClickHouse 已闭合 K 线历史。
- 可复用 Go 公共包：`exchangeclient`、`clickhousemarket`、`marketmodel`、`indicatorcalc`、`indicatorwindow`、`strategy`、`position` 和 `execution`。
- 派生 K 线聚合，例如 `10m`、`3m`、`2h` 等交易所缺失周期。
- 基于已闭合 K 线的技术指标计算。
- 动态指标快照模型：`values` 存数值，`signals` 存状态，新增指标通常不需要改 schema。
- Go 指标窗口聚合：自动分析历史指标的方向、斜率、变化、连续上升/下降、状态稳定性，并输出可直接给策略消费的语义特征。
- Redis 特征层：按交易所、市场、交易对和周期保存已收盘窗口特征 hash，以及当前未收盘 K 线实时指标 hash。
- Go 策略公共模型和 `strategy.Engine`。
- Go 独立仓位管理：止盈、止损、移动止损、分批退出。
- Go 仓位当前态 store：内存和 Redis。
- Go 策略事件 store：ClickHouse `strategy_events`、`backtest_trades` 和 `backtest_run_summary`。
- Go 执行协议：`OrderIntent`、`ExecutionReport` 和 `PaperBroker`。
- Go `strategy-engine` 在线服务：配置加载、Redis snapshot reader、策略 registry、runner 编排和策略决策发布。
- Go `backtest-engine` 回测初版链路：独立入口、独立配置、多交易对/多周期历史读取、滚动 snapshot、策略执行、模拟成交、事件/交易/摘要落库。
- Go `position-engine` 仓位路由服务：独立入口、独立配置、NATS JetStream 长驻消费、策略信号 sink 路由边界、paper route 处理和 paper 持仓 scanner。
- Go 策略决策总线：`pkg/strategybus`，定义 `strategy.Decision` 的 NATS JetStream 输入协议、`trace_id`、`signal_id` 和 result-level signal id。
- Go 幂等存储：`pkg/idempotency`，当前用于 position-engine 的消息/result 级重复处理控制。
- Go paper 仓位处理器：`pkg/positionhandler/paper`，承接 paper 仓位计划、订单意图、paper 成交和事件写入。
- Go `market-data` 内部队列约定：ClickHouse pending 和 K 线 backfill 任务使用 NATS JetStream，但 stream/subject/durable 命名由代码约定，不暴露为配置；配置只保留 NATS URL、ack wait、投递上限、batch 等运行参数。
- 指标 runner 已做性能优化：闭合指标 Redis 写入合并为单次 pipeline，连续 K 线的指标窗口 snapshot 增量缓存，扫描型冷启动任务进入 NATS JetStream 内部队列并由进程内 worker 消费。
- 模拟仓和回测 sizing 规则：`margin_quote * leverage`，默认可配置为 `100U * 100x`。
- 模拟手续费和返佣估算：`fee_rate` + `rebate_pct`。
- Supertrend 策略原型文档：3 分钟信号入场，多周期 5/10/15/30 分钟辅助决策，消费 Go 聚合后的 `pump/dump`、趋势、均线发散、MACD、成交量和多周期特征。

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

尚未完成：

- 图表化回测报告、参数化批量回测和结果查询入口。
- 真实交易所 order executor。
- 交易所账户级实时风控。
- 交易所 symbol 精度、张数和最小下单量换算。
- `testnet` / `live` / `notify` route handler。
- 管理 API。
- 前端。
- 参数化策略配置。
- 指标参数运行时配置。
- top500 长时间全链路压测和指标队列积压观测。
- 订单服务级幂等落库和重复订单意图拦截。
- HTTP 健康检查接口。

## 项目结构

```text
AlphaFlow/
  backend/
    go-service/
      market-data/                 # Go 行情采集、聚合、指标 runner 服务
      strategy-engine/             # Go 在线策略引擎；当前实现服务入口、配置、reader、app、runner 和策略 registry
      backtest-engine/             # Go 回测引擎；当前实现历史读取、滚动 snapshot、模拟成交和结果落库
      position-engine/             # Go 仓位/执行路由服务；当前实现入口、配置、消费循环、route 边界和 paper scanner
      pkg/
        configutil/                # 公共配置解码工具
        clickhousemarket/          # ClickHouse K 线历史读写
        exchangeclient/            # 交易所 REST K 线客户端
        execution/                 # 订单意图、执行报告、PaperBroker
        indicatorcalc/             # 纯指标计算
        indicatorwindow/           # 指标窗口语义聚合
        idempotency/               # Redis 幂等状态存储
        marketmodel/               # 公共行情和指标模型
        position/                  # 仓位管理、Redis 当前态、ClickHouse 事件态
        positionhandler/           # 策略结果到仓位/执行处理器
        strategy/                  # 策略模型、策略接口和策略引擎
        strategybus/               # 策略决策 NATS JetStream 协议
        strategyregistry/          # 策略插件注册和构造入口
        strategyroute/             # 策略结果路由和 dispatcher
    python-service/
      alphaflow-core/              # 旧 Python 策略原型，保留参考
  docs/
    architecture.md                # 架构边界和阶段规划
    market-data.md                 # 当前行情服务说明
    indicators.md                  # 指标字段、分类和策略使用建议
    progress.md                    # 当前进度、已知问题和下一步
    strategies/                    # 策略和 Go 策略引擎设计文档
  frontend/                        # 预留给未来前端
  data/                            # 本地运行数据，包括 Redis、ClickHouse、PostgreSQL 数据
  logs/                            # 本地服务日志
```

## 优先阅读

- [docs/architecture.md](docs/architecture.md) 说明服务边界、当前架构和计划模块。
- [docs/market-data.md](docs/market-data.md) 说明已实现的 Go 行情服务、Redis key、指标、本地运行命令和已知限制。
- [docs/indicators.md](docs/indicators.md) 说明当前指标字段、分类、用途和策略使用建议。
- [docs/progress.md](docs/progress.md) 说明当前推进进度、已知问题和建议下一步。
- [docs/strategies/README.md](docs/strategies/README.md) 说明策略文档入口。
- [docs/strategies/go-strategy-engine.md](docs/strategies/go-strategy-engine.md) 说明 Go 策略引擎、仓位、执行和事件持久化的当前边界。
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
- ClickHouse 写入失败时通过 NATS JetStream 队列补偿。
- 交易所缺失周期的派生 K 线聚合。
- K 线和指标运行健康检查。

### 指标系统

指标计算只使用已闭合 K 线。纯计算实现位于 `pkg/indicatorcalc`，`market-data/internal/indicator` 只负责 runner 调度、存储读取、结果写入和窗口状态缓存。

底层指标快照按动态 map 存储：

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

### Go 策略引擎

Go 策略引擎当前由公共包、策略包、在线策略服务和独立仓位路由服务组成：

- `pkg/strategy`：策略输入输出模型、策略接口、基础引擎。
- `pkg/position`：仓位计划、退出规则、Redis 当前态、ClickHouse 事件态。
- `pkg/execution`：订单意图、执行报告、paper broker。
- `pkg/strategyregistry`：策略注册和构造入口，供在线引擎和回测共用。
- `pkg/strategies/supertrend`：Supertrend 策略 Go 实现。
- `strategy-engine/internal/config`：在线配置加载和校验。
- `strategy-engine/internal/reader`：读取 Redis `indwin` / `indrt` 并构造 `strategy.Snapshot`。
- `strategy-engine/internal/app`：服务启动、依赖装配和周期调度。
- `strategy-engine/internal/runner`：在线策略评估和决策发布层。
- `backtest-engine`：独立回测入口，当前承接历史 K 线读取、滚动 snapshot、策略执行、模拟成交和结果持久化。
- `position-engine`：独立仓位/执行路由入口，当前可长驻消费 NATS JetStream 策略决策，并把 paper route 和 paper 持仓 scanner 交给公共仓位/执行能力处理。
- `pkg/strategybus`：策略决策 NATS JetStream 协议，默认 stream 为 `ALPHAFLOW_STRATEGY`，subject 为 `strategy.decision`，envelope 带 `trace_id` / `signal_id`。
- `pkg/strategyroute`：策略结果路由模型和 dispatcher。
- `pkg/positionhandler/paper`：paper 仓位、订单意图、paper broker、事件写入处理器。

当前 `strategy-engine` 已支持：

- 从 Redis `indwin` / `indrt` 读取入场周期快照，并读取确认周期窗口特征。
- 读取每个策略的当前仓位作为策略上下文。
- 调用公共 `strategy.Engine` 生成 `strategy.Decision`。
- 通过 `pkg/strategyregistry` 按配置加载策略集合；在线可以同时运行多个策略，回测通常一次只跑一个策略。
- 通过 `pkg/strategybus` 将策略决策发布到 NATS JetStream。

当前 `backtest-engine` 已支持：

- 按回测配置读取多交易对和入场/确认周期历史 K 线。
- 为每根入场周期 K 线构造与在线一致的 `strategy.Snapshot`，确认周期只取当时已经闭合的数据。
- 复用公共策略、仓位、paper broker 和 route dispatcher 执行回测。
- 使用独立 `bt` scope 和 run id 隔离回测仓位，不写在线 paper 仓位。
- 将策略事件、回测交易明细和 run 级摘要写入 ClickHouse。

当前 `position-engine` 已支持：

- 长驻消费 NATS JetStream 策略决策。
- result-level 幂等处理。
- paper route 的开仓、平仓、减仓计划。
- paper 当前持仓 scanner，用最新价格滚动检查止盈、止损、移动止损和分批退出。
- 止盈、止损、移动止损、分批退出。
- 移动止损最高价/最低价刷新。
- paper 成交后更新当前仓位。
- 模拟手续费、返佣、notional、PnL 和收益率事件持久化。
- 将策略事件写入 ClickHouse `strategy_events`。

`backtest-engine` 与在线 `strategy-engine` 独立。回测入口不复用在线 Redis reader，也不写在线 paper 仓位状态；它只复用公共策略、仓位和执行模型。

`position-engine` 与在线/回测数据源独立。在线和回测只负责产出策略决策；决策通过 `pkg/strategybus` 定义的 NATS JetStream 协议进入 position-engine。当前 paper route 已接入公共 paper handler，并可从 Redis `lp/mp` 价格 key 补最新价格上下文；服务长驻消费新消息，空批次带 backoff；处理成功后 Ack，过期开仓类信号跳过并 Ack，过期退出类信号会用当前持仓 exit rules 和最新价格做保守重裁决；处理失败的消息由 JetStream 按 ack wait 重投递，超过投递上限后进入 dead-letter subject。position-engine 已接入 `pkg/idempotency`，优先按 result-level signal id 做幂等，兼容旧消息的 message id 幂等。backtest、live 和 notify handler 后续补齐。

更多细节见 [docs/strategies/go-strategy-engine.md](docs/strategies/go-strategy-engine.md)。

### Python 策略原型

Python `alphaflow-core` 是旧策略原型框架，当前保留用于参考和对照：

- 从 Redis 读取窗口特征 hash 和当前未收盘 K 线实时特征 hash。
- 构造 `MarketSnapshot`。
- 运行已注册策略。
- 维护旧的 Redis 活跃仓位原型。

新策略执行和回测方向以 Go 公共策略包为主。

## 本地命令

启动 Redis：

```sh
make redis-up
```

启动本地基础设施：

```sh
make infra-up
```

`infra-up` 会启动 Redis、NATS JetStream、ClickHouse 和 PostgreSQL。NATS JetStream 使用 Docker Compose 中的文件存储，数据目录为 `data/nats`。

启动 PostgreSQL：

```sh
make postgres-up
```

本地运行 Go 行情服务：

```sh
make go-market-data-run
```

本地运行 Go 策略引擎：

```sh
make go-strategy-engine-run
```

`configs/strategy-engine.local.toml` 默认启用 ClickHouse 事件存储，因此本地运行策略引擎前需要先启动 Redis 和 ClickHouse。

构建 Go 策略引擎：

```sh
make go-strategy-engine-build
```

本地运行 Go 回测引擎骨架：

```sh
make go-backtest-engine-run
```

构建 Go 回测引擎：

```sh
make go-backtest-engine-build
```

本地运行 Go 仓位/执行路由引擎骨架：

```sh
make go-position-engine-run
```

构建 Go 仓位/执行路由引擎：

```sh
make go-position-engine-build
```

使用 Docker Compose 启动 Redis、NATS JetStream、ClickHouse、PostgreSQL 和 market-data：

```sh
make stack-up
```

只启动基础设施：

```sh
make infra-up
```

只启动在线行情栈：

```sh
make live-up
```

本地运行旧 Python 策略原型：

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

Docker 下只跑 K 线维护脚本时，可以使用 jobs profile；它只依赖 ClickHouse，不会拉起 Redis、PostgreSQL 或 market-data：

```sh
make kline-check
make kline-backfill
make kline-delete-dryrun
```

这些命令默认读取 `backend/go-service/configs/tasks/kline-default.toml`；删除命令默认读取 `backend/go-service/configs/tasks/kline-delete-default.toml`。临时覆盖日期或其他 CLI 参数时使用 `ARGS`：

```sh
make kline-check ARGS='--start 202606010000 --end 202607010000'
```

`market-data-admin` 是一次性 CLI，不作为服务常驻。它只维护 ClickHouse 里的已闭合 K 线历史，指标不再写入 ClickHouse，也不由该工具维护。时间参数使用 `YYYYMMDDHHmm`，范围语义统一为左闭右开：`start <= open_time < end`。

需要稳定执行或定时任务时，先编译本地二进制：

```sh
make go-market-data-build
```

Go 二进制统一输出到 `backend/go-service/bin/`，例如：

```sh
backend/go-service/bin/market-data-admin --config backend/go-service/configs/market-data.local.toml stats --exchange binance --market um --symbol ETHUSDT --intervals 1m,3m,5m,10m,15m,30m,1h,2h,4h --start 202605010000 --end 202607010000
```

Go 工程运行资产统一收口：

```text
backend/go-service/bin/      # Go 编译产物，按二进制文件名区分服务
backend/go-service/configs/  # Go 服务配置，按 {service}.{env}.toml 命名
backend/go-service/docker/   # Go 服务镜像构建文件，按 {service}.Dockerfile 命名
logs/go-service/             # Go 服务日志，按日志文件名区分服务
```

清理本地编译产物：

```sh
make go-market-data-clean
```

常用 `market-data-admin` 命令：

- `inventory`：查看库里有什么数据，以及逻辑行、物理行、重复行和首尾 open time。
- `stats`：按交易所、交易对、周期和时间段输出完整性总览。
- `check`：严格校验某段时间是否缺 K 线，可输出缺失 open time。
- `duplicates`：查看 ClickHouse 物理重复版本。
- `backfill`：只回填缺失 K 线；默认 `skip-existing`，可安全重复执行；加 `--async` 时提交 NATS JetStream 任务，由 `market-data` 进程内 worker 消费执行。
- `delete`：删除某段 K 线历史；默认 dry-run，必须传 `--confirm` 才会真实删除。

运行 Go 行情采集负载测试：

```sh
cd backend/go-service/market-data
go run ./cmd/market-data-loadtest -symbols=50 -duration=30s -rate=5000 -store-latency=1ms
```

运行 Go 指标计算负载测试：

```sh
cd backend/go-service
go run ./market-data/cmd/market-data-indicator-loadtest -symbols=10 -lookback=100 -runs=1 -redis-latency=1ms
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

行情和特征流：

```text
交易所 REST/WebSocket
  -> Go market-data collector
  -> Redis 实时状态 + ClickHouse 已闭合 K 线历史
  -> NATS JetStream 内部补偿队列（ClickHouse pending / async backfill）
  -> Go 指标计算 + 指标窗口聚合
  -> Redis 特征 hash
```

在线策略和仓位路由流：

```text
Redis 特征 hash
  -> strategy.Snapshot
  -> strategy.Engine
  -> strategy-engine/internal/runner
  -> NATS JetStream strategy.decision
  -> position-engine
  -> paper handler / 未来 testnet/live/notify handler
  -> Redis 当前仓位 + ClickHouse strategy_events
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
