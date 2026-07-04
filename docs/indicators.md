# 指标文档

本文档描述 Go `market-data` 服务当前输出的底层指标快照字段、窗口聚合特征，以及 Python 策略侧的使用方式。

指标计算只使用已闭合 K 线。每个底层快照按交易所、市场、交易对、周期和 open time 写入 Redis 最新状态；指标不写入 ClickHouse 历史。

## 底层指标存储模型

底层指标快照不是一列一个指标，而是两个动态 map：

- `values`：数值型字段，统一以字符串存储，策略侧会按需解析为浮点数。
- `signals`：枚举或状态型字段，统一以字符串存储。

新增指标通常不需要改数据库字段。只要 Go 计算端写入新的 `values["key"]` 或 `signals["key"]`，Redis 最新快照会随之保存。窗口聚合层会枚举当前计算窗口里的指标 key：

- 数值字段会生成最新值、前值、变化、斜率、方向、连续上升/下降次数、区间位置。
- 信号字段会生成最新状态、前值、是否变化、稳定持续次数、距上次变化多久。

注意：新字段只会出现在部署后的新快照里，老历史不会补齐。

底层指标序列不作为长期事实数据保留。K 线才是事实数据；窗口聚合口径或策略特征变化时，从 ClickHouse K 线重新计算指标。Redis 中的指标和窗口特征只是实时缓存。

## 窗口聚合特征层

当前实时策略路径优先消费 Go `indicatorwindow` 输出的窗口聚合特征，而不是让 Python 策略自己拉取大量历史指标再计算窗口。

窗口聚合层分两份 Redis hash：

- `indwin`：上一根已收盘 K 线的窗口聚合结果。
- `indrt`：当前未收盘 K 线的实时指标表现和 K 线基础信息。

窗口聚合层解决的问题：

- 把几十到几百根历史指标压缩成一个交易对、一个周期的一份特征 hash。
- 把底层指标转换成策略可读语义，例如趋势是否有效、均线是否缠绕、MACD 是否跟随、成交量是否放大。
- 为多策略共享同一份特征，避免每个策略重复做窗口计算。
- 通过 `meta:bar_seq`、`meta:updated_at` 和 `meta:age_limit_ms` 判断数据是否最新。

窗口字段分两类：

- 通用窗口字段：由底层数值和信号自动生成，例如 `{key}_win_latest`、`{key}_win_slope`、`{key}_win_stable_count`。
- 适配语义字段：按指标特点额外聚合，例如 `ma_ribbon_state`、`macd_window_bias`、`pump_window_signal`。

策略应优先使用适配语义字段；只有当策略需要更细粒度判断时，再读取通用窗口字段。

## 策略常用语义特征

### 拉盘/砸盘窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `value:pump_window_score` | value | 拉盘特征分数。 |
| `value:dump_window_score` | value | 砸盘特征分数。 |
| `signal:pump_window_signal` | signal | 是否出现可用多头触发。 |
| `signal:dump_window_signal` | signal | 是否出现可用空头触发。 |
| `signal:pump_window_fake_risk` | signal | 多头假信号风险。 |
| `signal:dump_window_fake_risk` | signal | 空头假信号风险。 |
| `signal:pump_window_quality` | signal | 多头触发质量。 |
| `signal:dump_window_quality` | signal | 空头触发质量。 |

### 趋势窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signal:trend_valid` | signal | 当前趋势是否有效。 |
| `signal:trend_window_bias` | signal | 窗口趋势偏向。 |
| `signal:trend_price_progress` | signal | 价格推进状态，例如 `advancing`、`declining`。 |
| `signal:trend_quality` | signal | 趋势质量。 |
| `signal:supertrend_direction` | signal | Supertrend 方向。 |
| `signal:alphatrend_direction` | signal | AlphaTrend 方向。 |

### 均线窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signal:ma_window_bias` | signal | 均线窗口偏向。 |
| `signal:ma_ribbon_state` | signal | 均线带状态，例如多头发散、空头发散、缠绕。 |
| `signal:ma_ribbon_phase` | signal | 均线带阶段，例如早期发散、趋势延续、横盘。 |
| `signal:ema_alignment` | signal | EMA 排列方向。 |

### MACD 窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signal:macd_window_bias` | signal | MACD 窗口偏向。 |
| `signal:macd_window_quality` | signal | MACD 动能质量。 |
| `signal:macd_momentum` | signal | MACD 动能状态。 |
| `signal:macd_divergence` | signal | MACD 背离状态。 |

### 成交量和价量窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signal:volume_window_state` | signal | 成交量窗口状态，例如放量、突破量、常态。 |
| `signal:volume_state` | signal | 底层成交量状态。 |
| `signal:price_volume_confirmation` | signal | 价量确认或背离。 |

### 通道和 TradingView 窗口

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signal:channel_window_bias` | signal | Donchian/Keltner 通道窗口偏向：`bull`、`bear`、`neutral`。 |
| `signal:channel_breakout_quality` | signal | 通道突破质量：`strong`、`weak`、`neutral`。 |
| `signal:channel_volatility_state` | signal | 通道宽度状态：`expanding`、`contracting`、`flat`。 |
| `signal:channel_position_state` | signal | 价格在通道中的位置：`upper`、`middle`、`lower`。 |
| `signal:channel_fake_risk` | signal | 通道假突破风险：`low`、`medium`、`high`。 |
| `signal:qqe_window_bias` | signal | QQE 窗口偏向。 |
| `signal:ut_window_direction` | signal | UT Bot 窗口方向。 |
| `signal:ssl_window_bias` | signal | SSL Channel 窗口偏向。 |
| `signal:range_filter_window_state` | signal | Range Filter 窗口状态：`up`、`down`、`flat`。 |
| `signal:nw_window_bias` | signal | 非重绘 Nadaraya-Watson 窗口偏向。 |
| `signal:tradingview_window_bias` | signal | QQE、UT、SSL、Range Filter、Nadaraya-Watson 合成偏向。 |
| `value:tradingview_window_score` | value | 合成偏向分数，正数偏多，负数偏空。 |
| `signal:exhaustion_risk` | signal | TD、WVF、Nadaraya-Watson 包络综合衰竭风险。 |

这些语义字段不是底层事实数据的替代品。它们是当前策略消费的稳定接口，后续可以在 Go 聚合层调整口径，再让策略保持较小改动。

## 命名约定

- `*_pct`：百分比距离或百分比变化。
- `*_distance_pct`：当前价格相对某条线或某个价位的距离。
- `*_slope*`：斜率或近期变化。
- `*_cross`：交叉信号，常见值为 `golden`、`dead`、`none`。
- `*_direction`：方向，常见值为 `up/down/bull/bear/range/neutral`。
- `*_state`：状态聚合，适合做过滤。
- `*_divergence`：背离，常见值为 `bullish`、`bearish`、`none`。

## 基础质量字段

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.sample_count` | value | 当前参与计算的已闭合 K 线数量。 |
| `values.required_count` | value | 当前配置需要的最小样本数。 |
| `signals.data_quality` | signal | 数据质量：`ok`、`insufficient`、`gap`、`invalid_ohlc`、`zero_volume`。 |
| `signals.data_quality_reason` | signal | 数据质量异常原因。 |

策略入口应优先检查 `data_quality == ok`。

## 基础价格和成交量派生

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.change_pct` | value | 当前 K 线开收涨跌幅。 |
| `values.amplitude_pct` | value | 当前 K 线振幅。 |
| `values.body_ratio` | value | 实体占高低区间比例。 |
| `values.upper_shadow_ratio` | value | 上影线比例。 |
| `values.lower_shadow_ratio` | value | 下影线比例。 |
| `values.volume_ratio20` | value | 当前成交量相对 20 周期均量。 |

这些字段适合识别单根 K 线力度和异常放量，不适合单独作为趋势方向。

## 均线和趋势结构

### 基础均线

`calculator` 会按配置输出：

- `values.sma{period}`
- `values.ema{period}`
- `values.wma{period}`

默认周期当前为 `7/25/99`。

### EMA 结构

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.price_ema7_distance_pct` | value | 价格相对 EMA7 的距离。 |
| `values.price_ema25_distance_pct` | value | 价格相对 EMA25 的距离。 |
| `values.price_ema99_distance_pct` | value | 价格相对 EMA99 的距离。 |
| `values.ema25_slope5_pct` | value | EMA25 近 5 根斜率。 |
| `values.ema_spread_pct` | value | EMA7 与 EMA99 的价差百分比。 |
| `values.ma_trend_strength` | value | 均线趋势强度。 |
| `values.ma_group_spread_pct` | value | EMA7/25/99 均线组发散程度。 |
| `signals.ema_alignment` | signal | EMA 多空排列：`bull`、`bear`、`mixed`。 |
| `signals.trend_direction` | signal | 趋势方向：`up`、`down`、`range`。 |
| `signals.ma_state` | signal | 均线状态。 |
| `signals.ma_arrangement` | signal | 均线排列。 |
| `signals.ma_cross` | signal | EMA7 与 EMA25 交叉。 |
| `signals.ma_spread_state` | signal | 均线发散状态。 |
| `signals.ma_compression` | signal | 均线压缩状态，适合过滤横盘缠绕。 |
| `signals.ma_slope_state` | signal | 均线斜率状态。 |
| `signals.ma_breakout` | signal | 价格相对均线组突破状态。 |

策略建议：EMA 结构适合作为方向确认和横盘过滤，不建议作为唯一入场触发。

### 扩展均线

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.hma21` | value | Hull MA 21。 |
| `values.hma21_slope3_pct` | value | HMA21 近 3 根斜率。 |
| `values.vwma20` | value | 成交量加权均线 20。 |
| `values.dema21` | value | DEMA 21。 |
| `values.tema21` | value | TEMA 21。 |
| `values.kama10` | value | KAMA 10。 |

### Alligator

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.alligator_jaw` | value | Alligator jaw。 |
| `values.alligator_teeth` | value | Alligator teeth。 |
| `values.alligator_lips` | value | Alligator lips。 |
| `values.alligator_spread_pct` | value | 三线发散程度。 |
| `signals.alligator_direction` | signal | Alligator 方向。 |
| `signals.alligator_state` | signal | Alligator 状态。 |

### TV 派生均线脚本

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.script_dual_ma_out1` | value | 脚本第一条均线。 |
| `values.script_dual_ma_out2` | value | 脚本第二条均线。 |
| `values.script_dual_ma_out1_slope_pct` | value | 第一条均线斜率。 |
| `values.script_dual_ma_out2_slope_pct` | value | 第二条均线斜率。 |
| `signals.script_dual_ma_cross` | signal | 双均线交叉。 |
| `signals.script_ma1_direction` | signal | 第一条均线方向。 |
| `signals.script_price_cross_ma1` | signal | 价格穿越第一条均线。 |
| `signals.script_price_cross_ma2` | signal | 价格穿越第二条均线。 |
| `values.script_ma_breakout_pct` | value | 均线脚本突破幅度。 |
| `values.script_ma_mid_direction` | value | 中线方向数值。 |
| `signals.script_ma_signal` | signal | 均线脚本信号。 |

### EMD

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.emd_avg` | value | EMD 平均线。 |
| `values.emd_value` | value | EMD 当前值。 |
| `values.emd_upper` | value | EMD 上轨。 |
| `values.emd_lower` | value | EMD 下轨。 |
| `signals.emd_direction` | signal | EMD 方向。 |
| `signals.emd_cross` | signal | EMD 交叉状态。 |

## MACD

当前有标准 MACD 和快速 MACD 两套。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.macd` | value | 标准 MACD 线，参数 12/26/9。 |
| `values.macd_signal` | value | 标准 MACD signal 线。 |
| `values.macd_hist` | value | 标准 MACD 柱。 |
| `values.macd_hist_delta` | value | MACD 柱变化。 |
| `values.macd_zero_distance` | value | MACD 离零轴距离。 |
| `signals.macd_cross` | signal | 标准 MACD 金叉/死叉。 |
| `signals.macd_zone` | signal | MACD 多空区域。 |
| `signals.macd_momentum` | signal | MACD 动能状态。 |
| `signals.macd_hist_phase` | signal | MACD 柱体四状态：`above_rising`、`above_falling`、`below_falling`、`below_rising`。 |
| `signals.macd_signal_side` | signal | MACD 线相对 signal 线位置：`above_signal`、`below_signal`。 |
| `signals.macd_divergence` | signal | MACD 背离。 |
| `values.macd_fast` | value | 快速 MACD 线，参数 7/19/9。 |
| `values.macd_fast_signal` | value | 快速 MACD signal 线。 |
| `values.macd_fast_hist` | value | 快速 MACD 柱。 |
| `values.macd_fast_hist_delta` | value | 快速 MACD 柱变化。 |
| `values.macd_fast_zero_distance` | value | 快速 MACD 离零轴距离。 |
| `signals.macd_fast_cross` | signal | 快速 MACD 金叉/死叉。 |
| `signals.macd_fast_zone` | signal | 快速 MACD 多空区域。 |
| `signals.macd_fast_momentum` | signal | 快速 MACD 动能状态。 |
| `signals.macd_fast_hist_phase` | signal | 快速 MACD 柱体四状态。 |
| `signals.macd_fast_signal_side` | signal | 快速 MACD 线相对 signal 线位置。 |
| `signals.macd_fast_divergence` | signal | 快速 MACD 背离。 |

策略建议：标准 MACD 更稳，快速 MACD 更适合 3 分钟入场动能确认。横盘时不要只看交叉，要结合柱体连续性和均线发散。

## 震荡和动量指标

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.rsi14` | value | RSI 14。 |
| `values.rsi_slope3` | value | RSI 近 3 根变化。 |
| `signals.rsi_state` | signal | RSI 超买/超卖/中性状态。 |
| `signals.rsi_divergence` | signal | RSI 背离。 |
| `values.kdj_k` | value | KDJ K。 |
| `values.kdj_d` | value | KDJ D。 |
| `values.kdj_j` | value | KDJ J。 |
| `values.stoch_k` | value | Stochastic K。 |
| `values.stoch_d` | value | Stochastic D。 |
| `values.stoch_rsi_k` | value | Stoch RSI K。 |
| `values.stoch_rsi_d` | value | Stoch RSI D。 |
| `signals.stoch_rsi_state` | signal | Stoch RSI 状态。 |
| `values.skdj_k` | value | SKDJ K。 |
| `values.skdj_d` | value | SKDJ D。 |
| `signals.skdj_cross` | signal | SKDJ 交叉。 |
| `values.cci20` | value | CCI 20。 |
| `signals.cci_state` | signal | CCI 状态。 |
| `values.williams_r14` | value | Williams %R 14。 |
| `signals.williams_state` | signal | Williams %R 状态。 |
| `values.roc12` | value | ROC 12。 |
| `signals.roc_state` | signal | ROC 状态。 |

### WaveTrend

LazyBear WaveTrend，默认参数 `10/21/4`，口径为 `hlc3 -> EMA(10) -> CI -> EMA(21)`，信号线为 `SMA(wt1, 4)`。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.wavetrend_wt1` | value | WaveTrend 主线。 |
| `values.wavetrend_wt2` | value | WaveTrend 信号线。 |
| `values.wavetrend_delta` | value | `wt1 - wt2`。 |
| `signals.wavetrend_cross` | signal | WT1/WT2 交叉。 |
| `signals.wavetrend_zone` | signal | `overbought`、`oversold`、`upper`、`lower`、`bull`、`bear`、`neutral`。 |
| `signals.wavetrend_momentum` | signal | 动能增强/减弱/走平。 |

策略建议：WaveTrend 比 MACD 更敏感，适合 3 分钟入场前确认短线动能，但横盘中会频繁交叉。

### QQE Mod

QQE Mod 第一版采用非重绘口径，默认参数为 RSI `6`、平滑 `5`、QQE factor `3`。增强字段采用 primary/secondary QQE 加 Bollinger 过滤口径，保留旧字段兼容窗口分析。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.qqe_line` | value | 平滑后的 RSI 动能线。 |
| `values.qqe_signal` | value | QQE trailing level。 |
| `values.qqe_hist` | value | `qqe_line - qqe_signal`。 |
| `values.qqe_primary_line` | value | Primary QQE 平滑 RSI。 |
| `values.qqe_primary_trend` | value | Primary QQE trend line。 |
| `values.qqe_secondary_line` | value | Secondary QQE 平滑 RSI。 |
| `values.qqe_secondary_trend` | value | Secondary QQE trend line。 |
| `values.qqe_bb_upper` | value | Primary QQE trend line histogram 的 Bollinger 上轨。 |
| `values.qqe_bb_lower` | value | Primary QQE trend line histogram 的 Bollinger 下轨。 |
| `values.qqe_primary_hist` | value | `qqe_primary_line - 50`。 |
| `values.qqe_secondary_hist` | value | `qqe_secondary_line - 50`。 |
| `signals.qqe_trend` | signal | QQE 趋势状态：`bull`、`bear`、`neutral`。 |
| `signals.qqe_cross` | signal | QQE 线和信号线交叉：`golden`、`dead`、`none`。 |
| `signals.qqe_mod_signal` | signal | 双 QQE 与 Bollinger 过滤后的信号：`up`、`down`、`none`。 |
| `signals.qqe_primary_zero_cross` | signal | Primary QQE 平滑 RSI 穿越 50：`up`、`down`、`none`。 |

策略建议：QQE 适合作为 Supertrend、Donchian 突破后的动能确认，不建议单独作为入场信号。

## 波动率和趋势指标

### ATR / ADX / DI

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.atr14` | value | ATR 14。 |
| `values.atr_pct14` | value | ATR 占价格比例。 |
| `values.natr14` | value | Normalized ATR。 |
| `signals.volatility_state` | signal | 波动率状态。 |
| `values.adx14` | value | ADX 14，按 TV 开源脚本逻辑计算。 |
| `values.di_plus14` | value | DI+。 |
| `values.di_minus14` | value | DI-。 |
| `signals.adx_trend_strength` | signal | ADX 趋势强度。 |
| `signals.di_direction` | signal | DI 方向。 |

策略建议：ADX/DI 适合过滤无趋势环境。大周期滞后时不要作为硬拦截，可以降权或只要求“不反向”。

### TradingView 趋势辅助

以下指标均只使用已闭合 K 线，不使用重绘口径。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.ut_stop` | value | UT Bot ATR trailing stop，默认 ATR `10`、倍数 `1`。 |
| `values.ut_stop_distance_pct` | value | 收盘价相对 UT stop 的距离。 |
| `signals.ut_direction` | signal | UT Bot 方向：`up`、`down`。 |
| `signals.ut_signal` | signal | UT Bot 翻转信号：`buy`、`sell`、`none`。 |
| `values.ssl_upper` | value | SSL Channel 上轨，默认周期 `10`。 |
| `values.ssl_lower` | value | SSL Channel 下轨，默认周期 `10`。 |
| `values.ssl_width_pct` | value | SSL Channel 宽度百分比。 |
| `signals.ssl_direction` | signal | SSL 方向：`bull`、`bear`、`neutral`。 |
| `signals.ssl_cross` | signal | SSL 方向交叉：`golden`、`dead`、`none`。 |
| `values.range_filter` | value | Range Filter 过滤线，默认周期 `100`、倍数 `3`。 |
| `values.range_filter_upper` | value | Range Filter 上带。 |
| `values.range_filter_lower` | value | Range Filter 下带。 |
| `values.range_filter_distance_pct` | value | 收盘价相对 Range Filter 的距离。 |
| `signals.range_filter_direction` | signal | Range Filter 方向：`up`、`down`、`flat`。 |

策略建议：UT Bot 更适合做趋势止损和方向翻转提醒；SSL 和 Range Filter 更适合过滤震荡噪音。

### 恐慌、衰竭和非重绘包络

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.wvf` | value | Williams Vix Fix，默认周期 `22`。 |
| `values.wvf_mid_line` | value | WVF 布林中轨，默认长度 `20`。 |
| `values.wvf_upper_band` | value | WVF 布林上轨，默认长度 `20`、倍数 `2`。 |
| `values.wvf_lower_band` | value | WVF 布林下轨，默认长度 `20`、倍数 `2`。 |
| `values.wvf_range_high` | value | WVF 回看高位阈值，默认回看 `50`、分位系数 `0.85`。 |
| `values.wvf_range_low` | value | WVF 回看低位阈值，默认回看 `50`、低位系数 `1.01`。 |
| `signals.wvf_state` | signal | WVF 状态：`panic`、`normal`。 |
| `signals.wvf_zone` | signal | WVF 细分区域：`panic`、`low_volatility`、`normal`。 |
| `values.td_buy_setup_count` | value | TD Sequential 买入 setup 计数。 |
| `values.td_sell_setup_count` | value | TD Sequential 卖出 setup 计数。 |
| `signals.td_exhaustion` | signal | TD setup 9 衰竭状态：`buy`、`sell`、`none`。 |
| `values.nw_middle` | value | 非重绘 Nadaraya-Watson 中线，默认长度 `50`、bandwidth `8`。 |
| `values.nw_upper` | value | 非重绘 Nadaraya-Watson 上轨。 |
| `values.nw_lower` | value | 非重绘 Nadaraya-Watson 下轨。 |
| `values.nw_width_pct` | value | Nadaraya-Watson 包络宽度百分比。 |
| `values.nw_position` | value | 收盘价在 Nadaraya-Watson 包络中的位置。 |
| `signals.nw_trend` | signal | Nadaraya-Watson 中线趋势：`up`、`down`、`flat`。 |
| `signals.nw_position_state` | signal | 收盘价相对包络位置：`breakout_up`、`breakout_down`、`inside`。 |

策略建议：WVF 用于识别恐慌砸盘，不适合直接追多；TD setup 适合提示趋势衰竭；Nadaraya-Watson 只作为平滑包络参考，避免用重绘版本做回测。

### Bollinger / Donchian / Keltner / Squeeze

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.bb_upper` | value | 布林上轨。 |
| `values.bb_middle` | value | 布林中轨。 |
| `values.bb_lower` | value | 布林下轨。 |
| `values.bb_width_pct` | value | 布林带宽度百分比。 |
| `values.bb_percent_b` | value | %B 位置。 |
| `values.bb_width_delta` | value | 布林宽度变化。 |
| `values.bb_middle_slope_pct` | value | 中轨斜率。 |
| `values.bb_upper_slope_pct` | value | 上轨斜率。 |
| `values.bb_lower_slope_pct` | value | 下轨斜率。 |
| `signals.bb_position` | signal | 价格在布林带中的位置。 |
| `signals.bb_width_state` | signal | 布林带扩张/收缩。 |
| `signals.bb_trend` | signal | 布林中轨趋势。 |
| `values.donchian_high20` | value | Donchian 20 高点。 |
| `values.donchian_low20` | value | Donchian 20 低点。 |
| `values.donchian_mid20` | value | Donchian 20 中轨。 |
| `values.donchian_width_pct20` | value | Donchian 20 通道宽度百分比。 |
| `values.donchian_position20` | value | 收盘价在 Donchian 20 通道中的位置。 |
| `signals.donchian_breakout` | signal | Donchian 通道突破状态：`breakout_up`、`breakout_down`、`inside`。 |
| `values.keltner_upper20` | value | Keltner 20 上轨，口径为 EMA20 + ATR20 * 2。 |
| `values.keltner_middle20` | value | Keltner 20 中轨，口径为 EMA20。 |
| `values.keltner_lower20` | value | Keltner 20 下轨，口径为 EMA20 - ATR20 * 2。 |
| `values.keltner_width_pct20` | value | Keltner 20 通道宽度百分比。 |
| `values.keltner_position20` | value | 收盘价在 Keltner 20 通道中的位置。 |
| `signals.keltner_breakout` | signal | Keltner 通道突破状态：`breakout_up`、`breakout_down`、`inside`。 |
| `values.squeeze_momentum` | value | LazyBear Squeeze Momentum 值。 |
| `values.squeeze_momentum_delta` | value | Squeeze Momentum 变化。 |
| `signals.squeeze` | signal | Squeeze 状态。 |
| `signals.squeeze_state` | signal | Squeeze 派生状态。 |
| `signals.momentum_state` | signal | Squeeze 动能状态。 |

策略建议：Squeeze 适合识别压缩后释放。`release_up/release_down` 类状态更适合配合 Supertrend 信号。

## 趋势跟踪指标

### Supertrend

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.supertrend` | value | 当前 Supertrend 线。 |
| `values.supertrend_distance_pct` | value | 价格相对 Supertrend 距离。 |
| `values.supertrend_stop_distance_pct` | value | Supertrend 止损距离。 |
| `signals.supertrend_direction` | signal | Supertrend 方向。 |
| `signals.supertrend_flip` | signal | 是否发生方向翻转。 |
| `values.supertrend_7_2` | value | 7/2 参数预设。 |
| `values.supertrend_10_3` | value | 10/3 参数预设。 |
| `values.supertrend_10_3_3` | value | 10/3.3 参数预设。 |
| `values.supertrend_14_4` | value | 14/4 参数预设。 |
| `signals.supertrend_7_2_direction` | signal | 7/2 方向。 |
| `signals.supertrend_10_3_direction` | signal | 10/3 方向。 |
| `signals.supertrend_10_3_3_direction` | signal | 10/3.3 方向。 |
| `signals.supertrend_14_4_direction` | signal | 14/4 方向。 |
| `values.adaptive_supertrend` | value | ATR K-Means 自适应 Supertrend 线，默认 ATR `10`、factor `3`、训练窗口 `100`。 |
| `values.adaptive_supertrend_distance_pct` | value | 当前价格相对自适应 Supertrend 的距离。 |
| `values.adaptive_supertrend_assigned_atr` | value | 当前波动簇分配给 Supertrend 的 ATR centroid。 |
| `values.adaptive_supertrend_high_centroid` | value | 高波动 ATR centroid。 |
| `values.adaptive_supertrend_mid_centroid` | value | 中波动 ATR centroid。 |
| `values.adaptive_supertrend_low_centroid` | value | 低波动 ATR centroid。 |
| `signals.adaptive_supertrend_direction` | signal | 自适应 Supertrend 方向。 |
| `signals.adaptive_supertrend_flip` | signal | 自适应 Supertrend 方向翻转。 |
| `signals.adaptive_supertrend_volatility_cluster` | signal | 当前 ATR 波动簇：`high`、`medium`、`low`。 |
| `values.ai_supertrend` | value | SuperTrend AI 线，按 factor 表现聚类选择最佳参数。 |
| `values.ai_supertrend_ama` | value | SuperTrend AI trailing stop 的表现指数自适应均线。 |
| `values.ai_supertrend_distance_pct` | value | 当前价格相对 SuperTrend AI 的距离。 |
| `values.ai_supertrend_target_factor` | value | 从 best performance cluster 选出的目标 factor。 |
| `values.ai_supertrend_performance_index` | value | best cluster 表现指数，按价格变化 EMA 归一化。 |
| `values.ai_supertrend_best_centroid` | value | best factor performance cluster centroid。 |
| `values.ai_supertrend_average_centroid` | value | average factor performance cluster centroid。 |
| `values.ai_supertrend_worst_centroid` | value | worst factor performance cluster centroid。 |
| `signals.ai_supertrend_direction` | signal | SuperTrend AI 方向。 |
| `signals.ai_supertrend_flip` | signal | SuperTrend AI 方向翻转。 |
| `signals.ai_supertrend_cluster` | signal | 当前使用的 performance cluster，默认 `best`。 |
| `signals.ai_supertrend_factor_cluster` | signal | 目标 factor 来源 cluster，默认 `best`。 |
| `values.ai_source_ma` | value | AI Source Switching MA，默认 EMA(50)。 |
| `values.ai_source_value` | value | O/H/L/C 动态源选择后平滑的 source。 |
| `values.ai_source_drive` | value | KNN analog、agreement、tightness 综合驱动力。 |
| `values.ai_source_score_open` | value | Open 源综合评分。 |
| `values.ai_source_score_high` | value | High 源综合评分。 |
| `values.ai_source_score_low` | value | Low 源综合评分。 |
| `values.ai_source_score_close` | value | Close 源综合评分。 |
| `values.ai_source_supertrend` | value | 基于 AI source 与自适应 ATR multiplier 的 Supertrend trail。 |
| `values.ai_source_supertrend_distance_pct` | value | 当前价格相对 AI source Supertrend 的距离。 |
| `values.ai_source_supertrend_adapt_mult` | value | AI source Supertrend 当前自适应 ATR multiplier。 |
| `signals.ai_source_selected` | signal | 当前选中的 OHLC 源：`open`、`high`、`low`、`close`。 |
| `signals.ai_source_changed` | signal | 当前 K 线是否切换了选中源。 |
| `signals.ai_source_supertrend_direction` | signal | AI source Supertrend 方向：`bull`、`bear`。 |
| `signals.ai_source_supertrend_flip` | signal | AI source Supertrend 翻转：`buy`、`sell`、`none`。 |
| `signals.ai_source_ready` | signal | AI source memory bank 是否已达到可用样本。 |
| `values.supertrend_zone_pivot_high` | value | 最近 Supertrend 翻转区间高点。 |
| `values.supertrend_zone_pivot_low` | value | 最近 Supertrend 翻转区间低点。 |
| `values.supertrend_zone_mid` | value | Supertrend zone 中位线。 |
| `values.supertrend_zone_fib_236` | value | Supertrend zone Fibonacci 0.236。 |
| `values.supertrend_zone_fib_382` | value | Supertrend zone Fibonacci 0.382。 |
| `values.supertrend_zone_fib_5` | value | Supertrend zone Fibonacci 0.5。 |
| `values.supertrend_zone_fib_618` | value | Supertrend zone Fibonacci 0.618。 |
| `values.supertrend_zone_fib_786` | value | Supertrend zone Fibonacci 0.786。 |
| `values.supertrend_zone_extension_1618` | value | Supertrend zone 顺势/逆势扩展位。 |
| `values.supertrend_zone_premium_band` | value | Supertrend 线叠加 ATR band 的上侧参考。 |
| `values.supertrend_zone_discount_band` | value | Supertrend 线叠加 ATR band 的下侧参考。 |
| `values.supertrend_zone_position_pct` | value | 当前收盘价在 Supertrend zone 高低点区间内的位置百分比。 |
| `signals.supertrend_zone_side` | signal | Supertrend zone 当前方向：`bull`、`bear`。 |
| `signals.supertrend_zone_area` | signal | 当前价格区域：`discount`、`mid`、`premium`、`extension`。 |
| `signals.supertrend_zone_ready` | signal | 是否已有足够 Supertrend 翻转点生成 zone。 |

策略建议：当前 Supertrend 策略把它作为主触发。为了防止 3 分钟来回翻转，需要结合窗口稳定性、均线发散、MACD/WaveTrend 动能和成交量确认。

### AlphaTrend / PSAR / Chandelier

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.alphatrend` | value | AlphaTrend 线。 |
| `values.alphatrend_distance_pct` | value | 价格相对 AlphaTrend 距离。 |
| `values.alphatrend_slope_pct` | value | AlphaTrend 斜率。 |
| `signals.alphatrend_direction` | signal | AlphaTrend 方向。 |
| `signals.alphatrend_flip` | signal | AlphaTrend 翻转。 |
| `signals.alphatrend_cross` | signal | AlphaTrend 与两根前 AlphaTrend 的交叉。 |
| `signals.alphatrend_signal` | signal | 按 TV 过滤逻辑确认后的 AlphaTrend 信号。 |
| `values.psar` | value | Parabolic SAR。 |
| `values.psar_distance_pct` | value | 价格相对 PSAR 距离。 |
| `signals.psar_direction` | signal | PSAR 方向。 |
| `values.chandelier_long` | value | Chandelier 多头止损线。 |
| `values.chandelier_short` | value | Chandelier 空头止损线。 |
| `values.chandelier_stop_distance_pct` | value | Chandelier 止损距离。 |
| `signals.chandelier_direction` | signal | Chandelier 方向。 |

策略建议：AlphaTrend 适合做 Supertrend 的方向佐证，Chandelier 更适合做止损/出场参考。

## 成交量、资金流和 VWAP

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.volume_ma20` | value | 20 周期成交量均线。 |
| `values.obv` | value | OBV。 |
| `values.obv_slope5` | value | OBV 近 5 根斜率。 |
| `values.vwap` | value | 全窗口 VWAP。 |
| `values.vwap_distance_pct` | value | 价格相对 VWAP 距离。 |
| `values.rolling_vwap20` | value | 20 周期滚动 VWAP。 |
| `values.rolling_vwap_distance_pct` | value | 价格相对滚动 VWAP 距离。 |
| `values.mfi14` | value | MFI 14。 |
| `values.cmf20` | value | CMF 20。 |
| `values.ad_line` | value | Accumulation/Distribution line。 |
| `values.ad_line_slope5` | value | A/D line 近 5 根斜率。 |
| `values.price_volume_trend` | value | PVT。 |
| `values.vfi` | value | LazyBear Volume Flow Indicator，默认长度 `130`。 |
| `values.vfi_signal` | value | VFI 的 EMA 信号线，默认长度 `5`。 |
| `values.vfi_hist` | value | `vfi - vfi_signal`。 |
| `values.vfi_volume_cutoff` | value | VFI 成交量截断阈值，默认 `SMA(volume, 130)[1] * 2.5`。 |
| `values.vfi_price_cutoff` | value | VFI 价格变化过滤阈值，默认 `0.2 * stdev(log(hlc3/hlc3[1]), 30) * close`。 |
| `values.volume_zscore20` | value | 成交量 20 周期 z-score。 |
| `values.volume_ratio5` | value | 5 周期成交量比。 |
| `values.volume_ratio10` | value | 10 周期成交量比。 |
| `values.volume_breakout_ratio` | value | 突破成交量比。 |
| `values.volume_trend5` | value | 成交量近 5 根趋势。 |
| `values.volume_divergence_score` | value | 价量背离分数。 |
| `values.volume_pressure20` | value | 20 周期量压。 |
| `values.supply_zone_top` | value | 近 120 根 Volume Range 供给区上边界。 |
| `values.supply_zone_bottom` | value | 近 120 根 Volume Range 供给区下边界。 |
| `values.supply_zone_avg` | value | 供给区均值线。 |
| `values.supply_zone_wavg` | value | 供给区成交量加权均值线。 |
| `values.demand_zone_top` | value | 近 120 根 Volume Range 需求区上边界。 |
| `values.demand_zone_bottom` | value | 近 120 根 Volume Range 需求区下边界。 |
| `values.demand_zone_avg` | value | 需求区均值线。 |
| `values.demand_zone_wavg` | value | 需求区成交量加权均值线。 |
| `values.supply_demand_equilibrium` | value | 供需可见区间中轴。 |
| `values.supply_demand_weighted_equilibrium` | value | 供需加权中轴。 |
| `signals.money_flow` | signal | 资金流方向。 |
| `signals.volume_state` | signal | 成交量状态。 |
| `signals.price_volume_confirmation` | signal | 价量确认。 |
| `signals.cmf_state` | signal | CMF 状态。 |
| `signals.price_volume_action` | signal | 价量行为。 |
| `signals.breakout_volume_confirm` | signal | 突破量确认。 |
| `signals.breakout_volume_strength` | signal | 突破量强度。 |
| `signals.volume_divergence` | signal | 价量背离。 |
| `signals.volume_phase` | signal | 成交量阶段。 |
| `signals.vfi_state` | signal | VFI 资金流状态：`inflow`、`outflow`、`neutral`。 |
| `signals.vfi_cross` | signal | VFI 与信号线交叉：`golden`、`dead`、`none`。 |
| `signals.vfi_momentum` | signal | VFI 柱体动能：`rising`、`falling`、`flat`。 |
| `signals.supply_demand_position` | signal | 最新收盘价相对供需区位置：`above_supply`、`in_supply`、`between_zones`、`in_demand`、`below_demand`。 |

策略建议：成交量字段更适合过滤“无力假信号”，例如 Supertrend 翻转但成交量萎缩、价量背离时降低信号质量。

### Dynamic Swing Anchored VWAP

基于 swing high/low 动态锚定的 VWAP，参考 Zeiierman 脚本思想。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.dynamic_swing_vwap` | value | 当前动态锚定 VWAP。 |
| `values.dynamic_swing_vwap_distance_pct` | value | 价格相对动态锚定 VWAP 的距离。 |
| `values.dynamic_swing_vwap_anchor_price` | value | 当前锚点价格。 |
| `values.dynamic_swing_vwap_anchor_age` | value | 锚点距离当前 K 线数量。 |
| `signals.dynamic_swing_vwap_direction` | signal | VWAP 结构方向。 |
| `signals.dynamic_swing_vwap_position` | signal | 价格位于 VWAP 上方/下方/附近。 |
| `signals.dynamic_swing_vwap_anchor_type` | signal | 锚点类型：swing high 或 swing low。 |
| `signals.dynamic_swing_vwap_swing_label` | signal | HH/HL/LH/LL 标签。 |

策略建议：不要作为主触发，更适合作为趋势成本线过滤和出场参考。

## 支撑阻力和结构

### 支撑阻力 / Fibonacci / Pivot

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.support_1` | value | 第一支撑位。 |
| `values.support_2` | value | 第二支撑位。 |
| `values.resistance_1` | value | 第一阻力位。 |
| `values.resistance_2` | value | 第二阻力位。 |
| `values.support_strength` | value | 支撑强度。 |
| `values.resistance_strength` | value | 阻力强度。 |
| `values.support_distance_pct` | value | 价格到支撑位距离。 |
| `values.resistance_distance_pct` | value | 价格到阻力位距离。 |
| `signals.sr_position` | signal | 价格相对支撑阻力位置。 |
| `values.fib_236` | value | Fibonacci 0.236。 |
| `values.fib_382` | value | Fibonacci 0.382。 |
| `values.fib_5` | value | Fibonacci 0.5。 |
| `values.fib_618` | value | Fibonacci 0.618。 |
| `values.fib_786` | value | Fibonacci 0.786。 |
| `signals.fib_zone` | signal | 当前 Fibonacci 区间。 |
| `values.pivot_point` | value | Pivot point。 |
| `values.pivot_r1` | value | Pivot R1。 |
| `values.pivot_r2` | value | Pivot R2。 |
| `values.pivot_s1` | value | Pivot S1。 |
| `values.pivot_s2` | value | Pivot S2。 |
| `signals.pivot_zone` | signal | Pivot 区间。 |

### Ichimoku

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.ichimoku_tenkan` | value | 转换线。 |
| `values.ichimoku_kijun` | value | 基准线。 |
| `values.ichimoku_span_a` | value | 云层 Span A。 |
| `values.ichimoku_span_b` | value | 云层 Span B。 |
| `signals.ichimoku_trend` | signal | Ichimoku 趋势。 |
| `signals.ichimoku_cloud` | signal | 价格相对云层。 |
| `signals.ichimoku_cross` | signal | Tenkan/Kijun 交叉。 |

### Smart Money / Market Structure

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.swing_high` | value | 最近结构高点。 |
| `values.swing_low` | value | 最近结构低点。 |
| `values.swing_high_distance_pct` | value | 价格到结构高点距离。 |
| `values.swing_low_distance_pct` | value | 价格到结构低点距离。 |
| `values.order_block_high` | value | Order block 高点。 |
| `values.order_block_low` | value | Order block 低点。 |
| `values.order_block_mid` | value | Order block 中点。 |
| `values.momentum_supply_top` | value | 动量触发供给区上边界。 |
| `values.momentum_supply_bottom` | value | 动量触发供给区下边界。 |
| `values.momentum_supply_mid` | value | 动量触发供给区中线。 |
| `values.momentum_supply_age` | value | 动量触发供给区距离当前 K 线数量。 |
| `values.momentum_demand_top` | value | 动量触发需求区上边界。 |
| `values.momentum_demand_bottom` | value | 动量触发需求区下边界。 |
| `values.momentum_demand_mid` | value | 动量触发需求区中线。 |
| `values.momentum_demand_age` | value | 动量触发需求区距离当前 K 线数量。 |
| `values.liquidity_sweep_level` | value | 最近流动性扫单参考位。 |
| `values.liquidity_sweep_top` | value | 最近流动性扫单区域上边界。 |
| `values.liquidity_sweep_bottom` | value | 最近流动性扫单区域下边界。 |
| `values.liquidity_sweep_age` | value | 最近流动性扫单距离当前 K 线数量。 |
| `signals.market_structure` | signal | 市场结构：BOS、range 等。 |
| `signals.smart_money` | signal | Smart money 事件，如流动性扫单。 |
| `signals.choch` | signal | CHOCH 方向。 |
| `signals.structure_event` | signal | 结构事件。 |
| `signals.structure_bias` | signal | 结构方向偏置。 |
| `signals.swing_high_strength` | signal | Swing 高点强弱：`strong`、`weak`、`unknown`。 |
| `signals.swing_low_strength` | signal | Swing 低点强弱：`strong`、`weak`、`unknown`。 |
| `signals.internal_swing_high_strength` | signal | Internal swing 高点强弱。 |
| `signals.internal_swing_low_strength` | signal | Internal swing 低点强弱。 |
| `signals.momentum_sd_position` | signal | 价格相对动量供需区位置：`above_supply`、`in_supply`、`between_zones`、`in_demand`、`below_demand`、`unknown`。 |
| `signals.momentum_sd_retest` | signal | 动量供需区回测事件：`supply_retest`、`demand_retest`、`none`。 |
| `signals.momentum_sd_break` | signal | 动量供需区失效事件：`supply_break`、`demand_break`、`none`。 |
| `signals.liquidity_sweep_type` | signal | 流动性扫单类型：`wick_high`、`wick_low`、`retest_high`、`retest_low`、`none`。 |

策略建议：结构类字段适合做突破有效性判断和止盈止损位置参考。

## K 线形态和 Heikin Ashi

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `signals.candle_pattern` | signal | K 线形态，如吞没、晨星、锤子线、顶部/底部结构等。 |
| `signals.candle_bias` | signal | K 线方向偏置。 |
| `signals.candle_strength` | signal | K 线形态强度。 |
| `signals.pin_bar` | signal | 是否 pin bar。 |
| `values.ha_open` | value | Heikin Ashi open。 |
| `values.ha_high` | value | Heikin Ashi high。 |
| `values.ha_low` | value | Heikin Ashi low。 |
| `values.ha_close` | value | Heikin Ashi close。 |
| `signals.ha_trend` | signal | Heikin Ashi 趋势。 |
| `signals.ha_strength` | signal | Heikin Ashi 强度。 |

K 线形态容易受单根波动影响，建议只作为辅助确认。

## Livermore

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `values.livermore_key_point` | value | Livermore 关键点。 |
| `values.livermore_pullback_point` | value | 回调点。 |
| `values.livermore_breakout_line` | value | 突破线。 |
| `values.livermore_previous_key_point` | value | 前一个关键点。 |
| `values.livermore_active_point` | value | 当前活跃点。 |
| `signals.livermore_trend` | signal | Livermore 趋势。 |
| `signals.livermore_signal` | signal | Livermore 买卖信号。 |

该类指标需要较长样本，短历史或新上线标的可能没有输出。

## 策略使用建议

### 入场主线

当前更合理的主线是：

1. 3 分钟 Supertrend 首先翻转。
2. EMA 窗口确认方向，并排除均线缠绕。
3. MACD 或 WaveTrend 确认短线动能。
4. ADX/DI、AlphaTrend、成交量、VWAP 作为可靠性过滤。
5. 5/10/15/30 分钟多周期只做方向打分，不作为绝对硬拦截。

### 过滤假信号

优先关注：

- `ma_compression`：均线缠绕时降低信号质量。
- `macd_hist_delta`、`macd_fast_hist_delta`：动能是否同步增强。
- `wavetrend_momentum`：短线动能是否扩张。
- `volume_state`、`price_volume_confirmation`：是否有成交量支持。
- `dynamic_swing_vwap_position`：价格是否站回结构成本线。
- `adx_trend_strength`、`di_direction`：是否有趋势环境。

### 出场参考

可组合使用：

- Supertrend 反向翻转。
- EMA 与 MACD 同步转弱。
- WaveTrend 动能明显衰减。
- 价格跌破/站回 Dynamic Swing Anchored VWAP。
- Chandelier stop 或结构支撑阻力失守。

## 注意事项

- 指标不是越多越好。策略应按职责选择少量指标：触发、确认、过滤、出场。
- 敏感指标如 WaveTrend、KDJ、Stoch RSI 在横盘中会频繁交叉，不适合单独入场。
- 趋势指标如 ADX、AlphaTrend、大周期 EMA 有滞后，不适合作为所有反转的硬拦截。
- TV 脚本来源指标需要关注许可证，尤其是非商用或署名要求。
- 字段命名一旦被策略使用，应避免随意重命名，否则历史窗口和策略兼容性会断开。
