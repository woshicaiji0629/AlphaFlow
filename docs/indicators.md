# 指标文档

本文档描述 Go `market-data` 服务当前输出的底层指标快照字段、窗口聚合特征，以及策略侧的使用方式。

指标计算只使用已闭合 K 线。每个底层快照按交易所、市场、交易对、周期和 open time 写入 Redis 最新状态，并通过 NATS market snapshot 发布给在线 `strategy-engine`；指标不写入 ClickHouse 历史。

## 底层指标存储模型

底层指标快照不是一列一个指标。计算域和传输边界使用不同表示：

- `NumericValues`：进程内数值字段，使用 `map[string]float64`；指标窗口、在线策略和回测优先直接读取该通道。
- `values`：Redis、NATS、JSON 等兼容边界使用的字符串字段，只在需要旧协议时编码。
- `signals`：枚举或状态型字段，统一以字符串存储。

`pkg/indicatorcalc.CalculateWindow` 同时返回兼容字符串和数值通道，供在线发布与持久化使用；`CalculateWindowNumeric` 不构造字符串 map，供回测等纯进程内消费者使用。新增数值指标应优先写入 `ValueSet`，不要在计算热路径中执行 `FormatFloat` 后再由下游 `ParseFloat`。

新增指标通常不需要改数据库字段。只要 Go 计算端写入新的 `values["key"]` 或 `signals["key"]`，Redis 最新快照和 market snapshot 会随之携带。窗口聚合层会枚举当前计算窗口里的指标 key：

- 数值字段会生成最新值、前值、变化、斜率、方向、连续上升/下降次数、区间位置。
- 信号字段会生成最新状态、前值、是否变化、稳定持续次数、距上次变化多久。

注意：新字段只会出现在部署后的新快照里，老历史不会补齐。

底层指标序列不作为长期事实数据保留。K 线才是事实数据；窗口聚合口径或策略特征变化时，从 ClickHouse K 线重新计算指标。Redis 中的指标和窗口特征是恢复缓存、观测和兼容数据；在线策略启动后主要消费 NATS market snapshot 并维护内存态。

## 窗口聚合特征层

当前实时策略路径优先消费 Go `indicatorwindow` 输出的窗口聚合特征。Go `strategy-engine` 启动时可从 Redis hash 恢复初始态，启动后主要通过 NATS market snapshot 接收这些特征，而不是让策略自己拉取大量历史指标再计算窗口。

窗口聚合层分两份 Redis hash，并同步发布到 market snapshot：

- `indwin`：上一根已收盘 K 线的窗口聚合结果。
- `indrt`：当前未收盘 K 线的实时指标表现和 K 线基础信息。
- `market.snapshot.closed`：已收盘底层指标和窗口聚合结果。
- `market.snapshot.realtime`：当前未收盘 K 线、实时指标和价格上下文。

窗口聚合层解决的问题：

- 把几十到几百根历史指标压缩成一个交易对、一个周期的一份特征 hash。
- 把底层指标转换成策略可读语义，例如趋势是否有效、均线是否缠绕、MACD 是否跟随、成交量是否放大。
- 为多策略共享同一份特征，避免每个策略重复做窗口计算。
- 通过 `meta:bar_seq`、`meta:updated_at` 和 `meta:age_limit_ms` 判断数据是否最新。

窗口字段分两类：

- 通用窗口字段：由底层数值和信号自动生成，例如 `{key}_win_latest`、`{key}_win_slope`、`{key}_win_stable_count`。
- 适配语义字段：按指标特点额外聚合，例如 `ma_ribbon_state`、`macd_window_bias`、`pump_window_signal`。

策略应优先使用适配语义字段；只有当策略需要更细粒度判断时，再读取通用窗口字段。

## 公共市场能力

窗口语义之上逐步建设公共市场能力层。它把多个底层指标和窗口状态整理为可被多个策略复用的市场描述，但不产生策略决策。

```text
底层指标
  -> 通用窗口统计
  -> 单周期公共能力
  -> 多周期市场上下文
  -> 策略消费
```

公共能力按趋势、波动、结构、位置、事件和质量六个板块演进。一个能力优先分开输出连续值、离散状态、状态或事件年龄、数据覆盖和可信度；不能只提供无法解释的总分。未来收益、策略触发、仓位和盈亏不得进入实时能力计算。

同名能力必须在所有策略中保持相同计算语义。策略参数可以规定如何使用能力，但不能重新定义公共能力公式。改变既有字段语义时需要升级对应能力板块版本，并重新执行在线/回测等价性和策略回放验证。

当前市场综合能力 v2.7 属于已实现的实验能力；其中方向、风险、延伸风险和覆盖信息可用于独立研究，综合强度尚未证明可以作为稳定策略准入条件。后续优先建设和验证基础能力，不继续围绕单一综合分扩展策略逻辑。

完整约定见 [市场能力层架构](market-capability-architecture.md)。

## 计算窗口和 realtime 性能

`market-data/internal/indicator` 的 runner 会缓存每个交易所、市场、交易对和周期的 `CalculationWindow`。窗口缓存只包含已闭合 K 线；当前未收盘 K 线进入 realtime 路径时，会基于缓存窗口 clone 一个临时窗口，再把 open kline 临时标记为 closed 后追加计算。临时窗口不会写回 runner 缓存，因此不会污染后续 closed K 线窗口。

在线指标路径按三层拆分：K 线预热层默认准备 `310` 根连续 K 线，指标计算层最多预热最近 `50` 个底层指标 snapshot，窗口分析层只读取这些已算好的指标 snapshot。内存和 Redis 中的指标 recent cache 默认保留 `60` 个 snapshot，用于窗口分析、buffer 和服务重启恢复。

`pkg/indicatorcalc.CalculationWindow` 支持基础指标流式状态。runner 在长期缓存窗口和窗口快照递推窗口上启用该状态，连续追加 K 线时复用以下中间结果：

- SMA 和 volume SMA。
- EMA。
- RSI 14 序列。
- ATR 14 和 ATR 22 序列。
- ADX 14、DI+ 和 DI-。
- 标准 MACD 和快速 MACD 序列。
- OBV。
- VWAP。
- WaveTrend。
- MoneyFlow 中的 OBV slope、PVT、PVT slope、AD line 和 AD line slope。
- AlphaTrend、Dynamic Swing VWAP、VFI 和 Heikin Ashi。
- PSAR、KAMA、HMA、EMD、SSL、Range Filter、Williams Vix Fix、TD Sequential、Nadaraya-Watson、UT Bot、QQE 和 Alligator 的末值状态。

这些状态只用于减少重复计算；指标输出仍由 `CalculateWindow` 统一生成。窗口出现缺口或需要替换最后一根 K 线时，runner 会回退到重新构建窗口，优先保持语义正确。

EMA、SMA、volume SMA 和 MACD 的连续状态按配置列表动态创建，不维护固定槽位编号。新增受支持周期或 MACD 组合时，只在对应配置列表追加配置；已经运行的窗口不会凭空获得新周期的历史状态，配置变化后必须用历史 K 线新建或重建窗口。算法内部的 `[5]`、`[10]`、`[50]` 等数组表示公式要求的固定滚动长度，不属于配置槽位，不应为追求表面动态化改成 map。

Realtime preview 不立即复制完整 K 线列表，而是引用 closed base 并保存一根 preview K 线；只有调用 `Klines`、继续 `Append`、再次 preview 或 clone 时才物化独立列表。OHLCV series、连续指标状态和会被追加的历史切片仍保持克隆隔离，preview 计算不能修改 closed window。

`pkg/indicatorcalc.CalculateWindows` 可在固定 warmup 后连续计算结果后缀。market-data runner 用它在 cold start 或缓存缺口时补齐 recent 指标 snapshot；缓存对齐后，窗口分析阶段只读取 recent 指标，不再回放 K 线补算历史指标。

回测引擎和在线 runner 复用同一个 `CalculationWindow`、`EnableBasicState` 和 `CalculateWindow` 计算路径。`SnapshotBuilder` 为每个 symbol/interval 建立独立滚动状态；同一周期内部仍按 K 线时间顺序递推，不同 symbol/interval 则通过共享并发限制分批计算。消费端只推进 `CloseTime <= AsOf` 的结果，确认周期只有真正收盘后才更新，因此不会读取未来数据。回测不会预计算并常驻整段历史指标，也不会为每个决策点重新扫描完整历史前缀。

周期预计算使用无缓冲批次通道提供背压：producer 完成一批后必须等待消费端接收，不能继续堆积未来批次。`[data].indicator_batch_size` 默认是 `30`；`[data].indicator_concurrency = 0` 表示按 `GOMAXPROCS` 自动设置，并由一次回测内的所有 symbol/interval 共享。批次只改变调度和常驻结果数量，不改变指标集合、连续状态或策略语义。

滚动窗口只保留计算所需的固定 K 线数量；递归指标保留连续状态，不因窗口裁剪重新初始化。当前连续状态除基础指标外还包括：

- AI Source 的 feature cursor、KNN bank、Fisher 权重、神经权重和自适应 Supertrend 状态。
- Adaptive Supertrend 的 ATR(10)、最近 100 个 ATR 聚类窗口、上下轨和方向。

这意味着 EMA、RSI、ATR、MACD、AI Source 和 Adaptive Supertrend 使用完整已见历史连续递推语义；SMA、区间最高最低等固定窗口指标仍按当前窗口计算。在线和回测必须使用相同入口，不能在回测侧单独实现一套“批量近似”公式。

回测只保留最近 `indicatorwindow.DefaultLookback` 个底层指标 snapshot。窗口语义分析按策略读取惰性计算，并以最新指标 `CloseTime` 缓存；大周期收盘时仍计算底层指标，但只有策略决策需要该周期窗口时才运行窗口分析。

`indicatorwindow.DefaultLookback` 是在线和回测共享的默认窗口长度。调整该值会同时影响窗口语义，应通过等价性测试和真实数据回放验证，不能只在某个入口单独硬编码。

部分指标还做了局部紧凑化，减少每次窗口分析时的临时数组和重复扫描：

- AI Source 的 source smoothing 和 MA smoothing 使用增量 EMA。
- HMA 只计算最后输出所需的差分窗口。
- DEMA/TEMA 使用流式 EMA 状态取最终值。
- Moving Average、EZ EMA 和脚本均线优先读取 `CalculationWindow` 的 EMA 状态。
- VFI 使用 compact 路径，以滚动 VCP、滚动 `sum/sum²` 波动率和流式 signal EMA 替代旧实现中的嵌套窗口求和、30 点双遍标准差扫描与 signal 序列数组；旧批量实现保留为 fallback 和等价性测试参考。
- `CalculateWindow` 正常路径复用 `CalculationWindow` 已解析 series 做数据质量检查，解析失败时回退旧质量检查逻辑以保留错误原因。
- Nadaraya-Watson envelope 复用栈上权重缓存，避免每个点重复构造权重；MoneyFlow 的区间最高/最低扫描已抽成公共 helper。
- Bollinger 20 和 Donchian 20 复用 `CalculationWindow` 中的滚动 sum/sum² 与单调队列；滚动方差定期全量校正并钳制浮点误差导致的极小负方差。
- ATR、Supertrend、AlphaTrend、QQE、Heikin Ashi、EMD、MACD divergence、AI Supertrend 和 Dynamic Swing VWAP 的只读末值或共享基础序列路径避免构造不必要的完整临时数组。Dynamic Swing VWAP 在非 adaptive 模式下只计算一次固定 APT alpha，并用单调队列维护滚动 swing high/low，避免每个点重复调用 `Log/Exp` 和扫描整个 swing period；相等极值及窗口未填满时的语义保持不变。
- 回测使用 `CalculateWindowNumeric`，不生成旧字符串值 map；策略读取先查 `NumericValues`，只有旧快照才回退解析 `Values`。

当前可用 benchmark：

```text
go test ./market-data/internal/indicator -run '^$' -bench BenchmarkWindowWithTemporaryKlineRealtime -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench 'BenchmarkCalculate(250|300)Bars' -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench BenchmarkCalculateWindowStreaming -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench BenchmarkCalculateWindowNumericStreaming -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench BenchmarkCalculationWindowRealtimePreview -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench BenchmarkVolumeFlowIndicatorCompact -benchmem
go test ./pkg/indicatorcalc -run '^$' -bench BenchmarkDynamicSwingAnchoredVWAP -benchmem
go test ./pkg/indicatorwindow -run '^$' -bench BenchmarkAnalyzeOrderedWindow20 -benchmem
go test ./pkg/strategyframe -run '^$' -bench BenchmarkWindowViewFromResult -benchmem
```

它覆盖 realtime open kline 的临时窗口构造和 `CalculateWindow` 完整特征计算。一次本地基线结果为：

```text
BenchmarkWindowWithTemporaryKlineRealtime-12    12    84095641 ns/op    6730386 B/op    4830 allocs/op
```

早期完整 realtime 基线约 `84ms/op`、`6.7MB/op`、`4830 allocs/op`。针对 `CalculationWindow.RealtimePreview` 的窗口级基准从约 `224850 B/op`、`71 allocs/op` 降至约 `144656 B/op`、`48 allocs/op`；动态配置槽位相对固定数组增加少量小对象分配，但避免周期、下标和 switch 三处同步维护。后续本地 benchmark 受 CPU 负载影响较明显，只作为趋势参考。继续降 CPU 时，应先用生产 profile 证明 AI Source 深克隆或历史切片复制已成为瓶颈，不能仅为压低 alloc 数引入复杂写时复制。

2026-07 的纯数值 streaming 优化基线从约 `255KB/op`、`488 allocs/op` 降至约 `76KB/op`、`146–147 allocs/op`，约 278 个数值字段和 145 个信号保持输出。该数据来自 Intel i7 本地 benchmark，主要用于比较分配趋势；墙钟耗时会受机器负载影响。年度五周期回测保持每个周期内部的时间顺序，并在周期间做有界并发。

本地年度真实路径试验表明，完整指标结果包含大量 map，过大的批次会显著放大常驻内存和 GC 压力。`512` 批次曾出现约 `5GB` 物理内存、约 `8.2GB` 峰值；在同一环境的短时对比中，无缓冲 `30` 批次优于 `20`、`40`、`50` 和 `512`。这些数据用于选择安全默认值，不是生产 SLA；机器负载、指标集合或结果结构变化后应重新 profile。

年度回测性能排查应使用真实滚动路径和 CPU profile，不能只用单次 `Calculate(300 bars)` 外推。当前已经消除的主要平方级或重复路径包括：整段历史预处理、窗口裁剪后重建基础状态、每根 K 线重建 AI Source、窗口分析排序和临时 numeric/signal series、VFI 的 30 点双遍标准差扫描，以及 Adaptive Supertrend 对历史 ATR 窗口的重复聚类。2026-07 本地 profile 中，VFI 优化后 `standardDeviationRing` 已退出热点列表；Dynamic Swing VWAP 缓存固定 alpha 后累计占比从约 `11.7%` 降至约 `5.0%`。进一步将 swing 极值扫描改为单调队列后，定向 benchmark 从约 `34.6µs/op` 降至约 `10.1µs/op`，约提升 `3.4x`，该函数在同机 CPU profile 中的累计占比从约 `6.5%` 降至约 `2.6%`。3 小时和 7 天真实回测的标准化报告与优化前逐字节一致；墙钟耗时受本机后台负载影响较大，因此不作为本次收益结论。这些数据只用于同机 profile 前后比较，不是生产 SLA。

已否决的实验也应保留结论：将指标并发度固定为 `1` 会降低五周期回放吞吐；把 AI Supertrend 的 `factor -> bar` 循环交换为 `bar -> factor state` 没有稳定 CPU 收益且增加分配；窗口结果 typed 快速路径若只覆盖通用 `*_win_*` 字段会遗漏 pump、SMC、资金流等适配语义，不能局部上线。AI Supertrend 若继续优化，需要随 `CalculationWindow` 推进的真正增量状态和逐 bar batch 对照测试。

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
| `signal:trend_price_progress` | signal | 相对当前趋势方向的价格推进状态：`advancing`、`reversing`、`stalling` 或 `unknown`。 |
| `signal:trend_quality` | signal | 趋势质量。 |
| `signal:supertrend_direction` | signal | Supertrend 方向。 |
| `signal:alphatrend_direction` | signal | AlphaTrend 方向。 |

`advancing` 表示价格沿 `trend_window_bias` 推进，即多头上涨或空头下跌，不表示绝对价格只会上涨。拉盘/砸盘语义会把它与趋势方向组合：`bull + advancing` 生成多头价格推进，`bear + advancing` 生成空头价格推进。

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

## 指标分层建议

当前指标库已经覆盖趋势、动量、波动率、成交量、结构、TradingView 派生脚本和 AI/自适应类指标。后续新增策略时，不建议把所有字段平铺成同等权重，而应按用途分层消费。

在线 market-data 默认保留最近 `310` 根已闭合 K 线，其中 `250` 根作为指标预热上下文，`50` 根用于生成窗口分析所需的底层指标点，`10` 根作为 buffer。新增在线指标应优先控制在 `250` 根预热上下文以内；超过该范围的指标先作为回测或离线研究候选。当前 VFI 默认参数约需要 265 根 K 线，超过默认在线预热上下文，接入实时策略前需要单独评估参数或扩大预热配置。

| 层级 | 定位 | 典型字段 |
| --- | --- | --- |
| 核心层 | 默认参与在线策略判断，用于方向、趋势有效性、基础风险过滤。 | `ema_alignment`、`ma_window_bias`、`macd_window_bias`、`rsi14`、`atr14`、`supertrend_direction`、`volume_ratio20`、`price_volume_confirmation` |
| 确认层 | 用于提高信号质量、过滤假突破、判断衰竭或结构位置。 | `qqe_window_bias`、`vfi_state`、`wvf_zone`、`exhaustion_risk`、`structure_bias`、`liquidity_sweep_type`、`supply_demand_position`、`premium_discount_zone` |
| 实验层 | 用于回测对比和策略研究，默认不应直接成为实盘唯一触发条件。 | `adaptive_supertrend_direction`、`ai_supertrend_direction`、`ai_source_selected`、`ai_source_supertrend_direction`、`momentum_sd_position` |

使用原则：

- 在线策略优先消费核心层和窗口语义字段，例如 `trend_valid`、`trend_quality`、`pump_window_signal`。
- 确认层适合作为加分、减分、过滤条件，不建议独立开仓。
- 实验层先进入回测和观察，不应在没有统计结果前直接提高实盘权重。
- 成本较高或参数较多的指标需要保留 benchmark 数据，避免随着指标数量增长拖慢实时计算。

## 命名约定

- `*_pct`：百分比距离或百分比变化。
- `*_distance_pct`：当前价格相对某条线或某个价位的距离。
- `*_slope*`：斜率或近期变化。
- `*_cross`：交叉信号，常见值为 `golden`、`dead`、`none`。
- `*_direction`：方向，常见值为 `up/down/bull/bear/range/neutral`。
- `*_capability_*`：策略无关的公共能力字段，不得包含开仓、阻断或仓位语义。
- `*_age_bars`：状态持续或事件距今的已闭合 K 线数量。
- `*_confidence`：数据覆盖和独立证据可信度，不代表交易胜率。
- `*_version`：字段计算契约版本；改变既有语义时必须升级。
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

## 指标源码组织约束

`backend/go-service/pkg/indicatorcalc` 按功能职责组织，而不是把新增实验继续堆入少数入口文件：

- `calculator.go` 只负责公共计算 API 和指标编排；数据质量、基础数学、派生字段和值编码分别放在独立模块。
- 指标族入口文件只负责组装输出；数值核心、信号解释、流式状态和高成本算法按真实职责拆分。
- TradingView 派生指标、Supertrend 变体、资金流、动量、均线、波动率和市场结构各自拥有独立实现文件。
- `CalculationWindow` 保留通用窗口生命周期，AI Source 预览和连续状态放在专属模块；在线和回测仍复用同一入口。
- 跨指标使用的纯函数放入公共 helper；只服务单个指标族的函数不要为了表面复用扩大作用域。

新增指标时优先新增内聚文件并接入已有编排入口。禁止把一次性实验参数、报告逻辑或失败方案写入生产指标模块；实验应通过研究命令的插件接口隔离。拆分或优化不得改变字段名称、默认参数、递推顺序和缺失值语义，必须继续通过 compact/batch 等价性测试及 `go test ./...`。

若新增指标需要连续状态，优先把配置加入现有动态状态列表并提供批量实现对照测试；只有无法复用现有状态类型时才增加新状态字段。状态 clone 必须明确区分可安全值复制的标量/固定数组和必须深拷贝的可变切片，测试至少覆盖逐点 batch 等价、clone 独立性和不支持配置的返回语义。

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
