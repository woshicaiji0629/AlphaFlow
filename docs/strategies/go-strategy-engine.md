# Go 策略引擎设计

本文档记录当前 Go-only 策略执行、仓位、执行和事件持久化的实际边界，以及后续规划。

## 当前状态

已经实现：

- 公共策略模型和策略引擎：`pkg/strategy`
- 独立仓位管理、Redis 当前态、ClickHouse 事件态：`pkg/position`
- 订单意图、执行报告和 paper broker：`pkg/execution`
- 在线策略引擎内部 runner：`strategy-engine/internal/runner`
- `bt` / `paper` 本地策略仓位隔离
- 止盈、止损、移动止损、分批退出
- 回测和模拟仓 sizing：`margin_quote * leverage`
- 模拟手续费和返佣估算：`fee_rate` + `rebate_pct`
- ClickHouse 事件表：`strategy_events`
- ClickHouse 回测摘要表：`backtest_run_summary`

尚未实现：

- `cmd/strategy-engine/main.go` 服务入口
- 在线配置加载
- Redis snapshot reader
- 真实交易所 order executor
- 回测 CLI / 批处理入口
- ClickHouse 历史 K 线回测读取编排
- 交易所账户级风控和 symbol 下单能力换算

## 目标边界

策略体系统一使用 Go 实现：

- 在线策略引擎是独立常驻服务。
- 回测是批处理脚本或 CLI。
- 策略实现放在 Go 公共包，供在线引擎和回测共同使用。
- Python 策略框架只作为旧原型参考，不作为新架构依赖。
- 策略引擎只产出策略结果、仓位计划和订单意图。
- 真实订单状态机后续拆到订单服务。

核心要求是在线和回测共享同一套策略逻辑，差异只存在于数据来源、成交模型和持久化范围。

## 包职责

### `pkg/strategy`

职责：

- 定义策略输入 `Snapshot`
- 定义策略输出 `Signal` / `Result`
- 定义仓位和事件公共模型
- 定义 `Strategy` 接口
- 定义只调用策略并返回结果的 `Engine`

关键文件：

```text
backend/go-service/pkg/strategy/model.go
backend/go-service/pkg/strategy/engine.go
backend/go-service/pkg/strategy/event.go
```

`Engine` 不做仓位管理，不做下单，不做策略之间的全局账户级合并。

### `pkg/position`

职责：

- 生成本地策略仓位计划
- 管理止盈、止损、移动止损、分批退出
- 维护 Redis 当前仓位 key 协议
- 提供内存 store
- 提供 Redis current-state store
- 提供 ClickHouse event store
- 提供回测临时 key registry

关键文件：

```text
backend/go-service/pkg/position/position.go
backend/go-service/pkg/position/keys.go
backend/go-service/pkg/position/store.go
backend/go-service/pkg/position/memory_store.go
backend/go-service/pkg/position/redis_store.go
backend/go-service/pkg/position/clickhouse_store.go
```

### `pkg/execution`

职责：

- 定义订单意图 `OrderIntent`
- 定义执行报告 `ExecutionReport`
- 将 `OrderPlan` 转成 `OrderIntent`
- 提供 paper broker
- 预留账户快照和 symbol 能力模型

关键文件：

```text
backend/go-service/pkg/execution/model.go
backend/go-service/pkg/execution/intent.go
backend/go-service/pkg/execution/broker.go
backend/go-service/pkg/execution/paper.go
```

### `strategy-engine/internal/runner`

职责：

- 从 `PositionStore` 读取每个策略的当前仓位
- 调用 `strategy.Engine`
- 每次处理前刷新本地仓位最高价/最低价
- 调用 `position.Manager` 生成仓位计划
- 将名义价值 sizing 转成基础资产数量
- 构造订单意图
- 调用 broker 执行 paper 成交
- 写入 `signal_generated`、`order_intent_created`、`order_filled` 事件
- 对 `bt` / `paper` 的本地成交结果更新当前仓位

`runner` 不负责：

- Redis 指标字段读取
- 服务配置加载
- 真实交易所账户仓位修改
- 交易所订单状态机
- 交易所精度和张数换算

## 服务形态

### 在线策略引擎

目标目录：

```text
backend/go-service/strategy-engine/
  cmd/strategy-engine/main.go
  internal/app/
  internal/config/
  internal/reader/
  internal/runner/
```

当前只实现了：

```text
backend/go-service/strategy-engine/internal/runner/
```

在线路径目标：

```text
Redis indwin / indrt
  -> strategy-engine/internal/reader
  -> strategy.Snapshot
  -> strategy.Engine
  -> strategy-engine/internal/runner
  -> position.Manager
  -> execution.OrderIntent
  -> order-executor 或 PaperBroker
  -> position.EventStore
```

在线引擎职责：

- 加载策略、交易对、周期和风控配置。
- 从 Redis 读取实时指标特征和已收盘窗口特征。
- 构造公共 `strategy.Snapshot`。
- 调用公共 `strategy.Engine`。
- 生成信号、仓位计划和订单意图。
- 写入事件历史和当前仓位。
- 做在线幂等控制，避免同一根 K 线重复开仓。
- 暴露健康检查和运行日志。

### 回测批处理

目标入口：

```text
backend/go-service/scripts/backtest/main.go
```

回测路径目标：

```text
历史 K 线
  -> indicatorcalc.CalculateWindow
  -> indicatorwindow.Analyze
  -> strategy.Snapshot
  -> strategy.Engine
  -> strategy-engine/internal/runner 或 backtest runner
  -> BacktestBroker
  -> ClickHouse / 回测报告
```

回测职责：

- 从 ClickHouse 或文件读取历史 K 线。
- 按时间滚动计算指标和窗口语义。
- 构造与在线服务一致的 `strategy.Snapshot`。
- 调用同一套策略和仓位管理逻辑。
- 用回测成交模型模拟订单成交。
- 输出交易明细、权益曲线、统计摘要和信号诊断。

回测不应复用在线服务的 Redis reader，也不应写在线 paper 仓位状态。

## Snapshot

`strategy.Snapshot` 是在线和回测共享的策略输入。

当前字段：

```go
type Snapshot struct {
	Target     Target
	Current    marketmodel.Kline
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
- `Window` 表示已收盘 K 线对应的窗口语义。
- `Timeframes` 只包含当时已经可用的确认周期窗口。
- 回测构造多周期窗口时必须避免未来函数。

策略层只能看到 `strategy.Snapshot` 和当前仓位快照，不知道数据来自在线 Redis 还是历史回放。

## 策略接口

```go
type Strategy interface {
	Name() string
	RequiredIntervals(target Target) []string
	Evaluate(ctx context.Context, snapshot Snapshot, position *Position) (Result, error)
}
```

策略返回：

```text
Result
- strategy_name
- signal
- analysis
- exit_rules
```

策略不直接下单，也不直接写仓位。

## 仓位模型

当前约定：

- `Position.Size` 保存实际基础资产数量。
- `OrderIntent.Quantity` 保存实际基础资产数量。
- `OrderPlan.TargetSize` 在启用 `MarginQuote + Leverage` 时表示目标名义价值。
- runner 在执行前用当前价格把目标名义价值换成基础资产数量。

例子：

```text
margin_quote = 100
leverage = 100
price = 2000

target_notional = 100 * 100 = 10000 USDT
base_qty = 10000 / 2000 = 5 ETH
```

未配置 `SizingConfig.MarginQuote` 和 `SizingConfig.Leverage` 时，保持兼容旧行为：`TargetSize` 直接作为基础资产数量。

## Sizing、手续费和返佣

`bt` 和 `paper` 使用本地配置估算交易成本：

```text
target_notional = margin_quote * leverage
base_qty = target_notional / price
gross_fee = notional * fee_rate
rebate = gross_fee * rebate_pct / 100
net_fee = gross_fee - rebate
```

默认业务配置：

```text
margin_quote = 100
leverage = 100
fee_rate = 0.0006
rebate_pct = 0
```

`rebate_pct` 取值为 `0-100`：

```text
0   = 不返佣
50  = 返还一半手续费，只承担 50%
60  = 返还 60% 手续费，只承担 40%
100 = 全返
```

事件会写入：

```text
notional
fee
pnl
metadata.gross_fee
metadata.rebate
metadata.fee_rate
metadata.rebate_pct
metadata.margin_quote
metadata.leverage
metadata.return_pct
metadata.return_on_margin_pct
```

`testnet` 和 `live` 不使用本地手续费和返佣规则覆盖成交结果。未来实盘必须以交易所返回的真实成交数量、成交均价和手续费为准。

## 止盈止损

策略可以在 `Result.ExitRules` 中返回退出规则：

```go
type ExitRule struct {
	Type         ExitReasonType
	Reason       string
	TriggerPrice string
	SizePct      float64
	Metadata     map[string]string
}
```

当前支持：

```text
take_profit
stop_loss
trailing_stop
partial_exit
```

规则：

- 固定止盈：多仓价格大于等于触发价，空仓价格小于等于触发价。
- 固定止损：多仓价格小于等于触发价，空仓价格大于等于触发价。
- 移动止损：使用 `Metadata["trail_pct"]` 和 `Metadata["reference_price"]`。
- 分批退出：使用 `SizePct`，例如 `0.5` 表示退出当前仓位的 50%。

runner 每次处理前会刷新：

```text
Position.HighestPrice
Position.LowestPrice
trailing_stop reference_price
```

触发退出后：

- 全平：删除当前 `bt` / `paper` 仓位 key。
- 分批退出：扣减仓位数量。
- 分批退出会移除已触发的那条 exit rule，避免同一规则反复触发。
- 退出原因写入事件：
  - `reason`
  - `metadata.exit_reason`
  - `metadata.trigger_price`
  - `metadata.size_pct`
  - `metadata.rule_reason`

## 仓位 Scope 和 Redis Key

仓位分为四类 scope：

| Scope | 含义 | 当前是否实现 | 是否接交易所 API | 是否需要账户 | 是否按策略隔离 |
| --- | --- | --- | --- | --- | --- |
| `bt` | 离线回测临时仓位 | 是 | 否 | 否 | 是 |
| `paper` | 在线本地模拟仓位 | 是 | 否 | 否 | 是 |
| `testnet` | 交易所测试网或 demo 仓位 | 预留 | 是 | 是 | 否，交易所账户级仓位 |
| `live` | 交易所实盘仓位 | 预留 | 是 | 是 | 否，交易所账户级仓位 |

`bt` 和 `paper` 都是本地策略仓位，不考虑账户管理。每个 `exchange + market + symbol + strategy` 独立维护自己的仓位。

例如 Binance USD-M 永续：

```text
ETHUSDT 10 个策略 = 10 个独立 paper 仓位
SOLUSDT 10 个策略 = 10 个独立 paper 仓位
```

`testnet` 和 `live` 是未来交易所 API 对接后的账户级仓位。交易所返回的是账户级净仓位或分仓模式仓位，不天然属于某个策略。策略归因需要单独做内部账本，不应混入交易所真实仓位主协议。

Redis key：

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
net
long
short
```

离线脚本写入 Redis 临时数据时必须登记到 `st:bt:{run_id}:keys`，并为 key 设置 TTL。脚本正常结束时删除 registry 中登记的所有 key；脚本异常退出时由 TTL 兜底。

## 交易所账户和交易能力预留

未来接入 `testnet` / `live` 时，订单服务需要读取：

- 账户余额
- 可用余额
- 保证金占用
- 未实现盈亏
- 当前持仓
- 挂单
- symbol 风控限制
- 数量精度
- 价格精度
- 合约张数单位
- 最小名义价值

预留 Redis key：

```text
st:acct:testnet:{account}:{exchange}:{market}
st:acct:live:{account}:{exchange}:{market}

st:cap:{exchange}:{market}:{symbol}
```

当前 `execution.SymbolCapability` 已预留：

```text
exchange
market
symbol
min_qty
qty_step
min_notional
max_leverage
max_order_qty
contract_size
updated_at
```

后续可能需要补充：

```text
quantity_mode
price_step
```

原因是不同交易所实盘下单数量单位不一致：

- 基础资产数量：`0.5 ETH`
- 名义价值：`100 USDT`
- 保证金：`20 USDT margin`
- 张数：`10 contracts`
- 不同合约面值：`1 contract = 0.01 ETH` 或 `0.001 ETH`

实盘最终必须以交易所下单接口和交易所真实回报为准。

## 持久化分层

```text
Redis      = 当前活跃状态，服务运行时快速读写
ClickHouse = 事件历史和分析数据，append-only
```

Redis 保存当前状态：

```text
st:pos:bt:{run_id}:{exchange}:{market}:{symbol}:{strategy}
st:pos:paper:{exchange}:{market}:{symbol}:{strategy}
st:pos:testnet:{account}:{exchange}:{market}:{symbol}:{position_side}
st:pos:live:{account}:{exchange}:{market}:{symbol}:{position_side}
```

ClickHouse 保存历史事件和分析结果。`pkg/position.ClickHouseStore` 当前会初始化并写入：

```text
strategy_events
backtest_run_summary
```

`strategy_events` 同时承载 `bt`、`paper`、`testnet` 和 `live` 的信号、仓位、订单、账户快照和交易所仓位同步事件。

常用可视化筛选和聚合字段是独立列：

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

事件类型：

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

当前 runner 已写入：

```text
signal_generated
order_intent_created
order_filled
```

`backtest_run_summary` 保存回测 run 级摘要：

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

可视化管理应读取 ClickHouse，不应直接读取 Redis。

## 幂等要求

在线服务必须生成稳定幂等键。当前 `execution.IntentIdempotencyKey` 包含：

```text
intent
scope
run_id
account
exchange
market
symbol
strategy_name
decision_open_time
action
side
position_side
```

同一个幂等键重复出现时，在线引擎不能重复写入新的订单意图。

## 当前限制

- 还没有在线服务入口。
- 还没有 Redis snapshot reader。
- 还没有回测 CLI。
- 还没有真实交易所订单服务。
- 还没有交易所精度、张数、最小下单量换算。
- runner 当前只对 `bt` / `paper` 更新本地仓位。
- `testnet` / `live` 当前只记录事件，不修改交易所账户级仓位。
- ClickHouse 表通过 `CREATE TABLE IF NOT EXISTS` 初始化，后续字段变更需要单独迁移策略。
- 当前 PnL 估算适用于模拟和回测；实盘以交易所成交和手续费为准。

## 后续顺序

建议按以下顺序推进：

1. 实现 Redis snapshot reader。
2. 实现 `strategy-engine` 服务入口和配置。
3. 实现回测 CLI，复用公共策略和仓位逻辑。
4. 增加交易所 symbol capability 缓存和数量换算。
5. 拆出 order executor 服务。
6. 接入 testnet。
7. 接入 live。
