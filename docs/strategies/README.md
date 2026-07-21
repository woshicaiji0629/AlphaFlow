# 策略文档

本目录记录每个策略的设计目的、数据依赖、入场条件、出场条件和待优化项。

策略文档只描述当前实现或已经明确的设计，不把实验想法写成既定规则。策略参数、权重和阈值如果还没有经过回放验证，应明确标注为待校准。

## 当前策略

| 策略 | 文件 | 说明 |
| --- | --- | --- |
| SuperTrend | [supertrend.md](supertrend.md) | 3 分钟入场，多周期确认，消费 Go 聚合后的窗口特征。 |

## 设计文档

| 文档 | 文件 | 说明 |
| --- | --- | --- |
| Go 策略引擎 | [go-strategy-engine.md](go-strategy-engine.md) | Go-only 在线策略引擎、回测批处理和公共策略包边界设计。 |
| 市场能力层 | [../market-capability-architecture.md](../market-capability-architecture.md) | 多策略共享的趋势、波动、结构、位置、事件和质量能力边界。 |
| Supertrend 优化记录 | [supertrend-optimization.md](supertrend-optimization.md) | Supertrend 优化假设、实现变化、重点行情与年度回测结论。 |
| 市场结构研究 | [market-structure-research-2025-08.md](market-structure-research-2025-08.md) | 市场结构 Episode、路径特征和状态机研究。 |
| 市场波段与准入实验归档 | [market-swing-admission-research-2025-08-10.md](market-swing-admission-research-2025-08-10.md) | 独立波段口径、V4/V5/V6 命中漏斗、失败方法与禁止重试边界。 |

## 回测与研究命令

- `backtest-engine run`：正式策略回测。
- `backtest-engine dataset-check`：回测数据完整性检查。
- `market-research swing`：与策略无关的独立市场波段。
- `market-research analysis`：V4/V5/V6 市场分析观察。
- `market-research forward-label`：未来路径标签分布。
- `market-research structure-regime`：市场结构和状态研究。
- `market-research supertrend-signal`：Supertrend 信号与版本实验。

研究命令只能生成市场事实、标签、诊断和实验报告；不得把研究命中率表述为正式策略收益，也不得绕过正式回测和 paper 验证进入线上策略。

## 通用约定

- 策略引擎只负责编排、调用策略和应用仓位计划。
- 具体触发、能力组合、阈值、出场、评分和仓位规则由策略自己决定。
- 趋势、波动、结构、位置、事件、质量和多周期一致性属于公共市场能力，应开发一次供多个策略消费。
- 策略不得复制公共能力计算；需要更细粒度信息时读取公共连续值和元数据。
- 公共能力不得输出 `entry_allowed`、`should_buy` 或策略仓位结论；策略也不得把交易盈亏反向写入实时能力计算。
- 新能力先通过固定时间观察样本做独立验证，再进入策略组合实验。
- 能力公式变化使用独立版本；策略升级能力版本前必须重新回放验证。
- 当前策略仓位模型是一锤子买卖：每个交易所、市场、交易对和策略组合最多一个活跃仓位。
- Go 在线策略引擎启动时可从 Redis `indwin` 和 `indrt` 恢复，启动后主要消费 NATS market snapshot 并维护内存态。
- 如果特征 freshness 校验失败，策略不应继续使用该快照。
- ClickHouse 只保留 K 线历史；研究、回放和重新计算窗口特征时按需从 K 线计算指标。
