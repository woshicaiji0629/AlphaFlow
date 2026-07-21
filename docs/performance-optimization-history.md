# 性能优化记录

本文档存档 2026-07 对 AlphaFlow 指标、行情和回测热路径进行的集中优化。它记录稳定的设计决策、正确性边界、有效与无效实验、基准结果以及后续排查入口；指标字段定义和业务语义仍以[指标文档](indicators.md)为准。

## 优化目标与边界

本轮目标是降低完整指标集合在在线计算和历史回放中的 CPU、分配和常驻内存，同时保持线上与回测使用同一套指标和策略实现。

必须保持的约束：

- 一次年度回测按一个 symbol 处理；周期内必须保持时间顺序，不通过同周期乱序并发换取吞吐。
- 不裁剪指标计算字段。新策略可以继续读取现有任意数值和信号字段。
- 不改变已闭合 K 线、确认周期收盘和 `AsOf` 语义，不能引入未来数据。
- 不改变 EMA、RSI、ATR、MACD、AI Source 和 Adaptive Supertrend 等连续状态的递推语义。
- 数值零值与字段缺失必须可区分，旧字符串输入和新数值输入必须等价。
- 旧 view 不能因为 schema 后续发现新字段而读取到未来字段。
- 对外 Getter 继续返回完整 `NumericSeries` 和 `SignalSeries`，策略不感知底层紧凑存储。
- 性能优化不能依赖整数窄化、忽略错误或静默降级。

## 最终数据路径

```text
ClickHouse K 线
  -> CalculationWindow 连续指标状态
  -> CalculateWindowNumeric 完整数值/信号字段
  -> OrderedAnalyzer 固定长度滚动槽
  -> NumericWindow / SignalWindow 分组结果
  -> WindowViewBuilder 稳定 schema + 紧凑数组
  -> strategy.Context
  -> 同一份 Go Strategy
```

在线 `market-data` 和回测继续共享 `CalculationWindow` 与指标实现。回测 producer 按 interval 顺序生成指标和窗口 view，consumer 只在对应 K 线真正闭合后推进状态。

## 已完成的优化

### 1. 指标计算热路径

- 回测使用 `CalculateWindowNumeric`，避免把约 278 个数值字段先格式化为字符串、再由窗口层解析回 `float64`。
- `CalculationWindow` 持有基础指标和高成本指标的连续状态；固定窗口只保留所需 K 线，递归状态不会因窗口裁剪重建。
- DEMA 和 TEMA 共享同一组流式 EMA 递推，HMA slope 复用本轮已经得到的 HMA。
- QQE Mod 与增强 QQE 共享 RSI smoothing 和 delta foundation，避免同一窗口重复生成基础序列。
- 完整 `CalculateWindow` 只在 `ValueSet` 中构建一次数值 map，旧字符串 map 在最终输出边界统一编码，不再预分配一张始终为空的中间 map。
- 增强 QQE 直接从已有 smoothing 序列读取末值，并在 primary trend 尾部窗口上计算 histogram 均值和标准差，不再复制完整 primary line 和 histogram；参考实现测试保证浮点结果逐字段精确一致。
- 趋势特征优先读取 `featureContext` 中已经计算的 EMA；Supertrend/AlphaTrend 内部方向改为紧凑枚举，只在输出边界转换为字符串。
- VFI、Dynamic Swing VWAP、Bollinger、Donchian、Nadaraya-Watson、ATR 和多种趋势指标移除了已确认的重复扫描或完整临时序列。

主要实现位于：

- [`pkg/indicatorcalc`](../backend/go-service/pkg/indicatorcalc)
- [`docs/indicators.md`](indicators.md)

### 2. 指标模块职责拆分

2026-07 的热路径优化完成后，`pkg/indicatorcalc` 同步完成按功能域拆分。该调整不是以文件行数为目标，而是把频繁变化的实验实现与稳定编排边界隔离：

- AI Source 分为生命周期、特征缓存、学习评分和辅助状态。
- 均线、动量、资金流、波动率、Smart Money 与 TradingView 指标按指标族拆分。
- `CalculationWindow` 的 AI Source 预览生命周期独立于通用窗口维护。
- AI Supertrend 的性能聚类独立于趋势推进和因子表现计算。
- 公共交叉、极值、数值编码和基础指标计算不再依附于任意单一实验文件。

模块迁移保持公式、阈值、调用顺序和输出字段不变。每一批迁移均通过指标、market-data、回测模拟器定向测试和全项目测试。后续性能实验不得把临时实现重新堆回编排入口；只有能够证明等价且具备稳定收益的实现才能进入共享核心。

### 3. 指标窗口分析

`OrderedAnalyzer` 取代每个决策点重新枚举、排序和构造窗口 map 的路径：

- 数值和信号 key 只在首次出现时加入稳定 schema；只有发现新 key 时才重新排序。
- 每个字段使用 `DefaultLookback` 大小的滚动槽和 generation 标记，不再复制完整 snapshot 窗口。
- `AppendTypedInto` 复用结果 map；`clear` 后保留容量，避免每根 K 线重新分配。
- `AppendDenseInto` 把通用滚动字段直接输出为 `NumericWindow` 和 `SignalWindow` 切片；pump、SMC、资金流等适配语义字段仍保留在兼容 map 中，不丢字段。
- 动态新增字段、字符串数值 fallback 和非数值信号仍按原规则发现和分类。

主要实现位于：

- [`pkg/indicatorwindow/analyzer.go`](../backend/go-service/pkg/indicatorwindow/analyzer.go)
- [`pkg/indicatorwindow/model.go`](../backend/go-service/pkg/indicatorwindow/model.go)

### 4. market-data 调度与缓存

- 派生周期聚合从“每个目标窗口查询一次并单条写入”改为“一次范围查询、内存分组、批量写入”。
- health 扫描构造 job 后使用有界 worker 执行；默认 worker 数跟随 `GOMAXPROCS`，错误仍显式汇总返回。
- indicator runner 的窗口缓存拆成 64 个 shard，降低多 symbol/interval 计算时对单一全局锁的竞争。
- 缓存更新继续保留 clone 隔离、缺口检测和全量重载 fallback，不用共享可变窗口换取性能。

主要实现位于：

- [`market-data/internal/aggregator`](../backend/go-service/market-data/internal/aggregator)
- [`market-data/internal/health`](../backend/go-service/market-data/internal/health)
- [`market-data/internal/indicator`](../backend/go-service/market-data/internal/indicator)

### 5. 回测读取与回放流水线

- 数据集读取使用最多 4 个 worker 并发读取 symbol/interval series，结果仍按请求顺序写入固定位置。
- 指标 producer 使用无缓冲 batch channel 提供背压；默认 `indicator_batch_size = 30`，所有 interval 共享并发上限。
- producer 顺序完成底层指标、窗口分析和 view 构建，prepared batch 直接携带不可变 indicator/window；consumer 不再重复维护窗口 snapshot 或临时分析 map。
- consumer 只在 `CloseTime <= AsOf` 时发布 prepared 状态，确认周期不会提前进入策略上下文。
- context 取消、读取错误、指标错误和窗口错误继续显式返回，worker/channel 都有确定退出路径。

主要实现位于：

- [`backtest-engine/internal/reader/reader.go`](../backend/go-service/backtest-engine/internal/reader/reader.go)
- [`backtest-engine/internal/simulator/snapshot.go`](../backend/go-service/backtest-engine/internal/simulator/snapshot.go)

### 6. 稳定 schema 与紧凑数组

策略 view 使用 append-only schema 把字段名映射到稳定下标：

- presence 位图区分合法零值和缺失字段。
- `NumericSeries` 的回放存储从包含常驻 `Direction string` 的 96-byte 结构改为 80-byte `DenseNumericSeries`。
- Direction 只在旧兼容数值字段真实包含方向时分配旁路字符串数组；正常 grouped 路径把方向作为 signal 存储。
- signal 计数继续使用完整 `int`。`DenseSignalSeries` 是 `SignalSeries` 的别名，避免为追求表面压缩而引入整数截断或转换复制。
- `WindowViewBuilder` 的 scratch 本身就是紧凑结构，生成 view 时执行整结构赋值。
- `Numeric`、`Signal` 和 `Empty` Getter 屏蔽底层 map/array 差异；Supertrend 策略只通过 Getter 读取窗口。
- 在线 `strategy-engine` 按 exchange/market/symbol/interval key 复用 `WindowViewBuilder`，收到 market snapshot 时直接生成紧凑窗口 view，不再把字符串窗口 map 长期保留在 market state 中。
- `WindowViewBuilder` 的 scratch 不是并发安全结构；在线路径对同一 key 串行使用 builder，不同 key 仍可并发处理，避免为复用 scratch 引入数据竞争。

主要实现位于：

- [`pkg/strategy/model.go`](../backend/go-service/pkg/strategy/model.go)
- [`pkg/strategyframe/view.go`](../backend/go-service/pkg/strategyframe/view.go)
- [`pkg/strategies/supertrend/supertrend.go`](../backend/go-service/pkg/strategies/supertrend/supertrend.go)

## 被否决或回滚的实验

以下实验没有进入最终实现，结论应保留，避免重复试错：

| 实验 | 结果 | 处理 |
| --- | --- | --- |
| 固定 `indicator_concurrency = 1` | 五周期回放吞吐下降 | 保留共享有界并发，`0` 跟随 `GOMAXPROCS` |
| AI Supertrend 交换 factor/bar 循环 | 没有稳定 CPU 收益，并增加分配 | 回滚；后续只能做真正的逐 bar 增量状态 |
| typed 快速路径只覆盖通用 `*_win_*` | 会遗漏 pump、SMC、资金流等语义字段 | 改为 grouped 通用字段 + 兼容语义 map |
| 每次生成 view 时逐字段转换到紧凑结构 | 分配下降但 CPU 明显退化，grouped 约从 7–8µs 升到 12.5–14µs | 改为紧凑 scratch + 整结构复制 |
| 把窗口计数从 `int` 压成 `int32` | 兼容字符串入口理论上可能截断 | 放弃窄化，完整保留 `int` 语义 |
| 通过裁剪指标字段降低内存 | 新策略可能依赖被裁剪字段 | 明确禁止；改为优化表示和生命周期 |

## 基准与年度回测记录

本地环境为 Intel i7-8850H，结果只用于同机趋势比较，不是生产 SLA。后台进程、ClickHouse/OrbStack、Spotlight 和温度都会显著影响墙钟时间。

### 窗口与 view 微基准

| 路径 | 优化前 | 优化后 | 分配 |
| --- | ---: | ---: | ---: |
| wide analyzer，300 numeric + 100 signal | 344–402µs/op | 220–253µs/op | 17,342 B/op，21 allocs/op，保持不变 |
| typed WindowViewBuilder | 16.6–18.5µs/op | 14.0–15.7µs/op | 15,384 -> 13,336 B/op |
| grouped WindowViewBuilder | 7–8µs/op | 6.45–7.30µs/op | 15,384 -> 13,336 B/op |

紧凑 view 的分配下降约 13.3%，分配次数保持 4 allocs/op。微基准必须和完整年度回测一起看，不能用单函数结果直接外推端到端吞吐。

### 指标计算微基准

| 路径 | 优化前 | 优化后 | 确定收益 |
| --- | ---: | ---: | ---: |
| 完整 `CalculateWindow` | 约 106.9 KiB/op，431 allocs/op | 约 84.2 KiB/op，425 allocs/op | 分配字节下降约 21.2% |
| QQE shared foundation | 16,896 B/op，8 allocs/op | 12,800 B/op，6 allocs/op | 分配字节下降约 24.2% |

2026-07-20 对 `CalculationWindow` 连续状态和 realtime preview 做了进一步收口：

- AlphaTrend、Dynamic Swing VWAP、VFI、Heikin Ashi、ATR22/Chandelier 以及多种 TradingView 指标改为复用流式末值状态。
- EMA 历史、DEMA/TEMA21、volume MA 和派生成交量分桶不再重复构造或扫描。
- Realtime preview 的 K 线列表改为 closed base + 单根 overlay 惰性物化；OHLCV 和可变指标历史继续克隆隔离。
- EMA、SMA、volume SMA 和 MACD 的配置槽位最终采用动态状态切片。固定数组版本最低达到 `44 allocs/op`，但需要同步维护周期、下标和 switch；动态版本以约 4 次小分配换取单一配置来源，最终为约 `48 allocs/op`。

同机窗口级结果：

| 路径 | Git HEAD 基线 | 当前版本 | 结果 |
| --- | ---: | ---: | ---: |
| Realtime Preview | 166,530 ns/op | 135,180 ns/op | CPU 约提升 18.8% |
| Preview 分配字节 | 224,850 B/op | 144,656 B/op | 下降约 35.7% |
| Preview 分配次数 | 71 allocs/op | 48 allocs/op | 下降约 32.4% |

按 `3m * 365天 = 175,200` 次迭代执行合成年度负载时，纯数值 streaming 从 `683,597 ns/op`、`70,719 B/op`、`145 allocs/op` 降到 `224,643 ns/op`、`43,582 B/op`、`129 allocs/op`。当前版本输出 287 个 numeric values，Git HEAD 输出 283 个，因此这不是完全相同输出集合的单函数对比；当前功能更多但仍显示约 3.04 倍指标吞吐。它也不是完整回测结果，不包含 ClickHouse、策略执行、事件持久化和报告生成。

完整指标结果继续同时保留字符串和数值表示：在线存储、发布协议需要字符串字段，窗口和策略内部优先消费数值字段。若要继续消除其中一张结果 map，需要先调整存储和消息协议边界，不能仅在计算器内部删除。CPU 和 market-data 冷启动墙钟结果在本机波动较大，本轮只把分配下降和输出一致性作为确定结论。

### market-data 冷启动采样

`market-data-indicator-loadtest` 使用四个模拟交易所和服务当前周期集合，不连接真实 Redis、ClickHouse 或交易所。每交易所 100 个 symbol 时共有 2800 个任务，`lookback=268`、`warmup=268`、`runs=1` 的交错采样如下：

| 版本 | 三次 cold elapsed | 中位数 | 中位吞吐 |
| --- | --- | ---: | ---: |
| Git HEAD | 21.51s / 23.44s / 21.82s | 21.82s | 约 128.33 tasks/s |
| 当前版本 | 13.26s / 18.21s / 16.28s | 16.28s | 约 172.04 tasks/s |

该规模下当前版本中位耗时约降低 25.4%，吞吐约提升 34.1%。每交易所 500 个 symbol 时共有 14000 个 symbol/interval 任务；当前版本实跑超过 4m38s 仍未结束，RSS 峰值观测约 3GB，CPU 约 220%–310%。该运行按时间盒主动终止，因此不能声称 top500 完成时间，也没有同规模 Git HEAD 结果。结论只包括：中等规模冷启动有收益；top500 已受窗口、snapshot 常驻量和 GC 非线性影响，不能按 100-symbol 样本线性外推。

冷启动比较应先交错运行至少三次中等规模样本，再对 top500 使用固定时间盒并记录已完成任务、RSS、heap/alloc 和 GC。无时间限制等待一次全量墙钟，或让两个版本并行竞争资源，都不能得到可靠结论。

### ETHUSDT 一年回测

固定配置：

- symbol：`ETHUSDT`
- 区间：`2025-07-11T00:00:00Z` 至 `2026-07-11T00:00:00Z`
- 主周期：`3m`
- 确认周期：`5m`、`10m`、`15m`、`30m`
- warmup：268 bars
- batch size：30，无缓冲 channel
- 完整指标字段，不做字段裁剪

| 阶段 | 主循环耗时 | 吞吐 | 说明 |
| --- | ---: | ---: | --- |
| 早期完整路径 | 约 12m43s | — | 早期本地观测/外推，用于确认优化量级 |
| grouped window 阶段 | 4m35.61s | 635.68 contexts/s | 175,200 contexts 完成 |
| 紧凑数组最终验证 | 6m10.53s | 472.83 contexts/s | 端到端 6m31.10s；运行时存在明显 OrbStack 与 Spotlight CPU 竞争，不能据此判定代码退化 |

最终验证报告：

- status：`completed`
- contexts / decisions / results / events：均为 175,200
- failures：0
- trades：0
- final equity：10,000
- 报告 JSON：约 103 MiB

完整年度结果证明紧凑数组没有改变当前策略行为。最后一次运行约 44% 时 RSS 为 860 MiB，与前一次同进度约 855 MiB 接近；由于没有在相同系统负载下获取双方峰值，不能把这组 RSS 当作内存收益结论。

## 正确性验证

本轮必须持续通过以下检查：

```sh
cd backend/go-service

go test ./pkg/strategy ./pkg/strategyframe \
  ./pkg/strategies/supertrend ./backtest-engine/internal/simulator

go test -race ./pkg/strategy ./pkg/strategyframe \
  ./pkg/strategies/supertrend ./backtest-engine/internal/simulator

go test ./...
```

关键等价性测试覆盖：

- 字符串、typed 和 grouped 三种窗口输入。
- NumericSeries/SignalSeries 全部公开字段。
- 合法零值与缺失字段。
- 动态新增和同数量字段替换。
- schema 扩展后历史 view 不暴露未来字段。
- 在线 market state 使用按 key 复用的 builder 后仍保持最新消息顺序，并通过 race 测试覆盖同一 key 并发更新。
- 增强 QQE 紧凑路径与原始临时序列路径在多种窗口长度下逐字段精确一致。
- ordered analyzer 与旧批量 analyzer 输出等价。
- context 取消、worker 退出和数据读取错误。
- 3 小时、7 天标准化报告与优化前逐字节一致。

## 复现命令

窗口基准：

```sh
cd backend/go-service

go test ./pkg/indicatorwindow -run '^$' \
  -bench 'BenchmarkOrderedAnalyzer(Dense|Typed)IntoWideWindow20$' \
  -benchmem -count=5

go test ./pkg/strategyframe -run '^$' \
  -bench 'BenchmarkWindowViewBuilderFrom(Grouped|Typed)Result$' \
  -benchmem -count=5

go test ./pkg/indicatorcalc -run '^$' \
  -bench '^(BenchmarkQQESharedFoundation|BenchmarkCalculateWindowStreaming)$' \
  -benchmem -count=5

go run ./market-data/cmd/market-data-indicator-loadtest \
  -symbols 2 -lookback 310 -warmup 250 \
  -window-lookback 50 -snapshot-cache-limit 60 \
  -runs 2 -advance-each-run
```

年度数据检查和回测：

```sh
cd backend/go-service

go run ./backtest-engine/cmd/backtest-engine dataset-check \
  -config configs/backtest-engine.ethusdt-1y.toml

go run ./backtest-engine/cmd/backtest-engine run \
  -config configs/backtest-engine.ethusdt-1y.toml
```

当前仓库中上述配置名虽然包含 `1y`，但 `[data]` 区间实际为 `2025-09-01` 至 `2025-12-01`。完整年度验证必须显式设置连续 365 天区间；不能根据文件名宣称已经完成一年回测。

年度性能比较必须尽量保持相同系统负载，并分别记录主循环耗时、端到端耗时、吞吐、RSS/heap 和 profile。一次受后台索引影响的墙钟结果不能单独作为性能回退依据。

## 下一步优化方向

当前最优先的工作是采集年度路径 40%–50% 进度时的 CPU、heap 和 allocs profile，而不是继续猜测热点。

根据现有证据，下一轮应重点区分：

1. `DenseValues` 或窗口 view 是否仍是主要保留对象。
2. 175,200 个 contexts/events/results 是否存在重复长期驻留。
3. 约 103 MiB 报告构建和结果持久化是否可以流式化或分批落盘。
4. GC 是否已经超过指标计算本身成为主耗时。
5. market-data 冷启动应在代表性 symbol 数量下单独采集 CPU/alloc profile；当前小样本 loadtest 主要验证计算次数和缓存路径，不作为吞吐结论。

这里的第 2、3 项是根据报告体积和常驻内存增长做出的待验证推断，不是已确认瓶颈。后续仍需遵守“不裁剪指标字段”和“结果语义不变”的约束。
