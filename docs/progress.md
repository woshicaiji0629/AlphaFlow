# 项目进度

本文档记录 AlphaFlow 当前推进状态、关键架构决策、已知问题和建议下一步。它只记录已经实现或已经明确的方向，不把实验想法写成既定事实。

## 当前主线

当前主线是把以下链路打通：

```text
ClickHouse 历史 K 线 / Redis 实时特征
  -> strategy.Snapshot
  -> 可插拔策略
  -> 统一策略结果
  -> 仓位计划和模拟成交
  -> ClickHouse 事件、交易明细和摘要
```

在线引擎和回测引擎共享同一份 Go 策略代码。两者的入口、数据源、仓位 scope、成交模型和持久化范围保持独立。

## 已完成

### 行情和指标

- Go `market-data` 已负责交易所行情采集、派生 K 线、指标计算和窗口特征发布。
- Redis 保存实时行情、已收盘窗口特征和当前 K 线实时特征。
- ClickHouse 保存已闭合 K 线历史。
- 指标计算和窗口分析已下沉到公共包，供实时服务、回测和未来重算复用。

### 策略框架

- `pkg/strategy` 定义统一策略输入、输出、接口和基础 engine。
- `pkg/strategyregistry` 提供策略注册和构造入口。
- 当前已注册 `supertrend`。
- `strategy-engine` 支持按配置启用策略集合，在线可以同时运行多个策略。
- 回测通常一次只回测一个策略，避免批量结果混杂；后续参数化批量回测应在回测层显式编排。

### 回测引擎

- `backtest-engine` 已具备独立入口和配置。
- 已支持读取多 symbol、多 interval 历史 K 线数据集。
- 已支持按入场周期滚动构造 `strategy.Snapshot`。
- 确认周期只使用当时已经闭合的数据，避免未来函数。
- 已复用公共策略、仓位管理、paper broker 和 route dispatcher 执行回测。
- 回测仓位使用独立 `bt` scope 和 run id，不写在线 paper 仓位。
- 已生成并持久化策略事件、回测交易明细和 run 级摘要。
- 已支持基础回测报告计算和可选 JSON 文件输出，包括 trade 级权益曲线、逐 K 浮动权益曲线、组合权益曲线、模拟账户资金曲线、最大回撤、胜率、profit factor 和连续亏损统计。
- 回测模拟账户已纳入初始资金、保证金占用、手续费、返佣、可用余额检查和账户权益归零爆仓处理。
- 回测 run summary 已优先采用模拟账户最终净值、账户回撤、手续费、返佣和爆仓状态作为账户级报告口径。
- 多 symbol 回测已按 K 线时间线归并执行，同一时间按 symbol 排序保证结果可复现。
- 同一 K 线时间点的多 symbol 批次会先统一刷新价格和账户浮盈亏，再执行该批次信号和订单，并只生成一条账户快照。
- 已新增静态 symbol capability，回测/paper 下单前会按 base/contract 单位、contract size、数量步长和最小名义金额做数量归一化。
- 回测/paper broker 已支持固定 bps 滑点，买入按成交价上浮、卖出按成交价下浮，并可通过 backtest-engine 配置控制。

### 仓位和执行路由

- `position-engine` 已支持 Redis Stream 长驻消费、pending reclaim、dead-letter 和 result-level 幂等。
- paper route 已接入公共 paper handler，支持开仓、平仓、减仓、止盈、止损、移动止损和分批退出。
- paper 当前持仓 scanner 已接入，可按最新价格滚动检查退出规则。
- paper 和 backtest 使用本地策略仓位，不依赖交易所账户仓位。

### 持久化

- ClickHouse `strategy_events` 保存策略事件和模拟成交事件。
- ClickHouse `backtest_trades` 保存由回测成交事件配对生成的交易明细。
- ClickHouse `backtest_run_summary` 保存回测 run 级摘要。
- Redis 继续作为当前活跃状态和服务交接层，不作为长期分析存储。

## 关键决策

- K 线维护仍是批处理/任务形态，不需要做成长驻在线服务；回测需要的是可重复、可校验、可补数的历史数据。
- 策略代码放在 Go 公共包，在线引擎和回测引擎共用。
- 在线策略引擎可以同时跑多个策略。
- 离线回测一般一次只回测一个策略；批量回测应显式生成多个 run。
- 上线或下线策略优先通过策略 registry 和配置控制，不在多个服务里分别硬编码。
- 回测仓位应独立于在线 paper 仓位，使用 `bt` scope 和 run id 隔离。
- `paper` / `bt` 是本地策略仓位；`testnet` / `live` 后续应按交易所账户级仓位处理，并通过内部账本做策略归因。

## 已知问题

- 回测还没有图表报告和结果查询 API。
- 回测还没有参数化批量运行和策略参数配置入口。
- 回测爆仓当前按账户权益归零处理，还没有接入交易所维持保证金、标记价格和阶梯强平公式。
- 回测滑点当前是固定 bps 模型，还没有按盘口深度、成交量、波动率或订单大小动态估算。
- position-engine 还没有 `backtest` / `live` / `notify` handler。
- 真实交易所 order executor 尚未实现。
- 交易所 symbol capability 目前来自静态配置，尚未接交易所 API 自动同步。
- 订单服务级幂等落库和重复订单意图拦截尚未实现。
- 账户级实时风控尚未实现。
- HTTP 健康检查接口尚未实现。
- 管理 API 和前端尚未实现。
- ClickHouse 表当前通过 `CREATE TABLE IF NOT EXISTS` 初始化，后续字段变更需要单独迁移策略。

## 建议下一步

1. 补回测图表报告和结果查询入口。
2. 补回测参数化运行和策略配置加载。
3. 实现 position-engine 的 notify handler。
4. 增加交易所 symbol capability 自动同步和缓存。
5. 明确过期策略反向退出但无 exit rule 时的 action 协议。
6. 拆出真实 order executor 服务。
7. 接入 testnet。
8. 接入 live。

## 验证状态

最近一轮 Go 全量测试已通过：

```sh
GO111MODULE=on go test ./...
```

本轮回测账户、报告、symbol capability 和滑点更新包含 Go 代码和文档进度同步。
