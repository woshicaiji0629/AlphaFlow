# Go 策略引擎设计

本文档描述 Go-only 策略执行引擎的目标边界。这里记录的是设计方向，不代表当前已经实现。

## 目标

策略体系后续统一使用 Go 实现：

- 在线策略引擎是独立常驻服务。
- 回测是批处理脚本或 CLI。
- 策略实现放在 Go 公共包，供在线引擎和回测共同使用。
- Python 策略框架只作为旧原型参考，不作为新架构依赖。
- 订单执行预留独立服务边界，策略引擎先只生成订单意图或仓位计划。

核心要求是在线和回测共享同一套策略逻辑，差异只存在于数据来源和成交模型。

## 服务形态

### 在线策略引擎

在线策略引擎是独立 Go 服务，建议目录：

```text
backend/go-service/strategy-engine/
  cmd/strategy-engine/main.go
  internal/app/
  internal/config/
  internal/reader/
  internal/store/
  internal/service/
```

在线引擎职责：

- 加载策略、交易对、周期和风控配置。
- 从 Redis 读取实时指标特征和已收盘窗口特征。
- 构造公共 `strategy.Snapshot`。
- 调用公共 `strategy.Engine`。
- 生成信号、仓位计划和订单意图。
- 持久化信号日志、仓位状态和订单意图。
- 做在线幂等控制，避免同一根 K 线重复开仓。
- 暴露健康检查和运行日志。

在线引擎不负责：

- 历史批量回测。
- 全量历史指标重算。
- 直接包含复杂交易所订单状态机。

### 回测批处理

回测是 Go 批处理脚本或 CLI，建议入口：

```text
backend/go-service/scripts/backtest/main.go
```

回测职责：

- 从 ClickHouse 或文件读取历史 K 线。
- 按时间滚动计算指标和窗口语义。
- 构造与在线服务一致的 `strategy.Snapshot`。
- 调用同一个 `strategy.Engine`。
- 用回测成交模型模拟订单成交。
- 输出交易明细、权益曲线、统计摘要和信号诊断。

回测不应复用在线服务的 Redis reader，也不应写在线仓位状态。

## 公共包边界

公共策略逻辑放在 `backend/go-service/pkg` 下，避免依赖具体服务的 `internal` 包。

建议结构：

```text
backend/go-service/pkg/
  strategy/
    model.go
    engine.go
    event.go

  position/
    position.go
    keys.go
    store.go
    memory_store.go
    strategies/
      keltner.go
      supertrend.go

  execution/
    model.go
    broker.go
    paper.go

  backtest/
    runner.go
    datasource.go
    portfolio.go
    report.go
```

公共区可以依赖：

- `pkg/marketmodel`
- `pkg/indicatorcalc`
- `pkg/indicatorwindow`

公共区不应依赖：

- `market-data/internal`
- `strategy-engine/internal`
- Redis 客户端
- ClickHouse 客户端
- 交易所私有 API 客户端
- 服务配置文件结构

## 数据流

在线路径：

```text
Redis indwin / indrt
  -> strategy-engine/internal/reader
  -> strategy.Snapshot
  -> strategy.Engine
  -> strategy-engine/internal/runner
  -> position.Manager
  -> execution.OrderIntent
  -> order-executor 或 PaperBroker
```

回测路径：

```text
历史 K 线
  -> indicatorcalc.CalculateWindow
  -> indicatorwindow.Analyze
  -> strategy.Snapshot
  -> strategy.Engine
  -> BacktestBroker
  -> 回测报告
```

策略层只能看到统一的 `strategy.Snapshot` 和当前仓位快照，不能知道数据来自在线 Redis 还是历史回放。

## 核心接口草案

策略接口：

```go
type Strategy interface {
	Name() string
	RequiredIntervals(target Target) []string
	Evaluate(ctx context.Context, snapshot Snapshot, position *Position) (Result, error)
}
```

引擎接口：

```go
type Engine struct {
	strategies []Strategy
}

func (e *Engine) Evaluate(ctx context.Context, input Context) (Decision, error)
```

订单执行接口：

```go
type Broker interface {
	Execute(ctx context.Context, intent OrderIntent) (ExecutionReport, error)
}
```

第一阶段可以只实现接口和基础模型，不接真实交易所。

## Snapshot 设计

`strategy.Snapshot` 是在线和回测共享的策略输入。

建议字段：

```go
type Snapshot struct {
	Target     Target
	Current    KlineView
	Indicator  IndicatorView
	Window     IndicatorWindowView
	Timeframes map[string]TimeframeSnapshot
	Price      PriceView
	Health     HealthView
	UpdatedAt  int64
}
```

设计约束：

- `Current` 表示当前入场周期 K 线。
- `Indicator` 表示当前入场周期指标。
- `Window` 表示上一根已收盘 K 线对应的窗口语义。
- `Timeframes` 只包含当时已经可用的确认周期窗口。
- 回测构造多周期窗口时必须避免未来函数。

## 策略输出

策略输出不直接下单，而是返回信号和计划输入：

```text
Signal
- side: buy / sell / hold
- score
- confidence
- reason
- open_time
- updated_at

Result
- strategy_name
- signal
- analysis
- exit_rules
```

仓位管理器基于 `Result` 和当前 `Position` 生成 `OrderPlan` 或 `OrderIntent`。

## 订单边界

策略引擎不直接绑定真实交易所下单。

策略引擎生成：

```text
OrderIntent
- intent_id
- idempotency_key
- strategy_name
- exchange
- account
- market
- symbol
- action: open / close / reduce / reverse
- side: buy / sell
- order_type: market / limit / stop
- quantity
- limit_price
- stop_price
- reduce_only
- reason
- created_at
```

订单执行服务或 broker 生成：

```text
ExecutionReport
- intent_id
- exchange_order_id
- status
- filled_quantity
- avg_fill_price
- fee
- error
- updated_at
```

真实交易前应拆出 `order-executor` 服务。MVP 阶段可以在在线引擎内使用 `PaperBroker`，但接口按独立订单服务边界设计。

## 多策略决策

第一阶段规则保持简单：

- 先处理已有仓位的退出和风控。
- 同一策略、同一交易对最多一个活跃仓位。
- 同一策略不允许多空双开。
- 多个策略同时产生信号时，先采用最高置信度信号。
- 反向信号默认先平旧仓，不在同一次决策里直接反手开新仓。

后续可以把冲突处理放入 `DecisionPolicy`：

- 策略优先级。
- 置信度差阈值。
- 账户级最大风险。
- 同向信号合并。
- 反向信号冷却期。

## 持仓与风控

公共 `pkg/position` 应提供与运行形态无关的仓位逻辑：

- 开仓计划。
- 平仓计划。
- 止盈。
- 止损。
- 移动止损。
- 分批退出。
- 当前仓位最高价和最低价更新。

在线服务负责持久化仓位状态。回测可以使用内存 store 或临时 Redis key，并在脚本结束时清理。

保证金、杠杆、手续费、滑点和仓位大小必须配置化，不应硬编码在策略实现里。

第一阶段回测和在线模拟仓统一按计价币名义价值 sizing：

```text
target_notional = margin_quote * leverage
base_qty = target_notional / price
```

默认业务配置可以使用：

```text
margin_quote = 100
leverage = 100
fee_rate = 0.0006
rebate_pct = 0
```

`fee_rate` 和 `rebate_pct` 只用于 `bt` / `paper` 的模拟成交。`rebate_pct` 取值为 `0-100`，表示返还手续费百分比，例如 `50` 表示返还一半手续费。`testnet` / `live` 必须以交易所返回的真实成交数量、成交均价和手续费为准。

## 仓位 Scope 和 Redis Key

仓位分为四类 scope：

| Scope | 含义 | 当前是否实现 | 是否接交易所 API | 是否需要账户 | 是否按策略隔离 |
| --- | --- | --- | --- | --- | --- |
| `bt` | 离线回测临时仓位 | 是 | 否 | 否 | 是 |
| `paper` | 在线本地模拟仓位 | 是 | 否 | 否 | 是 |
| `testnet` | 交易所测试网或 demo 仓位 | 预留 | 是 | 是 | 否，交易所净仓位 |
| `live` | 交易所实盘仓位 | 预留 | 是 | 是 | 否，交易所净仓位 |

`bt` 和 `paper` 都是本地策略仓位，不考虑账户管理。每个 `exchange + market + symbol + strategy` 独立维护自己的仓位。例如 Binance USD-M 永续 `ETHUSDT` 上 10 个策略会有 10 个独立 paper 仓位。

`testnet` 和 `live` 是未来交易所 API 对接后的账户级仓位。交易所返回的是账户级净仓位或分仓模式仓位，不天然属于某个策略。策略归因需要单独做内部账本，不应混入交易所真实仓位主协议。

建议 Redis key：

```text
# 离线回测，脚本启动时生成随机 run_id，结束后删除
st:pos:bt:{run_id}:{exchange}:{market}:{symbol}:{strategy}

# 在线本地模拟仓，长期保留
st:pos:paper:{exchange}:{market}:{symbol}:{strategy}

# 交易所测试网或 demo，预留
st:pos:testnet:{account}:{exchange}:{market}:{symbol}:{position_side}

# 交易所实盘，预留
st:pos:live:{account}:{exchange}:{market}:{symbol}:{position_side}

# 离线临时 key 注册，用于脚本结束时批量删除
st:bt:{run_id}:keys
```

`position_side` 用于兼容交易所单向和分仓模式：

```text
net    # 单向净仓
long   # hedge 模式多仓
short  # hedge 模式空仓
```

离线脚本写入 Redis 临时数据时必须登记到 `st:bt:{run_id}:keys`，并为 key 设置 TTL。脚本正常结束时删除 registry 中登记的所有 key；脚本异常退出时由 TTL 兜底。公共 `pkg/position.RedisStore` 负责当前仓位 JSON value 的读写、`bt` 临时 key 登记和清理；`paper` 默认不设置 TTL，长期保留当前模拟仓位。

`bt` / `paper` 的仓位 value 不需要 `account`。`testnet` / `live` 的仓位 value 必须包含账户、交易所原始仓位模式、原始方向字段和同步时间。

## 交易所账户和交易能力预留

交易所 API 通常可以读取账户余额、可用余额、保证金占用、未实现盈亏、当前持仓、挂单和 symbol 风控限制。未来接入 `testnet` / `live` 时，订单服务在提交订单前必须基于账户和交易能力做最终数量校验。

预留 Redis key：

```text
st:acct:testnet:{account}:{exchange}:{market}
st:acct:live:{account}:{exchange}:{market}

st:cap:{exchange}:{market}:{symbol}
```

预留模型：

```text
AccountSnapshot
- scope
- account
- exchange
- market
- equity
- available_balance
- used_margin
- unrealized_pnl
- updated_at

SymbolCapability
- exchange
- market
- symbol
- min_qty
- qty_step
- min_notional
- max_leverage
- max_order_qty
- contract_size
- updated_at
```

当前阶段 `bt` 和 `paper` 不依赖账户余额，也不根据交易所限制计算可开仓位。它们的仓位大小由本地配置控制，例如固定 size、固定保证金、固定名义价值或风险百分比。

## 持久化分层

策略状态分为运行态和分析态：

```text
Redis      = 当前活跃状态，服务运行时快速读写
ClickHouse = 事件历史和分析数据，append-only
```

Redis 只保存当前状态：

```text
st:pos:bt:{run_id}:{exchange}:{market}:{symbol}:{strategy}
st:pos:paper:{exchange}:{market}:{symbol}:{strategy}
st:pos:testnet:{account}:{exchange}:{market}:{symbol}:{position_side}
st:pos:live:{account}:{exchange}:{market}:{symbol}:{position_side}
```

ClickHouse 保存历史事件和分析结果。回测脚本结束时可以删除 Redis 临时 key，但回测结果和事件历史必须保留在 ClickHouse 或导出的结果文件中。公共 `pkg/position.ClickHouseStore` 负责写入 `strategy_events` 和 `backtest_run_summary`；为了降低 ClickHouse 类型兼容成本，`metadata` 和 `symbols` 第一阶段以 JSON String 保存。

当前建议只维护一张统一事件宽表和一张回测摘要表：

```text
strategy_events
backtest_run_summary
```

`strategy_events` 同时承载 `bt`、`paper`、`testnet` 和 `live` 的信号、仓位、订单、账户快照和交易所仓位同步事件。使用 `scope`、`run_id`、`account` 和 `event_type` 区分来源与语义。

常用可视化筛选和聚合字段必须是独立列，不应只放在 `metadata` 中：

```text
event_id
scope
run_id
account
exchange
market
symbol
strategy_name
event_type
event_time
bar_open_time
side
position_side
position_mode
size
price
notional
fee
pnl
reason
score
confidence
order_id
intent_id
exchange_order_id
metadata
created_at
```

典型事件类型：

```text
signal_generated
position_opened
position_updated
exit_rule_updated
position_reduced
position_closed
order_intent_created
order_filled
account_snapshot
exchange_position_synced
```

`backtest_run_summary` 保存回测 run 级摘要，用于回测任务列表和结果管理：

```text
run_id
status
strategy_set
exchange
market
symbols
start_time
end_time
total_trades
win_rate
net_pnl
max_drawdown
profit_factor
sharpe
failure_reason
metadata
created_at
updated_at
```

可视化管理应读取 ClickHouse，不应直接读取 Redis。Redis 中的当前态只服务策略引擎运行；ClickHouse 中的事件历史服务复盘、报表和横向对比。

策略事件可视化建议先支持以下视图：

```text
事件流：按时间展示 signal、order、position、account 事件
策略表现面板：按 scope、symbol、strategy 聚合收益和胜率
仓位时间线：查看单个策略仓位从开仓到平仓的生命周期
回测 run 管理：按 run_id 查看摘要和事件详情
bt vs paper 对比：同策略回测与在线模拟的信号和收益差异
```

当前阶段不引入 MySQL 或 PostgreSQL 存策略事件。后续如果需要管理后台、账户配置、API key 元数据、权限系统或人工审核状态，再单独引入事务型数据库。

## 幂等要求

在线服务必须生成稳定幂等键，建议至少包含：

```text
exchange
market
symbol
interval
strategy_name
signal_open_time
action
side
```

同一个幂等键重复出现时，在线引擎不能重复写入新的订单意图。

## 第一阶段实施范围

第一阶段只建立公共策略引擎骨架：

1. `pkg/strategy/model.go`
   - 定义 `Target`、`Snapshot`、`Signal`、`Position`、`ExitRule`、`Decision`。
2. `pkg/strategy/engine.go`
   - 定义 `Strategy` 接口和只产出策略结果的基础 `Engine`。
3. `pkg/position/position.go`
   - 定义独立仓位管理器，按策略信号和当前仓位生成仓位计划。
4. `pkg/position/keys.go`
   - 定义 `bt`、`paper`、`testnet`、`live` 当前仓位 key 协议。
5. `pkg/execution/model.go`
   - 定义 `OrderIntent` 和 `ExecutionReport`。
6. `strategy-engine/internal/runner/runner.go`
   - 定义在线策略引擎内部编排层：读取策略仓位、调用策略引擎、生成仓位计划、构造订单意图、写入策略事件。
   - 对 `bt` 和 `paper` 的本地成交结果更新当前仓位；`testnet` 和 `live` 只记录事件，不在策略引擎内改交易所账户级仓位。
   - 对 `bt` 和 `paper` 支持可配置手续费率，成交事件写入 `notional`、`fee`、`pnl`、`reason` 和 `metadata.return_pct`。
   - 对 `bt` 和 `paper` 支持 `margin_quote * leverage` 的名义价值 sizing，并把 `fee_rate`、`rebate_pct`、`gross_fee`、`rebate`、`margin_quote`、`leverage` 写入事件 metadata。
   - 每次处理前刷新本地仓位最高价/最低价，用于移动止损；触发止盈、止损、移动止损或分批退出后会生成退出订单意图，并把 `exit_reason`、`trigger_price`、`size_pct` 写入事件 metadata。

第一阶段不做：

- 真实交易所下单。
- Redis reader。
- ClickHouse 回测读取。
- 新数据库 schema。
- 完整在线服务入口。

## 风险点

- 公共策略包如果依赖服务 `internal` 包，在线和回测会被绑死。
- 多周期窗口如果对齐错误，回测会出现未来函数。
- 订单意图如果没有幂等键，在线服务重启或重复扫描可能重复下单。
- 回测成交规则会显著影响结果，必须明确记录是按 close、next open 还是 intrabar stop/limit 模拟。
- 真实订单状态机复杂，应在订单服务中单独设计，不应塞进策略引擎。
