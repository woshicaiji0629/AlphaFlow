# SuperTrend 策略

## 目标

SuperTrend 策略是当前第一版主策略原型。它不是单纯看 Supertrend 翻转，而是用 3 分钟触发信号入场，再通过多周期和窗口语义特征过滤假信号。当前提供默认 `trend` 和实验性 `intraday_adaptive` 两种入口画像，入口画像与出场模式可以独立配置。

已完成的年度回测只覆盖 `smc_bos-only` 过滤前的 `trend + trailing` 基线，结果为负收益。当前代码已经实现 `smc_bos-only` 入口 ablation、`intraday_adaptive` 入口和 `adaptive` 出场，但尚未完成同口径年度与样本外回测；所有画像和模式仍是研究实现，不能进入实盘。优化过程、实验口径和最新年度回测见 [Supertrend 策略优化记录](supertrend-optimization.md)。

当前 Go 在线实现位置：

```text
backend/go-service/pkg/strategies/supertrend/supertrend.go
```

## 数据依赖

Go 在线策略引擎消费 Go 聚合后的窗口特征：启动时可从 Redis 特征 hash 恢复初始态，启动后主要通过 NATS market snapshot 更新内存态：

```text
{exchange_code}:{market}:indwin:{symbol}:{interval}
{exchange_code}:{market}:indrt:{symbol}:{interval}
```

其中：

- `indwin`：上一根已收盘 K 线对应的窗口分析结果。
- `indrt`：当前未收盘 K 线的实时指标表现和 K 线基础信息。

Go 在线路径会把 market snapshot / Redis 恢复数据构造成 `strategy.Snapshot`：

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
- `trend` 画像要求 5m 确认短周期回调已经结束并重新与入场方向一致。
- `trend` 画像的普通趋势延续要求 10m、15m 同向，10m Supertrend 至少稳定两根，且 30m 不能反向阻断。
- `intraday_adaptive` 画像允许 5m 中性，但不允许 5m 缺失或反向；10m/15m/30m 的同向与阻断数量由参数约束。
- 独立 shock 突破要求 10m、15m、30m 全部同向，不能用大周期中性或缺失状态放行。

## 入场逻辑

策略会分别计算多头和空头两个 `EntryDecision`，然后选择得分达标且未被阻断的一侧。

当前入场阈值：

```text
entry_profile = trend
entry_threshold = 0.72
```

### 入口画像

`trend` 是默认画像，保持严格的大周期趋势延续路径。普通开仓至少需要一个非 `smc_bos` 触发来源；`smc_bos` 仍会参与诊断，也可以和其他来源共振，但不能再单独授权空仓后的普通入场。该限制只针对新开仓，持仓后的反向退出确认仍允许读取 SMC BOS。

`intraday_adaptive` 是实验画像，用于记录并验证较短的日内事件：

- 保留非 `smc_bos` 普通触发和严格 shock 路径。
- 当本地趋势延续环境成立、当前 3m 收盘变化落入 ATR 自适应脉冲区间且价量同向时，增加 `intraday_impulse` 触发来源。
- 脉冲下限为 `max(8 bps, ATR bps * 0.25)`，上限为 `min(20 bps, ATR bps * 0.75)`；超过上限的已扩张脉冲不会作为新鲜日内触发。
- 5m 可以中性，但不能反向或缺失；10m/15m/30m 至少需要 `intraday_min_aligned_timeframes` 个同向周期，反向周期数不能超过 `max_blocking_timeframes`。
- 动能不足会保留诊断和评分影响，但在该画像中不单独硬阻断；趋势、均线、MACD、价量、总分和假信号风险仍按现有规则检查。
- 无论最终开仓还是 `HOLD`，都会记录波动、通道、VWAP、支撑阻力、成交量分布、流动性扫单和耗竭风险等日内市场上下文。

`intraday_min_aligned_timeframes` 只允许在 `entry_profile = intraday_adaptive` 时配置，取值范围为 `1` 至 `4`，默认值为 `1`。严格 shock 路径优先于两个普通入口画像，仍需完整的全周期授权。

### 评分与硬阻断

入场分数是规则加分，不是校准后的胜率或概率。当前四个主要维度在完全同向时最多提供以下固定加分：

| 条件 | 加分 |
| --- | ---: |
| 入场触发 | `0.30` |
| 趋势确认 | `0.16` |
| 均线确认 | `0.14` |
| MACD 确认 | `0.14` |

四项完全同向时合计为 `0.74`，但趋势、均线和 MACD 检查在中性、缠绕或质量未确认时可以通过而不给分，因此不能把“通过检查”简单等同于固定获得 `0.74`。年度实际成交分数范围为 `0.72` 至 `1.16`，说明当前阈值确实会过滤低分候选；但分数与单笔净收益、MFE 的相关系数分别只有 `0.012` 和 `-0.004`，没有观察到交易质量随分数单调提高。策略会把分数写入 `Confidence`，但回测配置了 `margin_quote` 和 `leverage` 时，仓位大小仍按固定名义价值计算，不随该分数放大或缩小。

### 多头入场

多头首先需要普通右侧触发或稳定两根的 pump 持续信号。普通触发包括上一根下影刺破 Supertrend 后收回、Supertrend 刚翻多、趋势事件、均线金叉或多头 SMC BOS；其中 SMC BOS 只能作为共振来源，不能单独授权空仓后的普通入场。

普通触发进入 `trend_continuation` 路径，必须满足：

- 5m 已重新与多头方向一致。
- 10m、15m 同向，10m Supertrend 已稳定至少两根。
- 30m 不能反向阻断。
- 3m 趋势、均线、MACD、价量和动能条件通过。
- `pump_window_fake_risk` 不能为 `high`。

仅由第二根 pump 持续信号触发时，必须经过严格 `shock_breakout` 授权，不能借用普通趋势延续路径。

### 空头入场

空头与多头对称：普通触发来自上一根上影刺破后收回、Supertrend 刚翻空、趋势事件、均线死叉或空头 SMC BOS，且 SMC BOS 不能单独授权空仓后的普通入场；`trend` 路径需要 5m 回调结束、10m/15m 空头确认、30m 不阻断，以及 3m 趋势、均线、MACD、价量和动能确认。

仅由第二根 dump 持续信号触发时，同样必须经过严格 `shock_breakout` 授权。

### 严格 shock 突破

多空方向都必须同时满足：

- 已确认的突破结构或同向近期 SMC BOS。
- pump/dump 持续信号稳定计数至少为 `2`。
- 价量方向一致且成交量扩张。
- 3m、5m、10m、15m、30m 全部同向。
- 假信号风险精确为 `low`。
- 均线扩张、MACD 加速、价量方向、放量四类动能证据至少满足三类。

shock 入口会记录结构、各周期状态、假信号风险和动能确认数等诊断字段。未授权的 shock 信号不能回退成 `trend_continuation`。实际成交还会记录入口模式、所有同时成立的触发来源、本地状态、MFE、MAE、最大有利/不利时间、利润回吐和持仓时长，供回测分组分析。

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

- `trend` 画像的普通 `trend_continuation` 必须等 5m 与目标方向重新对齐。
- `trend` 画像要求 10m 和 15m 同向，且 10m Supertrend 稳定计数大于 `1`；30m 反向时阻断。
- `intraday_adaptive` 画像要求 5m 不反向、不缺失，并按配置检查 10m/15m/30m 的最少同向数和最大阻断数。
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

策略支持三种全仓价格退出模式：

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

`adaptive`：

- 不设置固定止盈；使用硬风险止损和全仓保护性跟踪退出，`max_stop_loss_bps` 未配置时默认限制为 `70 bps`。
- 把 `micro_profit_quote`、`target_profit_quote`、`runner_profit_quote` 三档报价货币利润换算为相对入场价的 bps；三者默认分别为 `10`、`20`、`30`，且必须严格递增。
- 保盈底线取 `round_trip_cost_bps + profit_buffer_bps` 与微利目标 `80%` 中的较大值；默认成本和缓冲分别为 `16 bps`、`8 bps`。
- 保盈激活至少高于底线 `2 bps`；动能衰减激活不低于普通目标；runner 激活不低于 runner 目标。
- 基础跟踪距离使用 ATR 百分比：低波动按 `0.75 * ATR` 并限制在 `0.18%` 至 `0.35%`，普通波动按 `1.0 * ATR` 并限制在 `0.25%` 至 `0.55%`，扩张波动按 `1.25 * ATR` 并限制在 `0.35%` 至 `0.75%`。
- 进入 runner 后跟踪距离放宽为基础距离的 `1.75` 倍，上限 `1.20%`；放宽时保留 runner 激活点对应的保护锚点，不会因为切换到宽跟踪而降低已经形成的保护价。
- 除通用的 10m/15m 反转和利润衰减退出外，还检查本地方向失效：5m 反向且至少一类本地证据反向时退出；未盈利时至少两类证据反向也退出；已盈利时需要至少三类反向证据。

`adaptive` 不能与固定 `trailing_stop_pct`、固定保盈参数或结构止盈几何参数混用。上述参数是代码默认值和风险边界，不是已经由回测证明有效的最优参数。

回测会按每根 K 线的 OHLC 判断盘中是否触及退出价。同一根 K 线同时触发多个规则时，采用止损、跟踪止损、止盈的保守顺序，并对止损或已生效的跟踪退出处理跳空成交。

## 假信号处理

策略当前用以下规则降低 3 分钟来回翻转和无力假信号：

- 普通入口在 `pump_window_fake_risk` 或 `dump_window_fake_risk` 为 `high` 时阻断；独立 shock 入口只接受 `low`。
- 均线带缠绕或横盘时不给均线加分；它本身不一定硬阻断，候选仍需依靠总分和其他条件决定。
- MACD 明确反向时阻断。
- 价量背离时阻断。
- 普通趋势延续必须等待 5m 回调解决并获得 10m/15m 趋势授权。
- shock 突破必须获得完整结构、价量、放量、动能和全周期对齐确认。
- 数据 freshness 不通过时阻断。

这些规则仍需要结合真实行情回放校准，尤其是趋势刚反转但大周期尚未跟上的场景。

## 当前限制

- 代码可达性审查确认 `trend`、`intraday_adaptive`、`structure`、`trailing`、`adaptive`、shock 和反转确认路径仍有生产调用或注册入口；`fakeRiskBlocked` 是当前唯一确认无调用的内部 helper，待随 Go 实现提交删除。
- 入场阈值和各项加分权重还没有经过系统回放校准；`0.72` 阈值会筛选候选，但现有分数没有观察到收益或 MFE 排序能力。
- 策略分数是规则积分，不是胜率概率；固定保证金模式也不会按分数调整仓位。
- `smc_bos-only` 过滤、`intraday_adaptive` 入口和 `adaptive` 出场只有单元测试覆盖，尚未完成同口径年度、重点行情窗口和样本外收益验证。
- 成交诊断只覆盖被接受的交易；硬条件筛选后本地 Supertrend、趋势、均线和 MACD 几乎完全同向，不能仅用成交样本估计各字段的独立边际价值。
- 普通路径实际成交高度集中于 `smc_bos`，其他触发来源样本很少，不能直接根据 8 笔 `trend_event` 历史交易认定该来源稳定盈利。
- MFE/MAE 基于回测 OHLC 和保守的盘中退出顺序，只用于诊断，不表示实盘能够按极值成交。
- 当前跟踪、保盈和衰减阈值只经过有限参数比较，尚未形成样本外稳定性证据。
- 当前策略只维护单一方向仓位，不做分批建仓。
- 特征字段依赖 Go 聚合层命名，字段协议需要继续稳定。

## 2026-07 历史基线回测结论

使用 ETHUSDT `2025-07-11` 至 `2026-07-11`、3m 入场周期、5m/10m/15m/30m 确认周期回测严格 shock 的 `trend + trailing` 历史基线。该 Run 发生在 `smc_bos-only` 过滤和两个自适应模式实现之前，不能代表当前实验代码的结果：

- 共 249 笔，盈利 75 笔，胜率 `30.12%`。
- 净收益 `-4855.42`，Profit Factor `0.660`，最大回撤 `5668.83`，手续费 `2987.67`。
- `shock_breakout` 共 9 笔、盈利 3 笔、净收益 `-84.48`。
- `trend_continuation` 共 240 笔、盈利 72 笔、净收益 `-4770.94`。

严格 shock 版本消除了放宽 shock 入口造成的明显退化，但与放宽前基线几乎完全相同。当前主要矛盾已经转为普通趋势延续入口及其出场质量；完整对比见 [Supertrend 策略优化记录](supertrend-optimization.md)。

2026-07-17 的同参数诊断回测保持完全相同的 249 笔交易和收益，并补齐全部交易级字段。普通路径 240 笔中，232 笔只有 `smc_bos` 触发，净亏 `-5250.68`；154 笔普通路径止损中有 101 笔从未达到 `50 bps` MFE，另有 18 笔达到 `100 bps` 但没有达到历史基线 `150 bps` 的保盈激活线。全年毛收益 `-1867.75`、手续费 `2987.67`，所以信号负期望和费用拖累同时存在。

## 后续优化

- 先回测已经实现的 `smc_bos-only` 入口 ablation，确认真实交易序列、重点行情和样本外表现；不能用历史交易的静态删减代替回放。
- 固定入口后分别比较 `trailing` 与 `adaptive`，重点观察曾达到 `100 bps` 后最终止损的交易；不得把入口画像和出场模式同时修改。
- `intraday_adaptive` 入口单独作为后续实验，先核对新增 `intraday_event` 的触发分布和市场上下文，再讨论收益。
- 暂不优先扫描 `entry_threshold`；只有评分产生可验证的独立信息和排序能力后再校准。
- 若要分析方向字段共线性，应增加被拒绝候选的诊断，而不只统计成交样本。
- 用重点单边行情、完整年度样本和样本外区间共同验证，避免只优化个别行情窗口。
