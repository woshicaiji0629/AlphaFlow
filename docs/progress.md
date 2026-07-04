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

- 回测还没有权益曲线、图表/文件报告和结果查询 API。
- 回测还没有参数化批量运行和策略参数配置入口。
- position-engine 还没有 `backtest` / `live` / `notify` handler。
- 真实交易所 order executor 尚未实现。
- 交易所 symbol 精度、张数、最小下单量和合约面值换算尚未实现。
- 订单服务级幂等落库和重复订单意图拦截尚未实现。
- 账户级实时风控尚未实现。
- HTTP 健康检查接口尚未实现。
- 管理 API 和前端尚未实现。
- ClickHouse 表当前通过 `CREATE TABLE IF NOT EXISTS` 初始化，后续字段变更需要单独迁移策略。

## 建议下一步

1. 补回测权益曲线、报告输出和结果查询入口。
2. 补回测参数化运行和策略配置加载。
3. 实现 position-engine 的 notify handler。
4. 增加交易所 symbol capability 缓存和数量换算。
5. 明确过期策略反向退出但无 exit rule 时的 action 协议。
6. 拆出真实 order executor 服务。
7. 接入 testnet。
8. 接入 live。

## 验证状态

最近一轮 Go 全量测试已通过：

```sh
GO111MODULE=on go test ./...
```

本文档更新只涉及文档，不改变运行时代码。
