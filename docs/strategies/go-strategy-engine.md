# Go 策略引擎设计

本文档记录当前 Go-only 策略执行、仓位、执行和事件持久化的实际边界，以及后续规划。

## 当前状态

已经实现：

- 公共策略模型和策略引擎：`pkg/strategy`
- 在线和回测共享的上下文组装与校验：`pkg/strategyframe`
- 稳定策略名称、启停和参数配置：`pkg/strategyspec`
- 策略注册和构造入口：`pkg/strategyregistry`
- 独立仓位管理、Redis 当前态、ClickHouse 事件态：`pkg/position`
- 订单意图、执行报告和 paper broker：`pkg/execution`
- Supertrend 策略 Go 实现：`pkg/strategies/supertrend`
- 在线服务入口：`strategy-engine/cmd/strategy-engine`
- 在线配置加载：`strategy-engine/internal/config`
- Redis snapshot reader：`strategy-engine/internal/reader`
- NATS market snapshot 协议：`pkg/marketbus`
- 在线内存市场态：`strategy-engine/internal/marketstate`
- 在线策略引擎 app 编排：`strategy-engine/internal/app`
- 在线策略引擎内部 runner：`strategy-engine/internal/runner`
- 独立回测引擎入口、历史读取、滚动 snapshot、模拟成交和结果持久化：`backtest-engine`
- 回测数据完整性检查命令：`backtest-engine/cmd/backtest-dataset-check`
- 独立仓位/执行路由服务：`position-engine`
- 策略决策 NATS JetStream 协议：`pkg/strategybus`
- 策略结果路由公共包：`pkg/strategyroute`
- paper 仓位处理器公共包：`pkg/positionhandler/paper`
- Redis 幂等状态存储：`pkg/idempotency`
- `bt` / `paper` 本地策略仓位隔离
- 止盈、止损、移动止损、分批退出
- 回测和模拟仓 sizing：`margin_quote * leverage`
- 模拟手续费和返佣估算：`fee_rate` + `rebate_pct`
- ClickHouse 事件表：`strategy_events`
- ClickHouse 回测交易表：`backtest_trades`
- ClickHouse 回测摘要表：`backtest_run_summary`
- paper 当前持仓 scanner：按最新价格滚动评估止盈、止损、移动止损和分批退出
- 回测基础报告：trade 级权益曲线、逐 K 浮动权益曲线、组合权益曲线、账户资金曲线、最大回撤、胜率、profit factor 和连续亏损统计
- 多策略错误隔离：单个策略失败不阻断同批其他策略，失败信息随 decision 单独输出

尚未实现：

- 真实交易所 order executor
- 图表化回测报告、参数化批量回测和结果查询入口
- `testnet` / `live` / `notify` route handler
- 交易所账户级风控和 symbol 下单能力换算
- 订单服务级幂等落库和重复订单意图拦截
- HTTP 健康检查接口

## 目标边界

策略体系统一使用 Go 实现：

- 在线策略引擎是独立常驻服务。
- 回测是批处理脚本或 CLI。
- 仓位管理、模拟成交、实盘执行和通知路由由独立 position-engine 承接。
- 策略实现放在 Go 公共包，供在线引擎和回测共同使用。
- 策略加载通过 registry 完成；在线可以同时运行多个策略，离线回测一般一次只回测一个策略。
- Python 策略框架只作为旧原型参考，不作为新架构依赖。
- 策略引擎只产出策略结果，并通过策略决策总线交给下游路由服务。
- 服务间行情快照和策略决策使用 NATS JetStream；Redis 只保留缓存、特征恢复数据和当前态，不作为队列。
- 真实订单状态机后续拆到订单服务。

核心要求是在线和回测共享同一套策略逻辑，入口、配置、数据来源、信号路由、成交模型和持久化范围保持独立。

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

### `pkg/strategyregistry`

职责：

- 维护策略名到策略构造函数的映射
- 为在线引擎按配置构造策略集合
- 为回测按名称构造单个策略
- 避免在线入口和回测入口分别硬编码策略包

关键文件：

```text
backend/go-service/pkg/strategyregistry/registry.go
```

当前已注册：

```text
supertrend
```

策略配置使用 `pkg/strategyspec.Spec` 表达稳定名称、启停状态和参数。新增策略只需要实现公共接口、注册 factory，并由在线或回测配置选择；不需要分别修改两套执行引擎。

### `pkg/strategyframe`

职责：

- 把指标、窗口和多周期数据统一组装为 `strategy.Context`。
- 统一在线恢复态、在线增量态和历史回放的字段转换。
- 校验 target identity、周期完整性和 `AsOf` 时间边界。
- 保证策略不感知数据来自在线行情还是历史数据集。

关键文件：

```text
backend/go-service/pkg/strategyframe/context.go
backend/go-service/pkg/strategyframe/view.go
```

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
- 在行情输入降级时拒绝新开仓，保留平仓、减仓和止损等退出路径
- 将 `strategy.Decision` 交给 decision publisher

`runner` 不负责：

- 服务配置加载
- paper / backtest / live / notify 的具体处理逻辑
- 仓位计划、订单意图和成交处理
- 真实交易所账户仓位修改
- 交易所订单状态机
- 交易所精度和张数换算

### `backtest-engine/internal/reader`

职责：

- 根据回测配置读取历史 K 线
- 支持多 symbol 和入场/确认多周期数据集
- 对缺失的必要周期返回显式错误
- 为回测 simulator 提供按 `exchange + market + symbol + interval` 索引的历史序列

关键文件：

```text
backend/go-service/backtest-engine/internal/reader/reader.go
```

### `backtest-engine/internal/simulator`

职责：

- 从历史 K 线构造回测用 `strategy.Snapshot`
- 确认周期只使用当前入场 K 线 open time 之前已经闭合的数据，避免未来函数
- 复用公共策略、仓位、paper broker 和 route dispatcher 执行策略结果
- 使用独立 `bt` scope 和 run id 隔离回测仓位
- 从 `order_filled` 事件生成回测交易明细和 run 级摘要

关键文件：

```text
backend/go-service/backtest-engine/internal/simulator/snapshot.go
backend/go-service/backtest-engine/internal/simulator/executor.go
backend/go-service/backtest-engine/internal/simulator/trades.go
backend/go-service/backtest-engine/internal/simulator/summary.go
```

### `pkg/positionhandler/paper`

职责：

- 每次处理前刷新本地仓位最高价/最低价
- 调用 `position.Manager` 生成仓位计划
- 将名义价值 sizing 转成基础资产数量
- 构造订单意图
- 调用 paper broker 执行模拟成交
- 写入 `signal_generated`、`order_intent_created`、`order_filled` 事件
- 对 `paper` 本地成交结果更新当前仓位

关键文件：

```text
backend/go-service/pkg/positionhandler/paper/handler.go
```

### `pkg/strategyroute`

职责：

- 定义策略结果路由 `Route`
- 定义结果处理器接口 `ResultHandler`
- 根据策略名和 sink 分发 `strategy.Result`

关键文件：

```text
backend/go-service/pkg/strategyroute/route.go
```

sink 当前预留：

```text
paper
backtest
testnet
live
notify
log
```

### `pkg/strategybus`

职责：

- 定义跨服务传递的 `strategy.Decision` envelope。
- 将 `strategy.Decision` 编码为 NATS JetStream payload。
- 提供 `trace_id`、`signal_id`、result-level signal id 和 `created_at` / `expires_at` 过期边界。
- 提供 NATS JetStream 发布、durable consumer 创建、读取、dead-letter 和 Ack 能力。

关键文件：

```text
backend/go-service/pkg/strategybus/decision.go
backend/go-service/pkg/strategybus/nats.go
```

当前默认协议：

```text
stream: ALPHAFLOW_STRATEGY
subject: strategy.decision
durable: position-engine
default_ttl: 60s
ack_wait: 30s
dead_letter_subject: strategy.decision.dead
max_deliveries: 5
```

这是服务间通信协议，需要在配置和文档中保持明确。与此不同，`market-data` 的 ClickHouse pending 和 backfill 队列属于服务内自产自销队列，stream、subject、durable 和 dead-letter 名称由 `market-data` 代码内部约定，不作为对外配置。

当前 envelope JSON 字段包括：

```text
target
results
trace_id
signal_id
created_at
expires_at
```

`signal_id` 是 envelope 级幂等和追踪标识，基于 target 以及每个 result 的 `strategy_name`、`signal.strategy`、`signal.side`、`signal.open_time` 生成；result 顺序变化不会改变 envelope 级 `signal_id`。`position-engine` 处理时会进一步使用 `NewResultSignalID(target, result)` 生成 result-level 幂等 key。

### `pkg/marketbus`

职责：

- 定义 `market-data -> strategy-engine` 的 market snapshot envelope。
- 区分已收盘 K 线指标和当前未收盘 K 线实时指标。
- 提供 `created_at` / `expires_at` 实时性边界和 `trace_id`。
- 提供 NATS JetStream 发布、durable consumer 创建、读取、dead-letter 和 Ack 能力。

关键文件：

```text
backend/go-service/pkg/marketbus/snapshot.go
backend/go-service/pkg/marketbus/nats.go
```

当前默认协议：

```text
stream: ALPHAFLOW_MARKET
closed_subject: market.snapshot.closed
realtime_subject: market.snapshot.realtime
durable: strategy-engine-market
dead_letter_subject: market.snapshot.dead
max_message_age: 10s
realtime_stale_after: 15s
```

`market.snapshot.closed` 携带已收盘底层指标和窗口特征；`market.snapshot.realtime` 携带当前未收盘 K 线、实时指标和价格上下文。Redis `indwin` / `indrt` 仍作为启动恢复、故障恢复、观测和兼容缓存。

### `strategy-engine/internal/marketstate`

职责：

- 启动时接收 Redis reader 构造出的 `strategy.Context` 作为初始市场态。
- 启动后应用 NATS market snapshot 更新进程内市场态。
- 按 target 和确认周期构造策略运行所需的 `strategy.Context`。
- 校验消息实时性，拒绝过期、过旧和低版本 open time / updated at 消息覆盖内存态。
- 判断行情输入是否降级，并把降级原因交给 runner。

关键文件：

```text
backend/go-service/strategy-engine/internal/marketstate/state.go
```

### `pkg/idempotency`

职责：

- 提供幂等状态接口。
- 提供 Redis 实现。
- 使用 `processing` 和 `completed` 状态区分“正在处理”和“已经完成”。
- 通过 TTL 回收异常中断的 processing key 和已完成 key。

关键文件：

```text
backend/go-service/pkg/idempotency/store.go
```

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
backend/go-service/strategy-engine/cmd/strategy-engine/
backend/go-service/configs/strategy-engine.local.toml
backend/go-service/strategy-engine/internal/app/
backend/go-service/strategy-engine/internal/config/
backend/go-service/strategy-engine/internal/reader/
backend/go-service/strategy-engine/internal/runner/
```

当前在线路径：

```text
Redis indwin / indrt / health
  -> strategy-engine/internal/reader（启动恢复）
  -> strategy-engine/internal/marketstate

NATS market.snapshot.closed / market.snapshot.realtime
  -> strategy-engine/internal/marketstate（运行时更新）
  -> strategyframe.Context
  -> strategy.Engine（配置启用的策略集合）
  -> strategy-engine/internal/runner
  -> pkg/strategybus.NATSPublisher
  -> NATS JetStream strategy.decision
```

在线引擎职责：

- 加载策略、交易对、周期和风控配置。
- 启动时从 Redis 读取实时指标特征和已收盘窗口特征，作为恢复初始态。
- 启动后消费 NATS market snapshot 并维护内存市场态。
- 校验 market snapshot 实时性，旧消息不覆盖新状态。
- 构造公共 `strategy.Snapshot`。
- 只在策略声明的入场周期闭合事件上触发计算。
- 调用公共 `strategy.Engine`。
- 当行情输入缺失或 stale 时降级，拒绝开新仓但保留退出路径。
- 发布 `strategy.Decision`。
- 输出运行日志。

尚未完成的在线职责：

- 暴露 HTTP 健康检查接口。
- 接入更多策略配置。

### 回测批处理

当前入口：

```text
backend/go-service/backtest-engine/cmd/backtest-engine/main.go
```

当前回测路径：

```text
ClickHouse 历史 K 线
  -> backtest-engine/internal/reader.Dataset
  -> backtest-engine/internal/simulator.PreparedSeries
  -> strategyframe.Context
  -> pkg/strategy.Engine
  -> backtest-engine/internal/simulator.Executor
  -> pkg/positionhandler/paper + execution.PaperBroker
  -> strategy_events / backtest_trades / backtest_run_summary
```

当前 `backtest-engine` 已实现入口、配置加载、历史数据读取、滚动 snapshot、策略执行、模拟成交和结果落库：

```text
backend/go-service/backtest-engine/cmd/backtest-engine/
backend/go-service/configs/backtest-engine.local.toml
backend/go-service/backtest-engine/internal/app/
backend/go-service/backtest-engine/internal/config/
backend/go-service/backtest-engine/internal/reader/
backend/go-service/backtest-engine/internal/simulator/
```

当前回测职责：

- 从 ClickHouse 或文件读取历史 K 线。
- 按入场周期时间推进，并读取当时已经闭合的确认周期数据。
- 每个 symbol/interval 使用 `CalculateWindows` 批量计算一次指标，并缓存窗口结果。
- 按 `AsOf` 二分定位当前可见数据，避免逐 bar 重算历史前缀。
- 构造与在线服务一致的 `strategy.Snapshot`。
- 调用同一套策略、仓位管理、paper broker 和 route dispatcher。
- 用 `bt` scope 和 run id 隔离回测仓位。
- 输出并持久化策略事件、回测交易明细和 run 级统计摘要。

尚未完成的回测职责：

- 图表化报告输出。
- 参数化批量回测。
- 回测结果查询 API。
- 更完整的诊断报表，例如逐 bar 信号、错过交易原因和特征快照抽样。

回测不应复用在线服务的 Redis reader，也不应写在线 paper 仓位状态。

运行正式回测前可使用只读数据检查命令验证重复 K 线、缺口、连续区间和可用 warmup：

```sh
go run ./backtest-engine/cmd/backtest-dataset-check -config configs/backtest-engine.local.toml
```

### 仓位/执行路由服务

当前入口：

```text
backend/go-service/position-engine/cmd/position-engine/main.go
```

当前 `position-engine` 实现入口、配置加载、NATS JetStream 输入、长驻消费、dead-letter、result-level 幂等、paper route 处理和 route 校验：

```text
backend/go-service/position-engine/cmd/position-engine/
backend/go-service/configs/position-engine.local.toml
backend/go-service/position-engine/internal/app/
backend/go-service/position-engine/internal/config/
```

目标路径：

```text
strategy.Decision / strategy.Result
  -> pkg/idempotency.Store
  -> pkg/strategyroute.Dispatcher
  -> paper handler / backtest handler / live handler / notify handler
  -> position.Store / execution.Broker / notification sink
```

仓位/执行服务职责：

- 按策略名和 sink 路由策略结果。
- 维护 paper/backtest/testnet/live 各自独立的仓位状态。
- 生成仓位计划、订单意图、成交事件或通知消息。
- 允许不同策略使用不同 sink，互不干扰。
- 对 NATS JetStream 消息和 result-level signal 做幂等控制。
- 对 paper 当前持仓做滚动扫描，用最新价格触发退出规则。

在线和回测入口只负责产生 `strategy.Decision`，不直接决定信号最终进入 paper、实盘、回测还是通知。当前跨服务输入协议先落在 NATS JetStream 上，`position-engine` 通过 durable consumer 长驻读取 decision，并从 Redis `lp/mp` 价格 key 补最新价格上下文。处理成功后 Ack；空批次带 backoff；过期开仓类信号跳过并 Ack，过期退出类信号会用当前持仓 exit rules 和最新价格做保守重裁决；处理失败的消息由 JetStream 按 ack wait 重投递，超过投递上限后进入 dead-letter subject。幂等优先使用 result-level signal id，兼容旧消息的 envelope signal id 或 message id。backtest/live/notify handler 后续补齐。

当前 paper 持仓 scanner 使用 `position.Store.ListPositions` 读取 paper scope 当前仓位，按最新价格刷新最高价/最低价，并复用 `position.Manager.PlanWithPrice` 判断退出规则。触发后仍走既有 dispatcher 和 paper handler，因此扫描退出和策略信号退出共享同一套事件、成交和仓位更新路径。

## Snapshot

`strategy.Snapshot` 是在线和回测共享的策略输入。

当前字段：

```go
type Snapshot struct {
	Target     Target
	Current    marketmodel.Kline
	AsOf       int64
	Trigger    Trigger
	Indicator  IndicatorView
	Realtime   *RealtimeView
	Window     IndicatorWindowView
	Timeframes map[string]TimeframeSnapshot
	Price      PriceView
	Health     HealthView
	UpdatedAt  int64
}
```

设计约束：

- `AsOf` 表示本次决策能够看到数据的时间上界。
- `Trigger` 表示触发决策的行情事件，当前在线和回测都以入场周期闭合为主。
- `Current`、`Indicator` 和 `Window` 表示已经闭合的入场周期数据。
- `Realtime` 是可选的未闭合行情视图，不会替代闭合指标语义。
- `Window` 表示已收盘 K 线对应的窗口语义。
- `Timeframes` 只包含当时已经可用的确认周期窗口。
- 回测构造多周期窗口时必须避免未来函数。

策略层只能看到 `strategy.Snapshot` 和当前仓位快照，不知道数据来自在线 Redis 还是历史回放。

## 策略接口

```go
type Strategy interface {
	Name() string
	Requirements(target Target) Requirements
	Evaluate(ctx context.Context, snapshot Snapshot, position *Position) (Result, error)
}
```

`Requirements` 声明入场周期、确认周期和触发模式。执行引擎先验证上下文满足声明，再调用策略，因此同一份策略可以直接运行在在线和回测入口。

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
backtest_trades
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

`backtest_trades` 保存由回测 `order_filled` 事件配对生成的交易明细：

```text
run_id
exchange
market
symbol
strategy_name
position_side
entry_time
exit_time
entry_price
exit_price
size
gross_pnl
fee
net_pnl
return_pct
exit_reason
metadata
created_at
```

当前交易配对规则：

- 没有 `metadata.exit_reason` 的 `order_filled` 视为入场成交。
- 带 `metadata.exit_reason` 的 `order_filled` 视为退出成交。
- 按 `run_id + exchange + market + symbol + strategy + position_side` 做 FIFO 配对。
- 找不到入场成交或退出数量超过入场数量时返回错误，回测 run 不应继续写摘要。

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

策略决策总线和仓位路由服务必须生成稳定幂等键。

当前 `pkg/strategybus` 会为每个 `DecisionEnvelope` 生成：

```text
trace_id
signal_id
```

`signal_id` 基于 target 和 results 生成，用于整包级追踪和兼容。`position-engine` 处理时优先使用 result-level signal id：

```text
result:{rsig_xxx}
```

result-level id 基于：

```text
exchange
market
symbol
interval
scope
account
run_id
strategy_name
signal.strategy
signal.side
signal.open_time
```

同一个 result-level 幂等键重复出现时，`position-engine` 不能重复执行该 result。当前 `pkg/idempotency` 使用 Redis `processing` / `completed` 状态控制：

- `started`：当前实例获得处理权。
- `processing`：其他实例或前一次处理仍可能在执行，不 Ack，保持 pending。
- `completed`：已处理完成，直接 Ack 当前消息并跳过。

后续订单服务还需要自己的订单意图幂等键。当前 `execution.IntentIdempotencyKey` 包含：

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

同一个订单意图幂等键重复出现时，订单服务不能重复写入新的订单意图或重复向交易所下单。

## 当前限制

- 回测已经有历史读取、时间推进、模拟成交、交易明细、run 级摘要和基础权益曲线数据，但还没有图表化报告、参数化批量回测和结果查询 API。
- position-engine 已接入 NATS JetStream `strategy.Decision` 输入、长驻消费、空批次 backoff、dead-letter、result-level 幂等和 paper route，并能从 Redis `lp/mp` 价格 key 补最新价格上下文；过期退出类信号会用当前持仓 exit rules 和最新价格做保守重裁决。
- position-engine 已接入 paper 当前持仓 scanner，但还没有 backtest/live/notify handler 实现。
- 还没有真实交易所订单服务。
- 还没有交易所精度、张数、最小下单量换算。
- 还没有订单服务级幂等落库和重复订单意图拦截。
- 还没有 HTTP 健康检查接口。
- `testnet` / `live` handler 尚未接入真实交易所账户级仓位。
- ClickHouse 表通过 `CREATE TABLE IF NOT EXISTS` 初始化，后续字段变更需要单独迁移策略。
- 当前 PnL 估算适用于模拟和回测；实盘以交易所成交和手续费为准。

## 后续顺序

建议按以下顺序推进：

1. 补回测权益曲线、报告输出和结果查询入口。
2. 补回测参数化运行和策略配置加载。
3. 实现 position-engine 的 notify handler。
4. 增加交易所 symbol capability 缓存和数量换算。
5. 为过期策略反向退出但无 exit rule 的场景补明确 action 协议。
6. 拆出 order executor 服务。
7. 接入 testnet。
8. 接入 live。
