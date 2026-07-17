# SuperTrend 策略

## 目标

SuperTrend 策略是当前第一版主策略原型。它不是单纯看 Supertrend 翻转，而是用 3 分钟触发信号入场，再通过多周期和窗口语义特征过滤假信号。

当前版本仍是负收益研究策略，不能进入实盘。优化过程、实验口径和最新年度回测见 [Supertrend 策略优化记录](supertrend-optimization.md)。

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
- 5m 必须确认短周期回调已经结束并重新与入场方向一致。
- 普通趋势延续要求 10m、15m 同向，10m Supertrend 至少稳定两根，且 30m 不能反向阻断。
- 独立 shock 突破要求 10m、15m、30m 全部同向，不能用大周期中性或缺失状态放行。

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

多头首先需要普通右侧触发或稳定两根的 pump 持续信号。普通触发包括上一根下影刺破 Supertrend 后收回、Supertrend 刚翻多、趋势事件、均线金叉或多头 SMC BOS。

普通触发进入 `trend_continuation` 路径，必须满足：

- 5m 已重新与多头方向一致。
- 10m、15m 同向，10m Supertrend 已稳定至少两根。
- 30m 不能反向阻断。
- 3m 趋势、均线、MACD、价量和动能条件通过。
- `pump_window_fake_risk` 不能为 `high`。

仅由第二根 pump 持续信号触发时，必须经过严格 `shock_breakout` 授权，不能借用普通趋势延续路径。

### 空头入场

空头与多头对称：普通触发来自上一根上影刺破后收回、Supertrend 刚翻空、趋势事件、均线死叉或空头 SMC BOS；普通路径需要 5m 回调结束、10m/15m 空头确认、30m 不阻断，以及 3m 趋势、均线、MACD、价量和动能确认。

仅由第二根 dump 持续信号触发时，同样必须经过严格 `shock_breakout` 授权。

### 严格 shock 突破

多空方向都必须同时满足：

- 已确认的突破结构或同向近期 SMC BOS。
- pump/dump 持续信号稳定计数至少为 `2`。
- 价量方向一致且成交量扩张。
- 3m、5m、10m、15m、30m 全部同向。
- 假信号风险精确为 `low`。
- 均线扩张、MACD 加速、价量方向、放量四类动能证据至少满足三类。

shock 入口会记录结构、各周期状态、假信号风险和动能确认数等诊断字段。未授权的 shock 信号不能回退成 `trend_continuation`。

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

- 普通 `trend_continuation` 必须等 5m 与目标方向重新对齐。
- 10m 和 15m 必须同向，且 10m Supertrend 稳定计数大于 `1`。
- 30m 反向时普通趋势延续被阻断。
- `shock_breakout` 进一步要求 10m、15m、30m 全部为 `aligned`。
- 5m 和 10m 同时反向仍会直接阻断；总阻断周期数也不能超过 `max_blocking_timeframes`。
- 对齐周期会加分，但不能替代入口模式授权。

## 出场逻辑

当前仓位模型是一锤子买卖：一次全仓进场，一次全仓出场，不做分批止盈或减仓。出场分为结构失效、利润保护、反向确认和价格规则。

### 结构失效与利润保护

- 10m 和 15m 同时确认持仓反方向时，全仓退出。
- `trailing` 模式下，持仓最大有利波动达到 `profit_decay_activation_bps` 后才启用指标衰减判断。
- 如果 5m 不再与持仓方向一致，且趋势推进、均线、MACD、价量四类本地衰减证据至少满足两类，全仓退出。
- 如果 5m/10m/15m 全部同向、动能证据至少三项、价格继续推进且没有反转风险，则允许利润继续运行。
- 其余状态暂时持有，等待趋势继续或衰减确认。

### 策略反向出场

多仓出场需要出现空头侧确认：

- 空头侧存在普通右侧触发或已授权 shock 触发。
- 并且满足以下任一确认：
  - 趋势、均线、MACD 都支持空头。
  - 5m 和 10m 同时支持空头阻断。
  - `supertrend_direction` 反向稳定超过 1 根。

空仓出场与多仓对称。

如果 3 分钟已经反向，但趋势、均线、MACD 和多周期确认不够，策略会暂缓出场，返回 `HOLD`。

### 价格规则出场

策略支持两种全仓价格退出模式：

`structure`：

多头：

- 止盈：`resistance_1`
- 止损：优先 `supertrend`，否则 `support_1`，并可由 `max_stop_loss_bps` 限制最大距离。

空头：

- 止盈：`support_1`
- 止损：优先 `supertrend`，否则 `resistance_1`，并可由 `max_stop_loss_bps` 限制最大距离。

`trailing`：

- 使用 `max_stop_loss_bps` 生成固定硬风险止损。
- 使用 `trailing_stop_pct` 跟随持仓最大有利价格。
- 只有最大有利波动达到 `profit_guard_activation_bps` 后跟踪退出才生效。
- 生效后退出价不会低于 `profit_guard_floor_bps` 对应的保盈底线。

回测会按每根 K 线的 OHLC 判断盘中是否触及退出价。同一根 K 线同时触发多个规则时，采用止损、跟踪止损、止盈的保守顺序，并对止损或已生效的跟踪退出处理跳空成交。

## 假信号处理

策略当前用以下规则降低 3 分钟来回翻转和无力假信号：

- 普通入口在 `pump_window_fake_risk` 或 `dump_window_fake_risk` 为 `high` 时阻断；独立 shock 入口只接受 `low`。
- 均线带缠绕或横盘时阻断。
- MACD 明确反向时阻断。
- 价量背离时阻断。
- 普通趋势延续必须等待 5m 回调解决并获得 10m/15m 趋势授权。
- shock 突破必须获得完整结构、价量、放量、动能和全周期对齐确认。
- 数据 freshness 不通过时阻断。

这些规则仍需要结合真实行情回放校准，尤其是趋势刚反转但大周期尚未跟上的场景。

## 当前限制

- 入场阈值和各项加分权重还没有经过系统回放校准；当前 `0.72` 阈值低于核心硬条件通过后的 `0.74`，不能独立筛选候选。
- 策略分数是规则积分，不是胜率概率；固定保证金模式也不会按分数调整仓位。
- `trend_continuation` 仍把多种普通触发合并统计，尚不能确定哪一种触发来源造成主要亏损。
- 交易明细尚未直接持久化 MFE、MAE、到达最大浮盈时间和利润回吐，出场质量仍需离线复原。
- 当前跟踪、保盈和衰减阈值只经过有限参数比较，尚未形成样本外稳定性证据。
- 当前策略只维护单一方向仓位，不做分批建仓。
- 特征字段依赖 Go 聚合层命名，字段协议需要继续稳定。

## 2026-07 年度回测结论

使用 ETHUSDT `2025-07-11` 至 `2026-07-11`、3m 入场周期、5m/10m/15m/30m 确认周期回测当前严格 shock 版本：

- 共 249 笔，盈利 75 笔，胜率 `30.12%`。
- 净收益 `-4855.42`，Profit Factor `0.660`，最大回撤 `5668.83`，手续费 `2987.67`。
- `shock_breakout` 共 9 笔、盈利 3 笔、净收益 `-84.48`。
- `trend_continuation` 共 240 笔、盈利 72 笔、净收益 `-4770.94`。

严格 shock 版本消除了放宽 shock 入口造成的明显退化，但与放宽前基线几乎完全相同。当前主要矛盾已经转为普通趋势延续入口及其出场质量；完整对比见 [Supertrend 策略优化记录](supertrend-optimization.md)。

## 后续优化

- 先给 `trend_continuation` 增加明确的 `trigger_source` 诊断，不改变交易行为。
- 为交易持久化或报告补充 MFE、MAE、最大浮盈时间和利润回吐。
- 按触发来源、多空、大周期状态和退出原因拆分收益，分别定位错误入口与错误出场。
- 只有在分组统计后，才调整普通入口、保盈阈值、衰减退出或强趋势持有规则。
- 用重点单边行情、完整年度样本和样本外区间共同验证，避免只优化个别行情窗口。
