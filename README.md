# AlphaFlow

AlphaFlow 是一个面向合约交易与事件合约研究的量化系统。当前重点是打通行情采集、指标特征、在线策略、模拟执行、历史回测和研究数据链路；真实账户自动交易仍处于联调和验收阶段。

## 当前能力

- `market-data`：采集 Binance、Gate、Bitget、Bybit 行情，生成派生 K 线、指标和窗口特征。
- `strategy-engine`：消费实时市场快照并运行 Go 策略。
- `position-engine`：处理 paper 仓位以及 testnet/live 仓位计划。
- `execution-engine`：支持 paper、testnet、live 和多账户执行路由。
- `backtest-engine`：统一提供正式回测和回测数据完整性检查，复用在线指标与策略代码完成多周期流式回放、无未来函数校验、模拟成交、结果持久化和回测报告。
- `market-research`：统一提供独立市场波段、市场状态、未来标签、结构状态和 Supertrend 信号研究；研究输出不等于可成交收益或正式策略授权。
- `polymarket-research`：采集 Polymarket 币种涨跌事件合约、盘口、成交、参考价格和结算结果，用于多币种、多周期研究。
- `control-api` 与 `frontend`：提供认证、Dashboard、策略目录和管理控制台。

项目以 Go 为实时主路径。Redis 保存实时状态和恢复缓存，NATS JetStream 承担服务间消息与内部补偿队列，ClickHouse 保存 K 线、策略事件、回测结果和 Polymarket 研究数据，PostgreSQL 保存控制面业务数据。

## 项目结构

```text
AlphaFlow/
├── backend/
│   └── go-service/                         # Go 实时服务、回测、研究工具和公共包
│       ├── market-data/                    # 交易所行情采集、K 线聚合、指标和窗口特征
│       │   ├── cmd/
│       │   │   ├── market-data/            # 常驻行情服务入口
│       │   │   ├── market-data-admin/      # 补数、队列状态和行情健康检查管理入口
│       │   │   ├── market-data-symbols/    # 拉取交易所活跃交易对并生成配置
│       │   │   ├── market-data-loadtest/   # 行情采集与存储链路压测入口
│       │   │   └── market-data-indicator-loadtest/ # 指标计算和 Redis 写入压测入口
│       │   └── internal/                   # 采集器、聚合器、指标、补偿队列和存储实现
│       ├── strategy-engine/                # 消费市场快照并执行在线 Go 策略
│       │   ├── cmd/strategy-engine/        # 在线策略引擎常驻入口
│       │   └── internal/                   # 配置、行情内存态、snapshot reader 和 runner
│       ├── position-engine/                # 策略仓位、退出规则和执行路由
│       │   ├── cmd/position-engine/        # paper/testnet/live 仓位处理常驻入口
│       │   └── internal/                   # 配置和仓位路由编排
│       ├── execution-engine/               # 多账户订单执行、恢复、对账和回报发布
│       │   ├── cmd/execution-engine/       # paper/testnet/live 执行服务常驻入口
│       │   └── internal/                   # 账户 fan-out、执行适配和配置
│       ├── backtest-engine/                # 历史数据检查、流式回测和策略研究
│       │   ├── cmd/
│       │   │   ├── backtest-engine/        # 正式回测与数据检查统一入口
│       │   │   └── market-research/        # 市场标签、状态和策略研究统一入口
│       │   └── internal/                   # 配置、历史读取、模拟、报告和研究实现
│       ├── control-api/                    # 用户、账户、权限和策略控制面 HTTP API
│       │   ├── cmd/control-api/            # 控制面 API 常驻入口
│       │   ├── cmd/control-api-admin/      # 创建管理员等控制面管理入口
│       │   └── internal/                   # API、认证、领域、服务、仓储和迁移实现
│       ├── polymarket-research/            # Polymarket 行情、盘口、成交与结算研究
│       │   ├── cmd/polymarket-research/    # 实时研究数据采集入口
│       │   ├── cmd/polymarket-research-report/ # 历史盘口研究报告入口
│       │   └── internal/                   # Gamma、CLOB、RTDS、研究和存储实现
│       ├── pkg/                            # 跨服务复用的公共协议与基础能力
│       │   ├── indicatorcalc/              # 连续指标计算
│       │   ├── marketmodel/                # K 线等公共行情模型
│       │   ├── marketregime/               # 只使用当时可见数据的实时市场状态研究
│       │   ├── signalresearch/             # 离线信号回放、未来路径和标签研究
│       │   ├── strategies/supertrend/      # Supertrend 正式策略实现
│       │   ├── strategyframe/              # 在线与回测共享的策略上下文组装
│       │   ├── positionhandler/            # paper 等仓位处理公共实现
│       │   ├── executionadapter/           # 各交易所统一执行适配器
│       │   └── *bus/                       # NATS 行情、策略和执行消息协议
│       ├── configs/                        # 本地、回测、研究、testnet 和 live 配置样例
│       ├── docker/                         # Go 服务相关容器文件
│       └── go.mod                          # Go 模块和依赖声明
├── frontend/                               # React + TypeScript + Vite 管理控制台
│   ├── src/main.tsx                        # 前端启动、路由、鉴权和 QueryClient 入口
│   ├── src/pages/                          # 登录、策略、绩效和管理页面
│   ├── src/auth/                           # 前端认证状态与受保护路由
│   ├── package.json                        # 前端依赖和 dev/build/typecheck 命令
│   └── vite.config.ts                      # Vite 构建与开发配置
├── docs/                                   # 系统架构、服务说明、策略和研究结论
│   └── strategies/                         # 策略语义、市场状态和优化记录
├── data/                                   # Redis、NATS、ClickHouse 等本地持久化数据
├── Makefile                                # 基础设施、服务运行、检查和管理命令
└── docker-compose.yml                      # 本地基础设施编排
```

## 快速开始

启动 Redis、NATS JetStream、ClickHouse 和 PostgreSQL：

```sh
make infra-up
```

运行主要 Go 服务：

```sh
make go-market-data-run
make go-strategy-engine-run
make go-position-engine-run
make go-backtest-engine-run
```

运行全部可用检查：

```sh
make check
```

检查并运行 ETHUSDT 一年期回测：

```sh
cd backend/go-service
go run ./backtest-engine/cmd/backtest-engine dataset-check \
  -config configs/backtest-engine.ethusdt-1y.toml
go run ./backtest-engine/cmd/backtest-engine run \
  -config configs/backtest-engine.ethusdt-1y.toml
```

### 回测与市场研究入口

`backtest-engine/cmd` 只保留两个长期入口。版本和研究主题通过子命令或参数表达，不再为 V4、V5、V6、V7 等版本新增 `main`。

| 入口 | 子命令 | 职责 |
| --- | --- | --- |
| `backtest-engine` | `run` | 运行正式策略回测并生成、持久化回测结果。 |
| `backtest-engine` | `dataset-check` | 在回测前检查多周期数据缺口、重复时间戳、连续区间和 warmup。 |
| `market-research` | `swing` | 生成并持久化与策略、批次无关的独立市场波段。 |
| `market-research` | `analysis` | 独立回放市场分析 V4/V5/V6 并持久化观察结果。 |
| `market-research` | `forward-label` | 生成未来路径和市场标签分布。 |
| `market-research` | `structure-regime` | 研究市场结构、Episode 和状态机特征。 |
| `market-research` | `supertrend-signal` | 运行 Supertrend 信号、单持仓和版本对照研究。 |

常用调用方式：

```sh
cd backend/go-service

go run ./backtest-engine/cmd/backtest-engine run -config configs/backtest-engine.local.toml
go run ./backtest-engine/cmd/backtest-engine dataset-check -config configs/backtest-engine.local.toml

go run ./backtest-engine/cmd/market-research swing -config configs/supertrend-signal-research.ethusdt-20250801-20251101.toml
go run ./backtest-engine/cmd/market-research analysis -config configs/supertrend-signal-research.ethusdt-20250801-20251101.toml
go run ./backtest-engine/cmd/market-research forward-label -help
go run ./backtest-engine/cmd/market-research structure-regime -help
go run ./backtest-engine/cmd/market-research supertrend-signal -help
```

为兼容已有脚本，`backtest-engine -config ...` 暂时等价于 `backtest-engine run -config ...`。旧的独立研究命令路径已经删除，外部脚本应迁移到 `market-research` 子命令。

配置文件名中的 `1y` 不是区间保证；运行前必须检查 `[data].start_time` 和 `[data].end_time`。仓库当前 `backtest-engine.ethusdt-1y.toml` 实际覆盖 `2025-09-01` 至 `2025-12-01`，若要做完整年度比较，需要显式改为连续 365 天并先执行数据集检查。

回测按已闭合 K 线流式推进，确认周期只有真正收盘后才进入策略上下文。回测仓位保存在进程内并使用独立 `bt` scope，不写在线 Redis 仓位；策略事件、成交明细和 run summary 持久化到 ClickHouse。长回测日志最长每 10 秒输出处理速率和 ETA。

运行 Polymarket 研究采集器：

```sh
cd backend/go-service
go run ./polymarket-research/cmd/polymarket-research \
  -config configs/polymarket-research.local.toml
```

查询到期前 5 分钟的历史盘口研究结果：

```sh
cd backend/go-service
go run ./polymarket-research/cmd/polymarket-research-report \
  -config configs/polymarket-research.local.toml \
  -start 202607110000 \
  -end 202607120000 \
  -entry-seconds 300
```

更多命令和配置说明见对应专项文档及 [Makefile](Makefile)。

## 核心数据流

```text
交易所 REST/WebSocket
  -> market-data
  -> Redis 实时状态 + ClickHouse K 线
  -> NATS market snapshot
  -> strategy-engine
  -> NATS strategy decision
  -> position-engine
  -> paper / execution-engine
```

```text
ClickHouse 历史 K 线
  -> backtest-engine 多周期流式指标状态
  -> strategyframe.Context
  -> 同一份 Go Strategy
  -> 内存回测仓位 + PaperBroker
  -> ClickHouse strategy_events / backtest_trades / backtest_run_summary
```

```text
Polymarket Gamma + CLOB WebSocket + RTDS
  -> polymarket-research
  -> ClickHouse 市场、盘口、成交、参考价格和结算数据
  -> research report
```

## 文档

- [系统架构](docs/architecture.md)
- [项目进度](docs/progress.md)
- [行情服务](docs/market-data.md)
- [指标系统](docs/indicators.md)
- [性能优化记录](docs/performance-optimization-history.md)
- [策略系统](docs/strategies/README.md)
- [Go 策略引擎](docs/strategies/go-strategy-engine.md)
- [市场波段与开单准入实验归档](docs/strategies/market-swing-admission-research-2025-08-10.md)
- [Polymarket 事件合约研究](docs/polymarket-research.md)
- [控制面 API](docs/control-api.md)

## 当前边界

- Polymarket 模块当前仅采集和研究，不包含下单接口或自动交易。
- 研究报表输出的是未扣手续费和滑点的毛收益，不应直接视为可实现收益。
- `forward-market-label.v1` 使用未来行情生成，只能作为离线研究和监督目标，禁止进入在线 snapshot、策略评分、仓位授权或执行路径；当前只完成连续指标与训练分布，六类互斥标签阈值尚未冻结。
- testnet/live 仍需使用真实交易所凭证完成端到端联调、小额订单验收和账户级风控。
- 前端账户、订单和完整运营管理能力仍在建设中。

在线与回测共用 `CalculationWindow`、连续指标状态、窗口语义和 Go 策略实现。基础指标、AI Source 和 Adaptive Supertrend 已支持连续递推；realtime preview 使用与 closed state 隔离的临时窗口，回测指标窗口按策略读取惰性分析，避免整年预计算和重复窗口扫描。性能结果、复现命令和 top500 冷启动边界统一记录在[性能优化记录](docs/performance-optimization-history.md)中。
