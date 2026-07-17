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
| Supertrend 优化记录 | [supertrend-optimization.md](supertrend-optimization.md) | Supertrend 优化假设、实现变化、重点行情与年度回测结论。 |

## 通用约定

- 策略引擎只负责编排、调用策略和应用仓位计划。
- 具体入场、出场、过滤、评分和仓位规则由策略自己决定。
- 当前策略仓位模型是一锤子买卖：每个交易所、市场、交易对和策略组合最多一个活跃仓位。
- Go 在线策略引擎启动时可从 Redis `indwin` 和 `indrt` 恢复，启动后主要消费 NATS market snapshot 并维护内存态。
- 旧 Python 原型仍可读取 Redis `indwin` 和 `indrt` 特征 hash。
- 如果特征 freshness 校验失败，策略不应继续使用该快照。
- ClickHouse 只保留 K 线历史；研究、回放和重新计算窗口特征时按需从 K 线计算指标。
