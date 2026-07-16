# SuperTrend 策略

## 目标

SuperTrend 策略是当前第一版主策略原型。它不是单纯看 Supertrend 翻转，而是用 3 分钟触发信号入场，再通过多周期和窗口语义特征过滤假信号。

旧 Python 原型实现位置：

```text
backend/python-service/alphaflow-core/src/alphaflow/strategy/strategies/supertrend.py
```

当前 Go 在线实现位置：

```text
backend/go-service/pkg/strategies/supertrend/supertrend.go
```

## 数据依赖

Go 在线策略引擎消费 Go 聚合后的窗口特征：启动时可从 Redis 特征 hash 恢复初始态，启动后主要通过 NATS market snapshot 更新内存态。旧 Python 原型仍直接读取 Redis 特征 hash：

```text
{exchange_code}:{market}:indwin:{symbol}:{interval}
{exchange_code}:{market}:indrt:{symbol}:{interval}
```

其中：

- `indwin`：上一根已收盘 K 线对应的窗口分析结果。
- `indrt`：当前未收盘 K 线的实时指标表现和 K 线基础信息。

Go 在线路径会把 market snapshot / Redis 恢复数据构造成 `strategy.Snapshot`。旧 Python reader 会把这些 hash 解码成 `MarketSnapshot`：

- `snapshot.indicator`：当前实时指标和当前价格相关字段。
- `snapshot.indicator_window`：上一根已收盘 K 线的窗口聚合特征。
- `snapshot.klines`：当前实时 K 线。
- `snapshot.freshness`：特征时间版本校验结果。

策略会先检查：

- 目标周期是否是入场周期。
- `snapshot.health.is_ok()` 是否为真。
- `snapshot.indicator_window` 是否存在。
- `snapshot.freshness` 如果存在，必须有效。
- `data_quality` 必须为空或 `ok`。

## 周期

当前配置：

| 用途 | 周期 |
| --- | --- |
| 入场周期 | `3m` |
| 确认周期 | `5m`、`10m`、`15m`、`30m` |

设计意图：

- 3 分钟负责足够快地捕捉入场。
- 5m/10m 用于过滤明显反向的短周期阻断。
- 15m/30m 用于提供趋势背景，但当前不是绝对硬拦截。

## 入场逻辑

策略会分别计算多头和空头两个 `EntryDecision`，然后选择得分达标且未被阻断的一侧。

当前入场阈值：

```text
entry_threshold = 0.72
```

### 评分与硬阻断

入场分数是规则加分，不是校准后的胜率或概率。当前核心硬条件通过后的固定加分为：

| 条件 | 加分 |
| --- | ---: |
| 入场触发 | `0.30` |
| 趋势确认 | `0.16` |
| 均线确认 | `0.14` |
| MACD 确认 | `0.14` |

四项合计为 `0.74`，因此在当前 `entry_threshold = 0.72` 下，通过全部核心硬条件的候选天然已经超过阈值。更高分主要来自价量和确认周期加分，不能直接解释为更高胜率。策略会把分数写入 `Confidence`，但回测配置了 `margin_quote` 和 `leverage` 时，仓位大小仍按固定名义价值计算，不随该分数放大或缩小。

### 多头入场

多头入场需要满足以下方向：

1. 3 分钟存在 `pump_window_signal`，或 `supertrend_direction == up`。
2. `pump_window_fake_risk` 为空或 `low`。
3. 趋势窗口有效：
   - `trend_valid` 为空或 `true`。
   - `trend_window_bias`、`supertrend_direction` 或 `alphatrend_direction` 指向多头。
   - `trend_price_progress` 为空或 `advancing`。
   - `trend_quality` 不能是 `flat` 或 `weak`。
4. 均线带不能缠绕：
   - `ma_ribbon_state` 不能是 `tangled`、`flat`、`range`。
   - `ma_ribbon_phase` 不能是 `tangled`、`flat`、`range`。
   - `ma_ribbon_state` 或 `ema_alignment` 需要支持多头。
5. MACD 不能反向：
   - `macd_window_bias` 或 `macd_momentum` 需要支持多头。
   - 如果 MACD 明确偏空，则阻断。
   - 如果存在空头背离，则阻断。
6. 价量不能反向：
   - `price_volume_confirmation` 不能是 `divergence_bear`。
   - `confirm_up`、放量、突破量会加分。
7. 多周期不能明显反向：
   - 如果 5m 和 10m 同时阻断，多头直接被阻断。
   - 如果阻断周期数量超过配置，也会阻断。

### 空头入场

空头入场与多头对称：

1. 3 分钟存在 `dump_window_signal`，或 `supertrend_direction == down`。
2. `dump_window_fake_risk` 为空或 `low`。
3. 趋势窗口偏空，价格推进状态为空或 `advancing`；这里的 `advancing` 表示沿空头趋势下跌。
4. 均线带支持空头发散，不能缠绕横盘。
5. MACD 偏空，不能明确偏多。
6. 价量不能出现多头背离。
7. 5m 和 10m 同时反向会硬阻断。

## 多周期判断

每个确认周期会被分类为：

- `aligned`：支持当前方向。
- `blocking`：反向阻断。
- `neutral`：没有明确方向。
- `missing`：缺少窗口或健康状态异常。

分类依据包括：

- `trend_window_bias`
- `ma_window_bias`
- `macd_window_bias`
- `supertrend_direction`
- 反向 `pump_window_signal` 或 `dump_window_signal`

当前硬规则：

- 5m 和 10m 同时反向，直接阻断入场。
- 阻断周期数量超过 `max_blocking_timeframes`，阻断入场。
- 对齐周期会加分，但不是单独入场理由。

## 出场逻辑

当前仓位模型是一锤子买卖，不做复杂仓位管理。出场分两类：策略反向出场和价格规则出场。

### 策略反向出场

多仓出场需要出现空头侧确认：

- `dump_window_signal` 为真，或 `supertrend_direction == down`。
- 并且满足以下任一确认：
  - 趋势、均线、MACD 都支持空头。
  - 5m 和 10m 同时支持空头阻断。
  - `supertrend_direction` 反向稳定超过 1 根。

空仓出场与多仓对称。

如果 3 分钟已经反向，但趋势、均线、MACD 和多周期确认不够，策略会暂缓出场，返回 `HOLD`。

### 价格规则出场

策略生成信号时会附带退出规则：

多头：

- 止盈：`resistance_1`
- 止损：`supertrend`，如果没有则使用 `support_1`

空头：

- 止盈：`support_1`
- 止损：`supertrend`，如果没有则使用 `resistance_1`

仓位管理器会优先检查已有退出规则，例如止盈、止损、移动止损和分批退出。

## 假信号处理

策略当前用以下规则降低 3 分钟来回翻转和无力假信号：

- `pump_window_fake_risk` 或 `dump_window_fake_risk` 高风险时阻断。
- 均线带缠绕或横盘时阻断。
- MACD 明确反向时阻断。
- 价量背离时阻断。
- 5m 和 10m 同时反向时阻断。
- 数据 freshness 不通过时阻断。

这些规则仍需要结合真实行情回放校准，尤其是趋势刚反转但大周期尚未跟上的场景。

## 当前限制

- 入场阈值和各项加分权重还没有经过系统回放校准；当前 `0.72` 阈值低于核心硬条件通过后的 `0.74`，不能独立筛选候选。
- 策略分数是规则积分，不是胜率概率；固定保证金模式也不会按分数调整仓位。
- 多周期规则目前偏简单，后续可能需要区分“趋势刚反转”“趋势延续”“高位衰竭”。
- 出场逻辑仍是第一版，固定结构位没有手续费感知的最小止盈和最低盈亏比约束，也尚未引入更细的移动止损和结构位保护。
- 当前策略只维护单一方向仓位，不做分批建仓。
- 特征字段依赖 Go 聚合层命名，字段协议需要继续稳定。

## 2026-07 年度回测结论

使用 ETHUSDT `2025-07-11` 至 `2026-07-11`、3m 入场周期、5m/10m/15m/30m 确认周期完成了两次同参数回测：

- 卖侧语义修复前共 357 笔，全部为多头，胜率 `52.38%`，净收益 `-3815.90`，手续费 `4284.49`。决策诊断显示卖侧趋势和均线通过率均为 `0%`。
- 修复均线、趋势距离和 dump 方向语义后共 685 笔，其中多头 327 笔、空头 358 笔；卖侧趋势通过率恢复为 `3.71%`，均线通过率恢复为 `12.69%`，说明空头链路已经解锁。
- 修复后胜率为 `52.12%`，净收益 `-9946.99`，手续费 `8219.94`，最终权益 `53.01`。账户未触发模拟爆仓，但因可用余额不足停止继续开仓。
- 空头止盈 251 笔，净收益 `+6775.63`；空头止损 107 笔，净亏损 `-12977.62`；空头合计净亏损 `-6201.99`。

这次结果验证的是多空语义正确性，不代表策略有效。当前主要矛盾已经从“卖侧永久阻断”转为“止损损失、止盈收益和交易成本不对称”；后续不应通过重新制造卖侧阻断来改善报表。

## 后续优化

- 先确定手续费感知的最小止盈和最低盈亏比规则，再修改出场实现并分别统计多空贡献。
- 在出场规则稳定后，用历史样本重新校准 `entry_threshold` 和各项权重。
- 增加“拉盘前兆”和“放量突破”专项验证。
- 对 5m/10m 反向阻断加入反转早期容忍机制。
- 增加趋势衰竭、放量滞涨、缩量回落等出场特征。
- 为策略参数提供配置化入口。
