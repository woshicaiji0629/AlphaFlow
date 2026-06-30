# AlphaFlow

AlphaFlow 是一个智能交易系统项目。当前实现重点包括实时行情采集、派生 K 线聚合、技术指标计算、基于 Redis 的实时数据交接、基于 ClickHouse 的行情历史存储，以及带持仓管理能力的 Python 策略框架原型。

## 当前状态

- `backend/go-service/market-data` 是当前活跃的行情数据服务。
- `backend/python-service/alphaflow-core` 是当前活跃的策略框架原型。
- Redis 用作实时行情缓存、服务间交接层，以及活跃策略仓位存储。
- ClickHouse 存储已闭合 K 线和指标历史，用于后续研究、回测、报表和 API 工作流。
- PostgreSQL 存储已平仓策略仓位，用于分析策略表现。
- 本地配置默认启用 Binance 和 Gate。Bitget 和 Bybit 适配器已存在，但默认关闭。
- Python 服务可以读取 Redis 行情快照、ClickHouse 指标历史，执行多策略研判，在 Redis 管理活跃仓位，并把已平仓仓位持久化到 PostgreSQL。
- Frontend 预留给未来的 React + TypeScript 应用。

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
  frontend/                        # 预留给未来前端
  data/                            # 本地运行数据，包括 Redis、ClickHouse、PostgreSQL 数据
  logs/                            # 本地服务日志
```

## 优先阅读

- [docs/architecture.md](docs/architecture.md) 说明服务边界、当前架构和计划模块。
- [docs/market-data.md](docs/market-data.md) 说明已实现的 Go 行情服务、Redis key、指标、本地运行命令和已知限制。
- [Makefile](Makefile) 是主要的本地命令入口。

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

## 重要说明

- Redis 用于实时行情状态、短窗口缓存、服务交接和活跃策略仓位。
- ClickHouse 用于已闭合 K 线和指标历史。
- PostgreSQL 用于已平仓策略仓位和策略表现分析。
- 当前策略框架是信号生成和模拟持仓管理原型。真实交易所下单执行、回测、账户级风控、API 工作流和前端还不是生产模块。
- 文档应始终和实际实现保持一致。未来想法需要明确标记为计划项，不要写成当前行为。
