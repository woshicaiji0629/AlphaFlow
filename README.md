# AlphaFlow

AlphaFlow 是一个面向合约交易与事件合约研究的量化系统。当前重点是打通行情采集、指标特征、在线策略、模拟执行、历史回测和研究数据链路；真实账户自动交易仍处于联调和验收阶段。

## 当前能力

- `market-data`：采集 Binance、Gate、Bitget、Bybit 行情，生成派生 K 线、指标和窗口特征。
- `strategy-engine`：消费实时市场快照并运行 Go 策略。
- `position-engine`：处理 paper 仓位以及 testnet/live 仓位计划。
- `execution-engine`：支持 paper、testnet、live 和多账户执行路由。
- `backtest-engine`：完成数据检查、无未来函数回测、模拟成交和结果持久化。
- `polymarket-research`：采集 Polymarket 币种涨跌事件合约、盘口、成交、参考价格和结算结果，用于多币种、多周期研究。
- `control-api` 与 `frontend`：提供认证、Dashboard、策略目录和管理控制台。

项目以 Go 为实时主路径。Redis 保存实时状态和恢复缓存，NATS JetStream 承担服务间消息与内部补偿队列，ClickHouse 保存 K 线、策略事件、回测结果和 Polymarket 研究数据，PostgreSQL 保存控制面业务数据。

## 项目结构

```text
AlphaFlow/
  backend/
    go-service/
      market-data/             # 行情、K 线、指标和窗口特征
      strategy-engine/         # 在线策略引擎
      position-engine/         # 仓位与执行路由
      execution-engine/        # 多账户订单执行
      backtest-engine/         # 历史回测
      polymarket-research/     # Polymarket 事件合约采集与研究
      control-api/             # 控制面 API
      pkg/                     # Go 公共模型与基础能力
    python-service/
      alphaflow-core/          # 旧 Python 策略原型
  frontend/                    # React + TypeScript + Vite 控制台
  docs/                        # 架构、服务和研究文档
  data/                        # 本地基础设施数据
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
- [策略系统](docs/strategies/README.md)
- [Go 策略引擎](docs/strategies/go-strategy-engine.md)
- [Polymarket 事件合约研究](docs/polymarket-research.md)
- [控制面 API](docs/control-api.md)

## 当前边界

- Polymarket 模块当前仅采集和研究，不包含下单接口或自动交易。
- 研究报表输出的是未扣手续费和滑点的毛收益，不应直接视为可实现收益。
- testnet/live 仍需使用真实交易所凭证完成端到端联调、小额订单验收和账户级风控。
- 前端账户、订单和完整运营管理能力仍在建设中。
