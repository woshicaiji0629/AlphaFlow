# 项目进度

本文档记录 AlphaFlow 当前推进状态、关键架构决策、已知问题和建议下一步。它只记录已经实现或已经明确的方向，不把实验想法写成既定事实。

## 当前主线

当前主线是把以下链路打通：

```text
ClickHouse 历史 K 线 / Redis 恢复缓存 / NATS market snapshot
  -> strategy-engine 内存市场态
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
- Redis 保存实时行情、已收盘窗口特征和当前 K 线实时特征，作为恢复缓存、观测和兼容路径。
- `market-data` 已通过 NATS JetStream 发布已收盘和未收盘 market snapshot，供 `strategy-engine` 运行期更新内存态。
- ClickHouse 保存已闭合 K 线历史。
- 指标计算和窗口分析已下沉到公共包，供实时服务、回测和未来重算复用。
- 指标 runner 已按 K 线预热、指标计算、窗口分析三层拆分：启动先准备连续 K 线，再补齐底层指标 snapshot，最后窗口分析只读取 recent 指标缓存；闭合指标和窗口特征 Redis 写入合并为 pipeline，同一任务补齐的冷启动指标历史也批量写入 Redis，扫描型冷启动任务进入 NATS JetStream 内部队列并由 `market-data` 进程内 worker 消费。
- 指标计算已流式化主要热点路径：`CalculationWindow` 复用 SMA、EMA、RSI、ATR、ADX/DI、MACD、OBV、VWAP、WaveTrend、MoneyFlow、AI Source 和 Adaptive Supertrend 状态；在线和回测稳态都只为新收盘 K 线推进一次状态。
- 指标计算局部热点已继续削减重复工作：`CalculateWindow` 正常路径避免重复解析，Nadaraya-Watson envelope 复用栈上权重缓存，MoneyFlow 区间最高/最低扫描抽成公共 helper；AI Source realtime preview 复用 prefix 并递推单点 ATR，避免每个 tick 重算完整 ATR 序列。
- Collector 已周期输出队列、延迟、丢弃、错误和 K 线 gap 统计；正常关闭时先停止 producer，再最多等待 `10s` 排空已入队 critical event，异常退出后的缺口仍由启动 REST backfill 补偿。
- 最近一次小样本本地指标负载验证使用 `symbols=2`、四个模拟交易所、服务当前周期集合、`lookback=300`、`warmup=250`、`window-lookback=50`、`runs=2` 和 `advance-each-run`，结果中 `tasks=56`、`calculate_window_calls=2856`，等于冷启动 `56 * 50` 加第二轮稳态 `56 * 1`；该结果只验证计算次数收敛，不是生产 SLA。

### 策略框架

- `pkg/strategy` 定义统一策略输入、输出、接口和基础 engine。
- `pkg/strategyframe` 统一在线和回测的指标、窗口、多周期上下文组装与时间边界校验。
- `pkg/strategyspec` 统一策略名称、启停状态和参数配置。
- `pkg/strategyregistry` 提供策略注册和构造入口。
- 当前已注册 `supertrend`。
- `strategy-engine` 支持按配置启用策略集合，在线可以同时运行多个策略。
- `strategy-engine` 启动时从 Redis 恢复特征快照，启动后消费 NATS market snapshot 更新内存市场态。
- `strategy-engine` 会校验 market snapshot 的实时性；行情输入缺失或过期时降级，拒绝新开仓但保留退出路径。
- 回测通常一次只回测一个策略，避免批量结果混杂；后续参数化批量回测应在回测层显式编排。
- 多策略执行已支持错误隔离；单个策略失败不会阻断同批其他策略，失败信息会显式记录和发布。

### 回测引擎

- `backtest-engine` 已具备独立入口和配置。
- 已支持读取多 symbol、多 interval 历史 K 线数据集。
- 已支持按入场周期滚动构造 `strategy.Snapshot`。
- 已新增只读数据完整性检查命令，可检查重复 K 线、缺口、连续区间和可用 warmup。
- 确认周期只使用当时已经闭合的数据，避免未来函数。
- 每个 symbol/interval 维护独立 `CalculationWindow`，按 `CloseTime <= AsOf` 流式推进；大周期只在真正收盘后更新。
- 只保留固定 K 线窗口和最近的指标 snapshot；窗口语义按策略读取惰性分析，并按最新 close time 缓存。
- 长回测支持 context 取消、分批事件提取，以及最长 10 秒一次的速率、elapsed 和 ETA 日志。
- 单 symbol 回测按 Context 即时消费；同一时间戳的多 symbol 仍保持原子批次。模拟执行器保留事件 cursor、累计已实现盈亏和成交计数，避免每个 Context 从头扫描并物化完整策略事件历史。
- 已复用公共策略、仓位管理、paper broker 和 route dispatcher 执行回测。
- 回测仓位使用独立 `bt` scope 和 run id，不写在线 paper 仓位。
- 已生成并持久化策略事件、回测交易明细和 run 级摘要。
- 已支持基础回测报告计算和可选 JSON 文件输出，包括 trade 级权益曲线、逐 K 浮动权益曲线、组合权益曲线、模拟账户资金曲线、最大回撤、胜率、profit factor 和连续亏损统计。
- 回测报告已包含决策诊断：信号分布、按多空拆分的检查通过/阻断/缺失统计、主要阻断原因和入场分数区间，可区分引擎异常、语义阻断和阈值过滤。
- 已新增独立 Supertrend 信号研究命令和 ClickHouse 明细表。研究链路不执行策略仓位，每个原始信号独立观察 12 小时，按固定保证金止损和 ATR 止损分别记录止损前最高止盈阶梯、精确 MFE/MAE、固定止盈首触结果，以及入场时完整底层指标、窗口语义和多周期上下文。
- 回测交易明细可持久化入口模式、所有同时成立的触发来源、入口分数、周期/波动/均线状态，以及 MFE、MAE、极值时间、利润回吐和持仓时长。旧策略实验 Run 已清理，后续基线使用新 Run ID 重新生成。
- 回测策略事件、成交和交易明细使用对应历史 snapshot 的 `AsOf` / `CloseTime`，不再使用零值运行时钟。
- 回测模拟账户已纳入初始资金、保证金占用、手续费、返佣、可用余额检查和账户权益归零爆仓处理。
- 回测 run summary 已优先采用模拟账户最终净值、账户回撤、手续费、返佣和爆仓状态作为账户级报告口径。
- 多 symbol 回测已按 K 线时间线归并执行，同一时间按 symbol 排序保证结果可复现。
- 同一 K 线时间点的多 symbol 批次会先统一刷新价格和账户浮盈亏，再执行该批次信号和订单，并只生成一条账户快照。
- 已新增静态 symbol capability，回测/paper 下单前会按 base/contract 单位、contract size、数量步长和最小名义金额做数量归一化。
- 回测/paper broker 已支持固定 bps 滑点，买入按成交价上浮、卖出按成交价下浮，并可通过 backtest-engine 配置控制。
- 2026-07-10 使用 ETHUSDT、3 小时历史数据和 268 根 warmup 完成真实回测：共生成 60 次决策和 60 个事件，策略均返回 `hold`，无策略异常、无成交，说明执行和持久化链路正常，零成交来自当前样板策略过滤条件。该短样本包含五个周期各自的 warmup，不能直接线性外推年度耗时。
- 已校验 ETHUSDT 2025-07-11 至 2026-07-11 的 3m/5m/10m/15m/30m 数据并完成年度回测。卖侧语义修复前共 357 笔且全部为多头，胜率 `52.38%`、净收益 `-3815.90`、手续费 `4284.49`；诊断显示卖侧趋势和均线通过率均为 `0%`。
- 修复均线发散、趋势距离和 dump 方向语义后，年度回测共产生 685 笔，其中多头 327 笔、空头 358 笔；卖侧趋势通过率为 `3.71%`，均线通过率为 `12.69%`。胜率 `52.12%`、净收益 `-9946.99`、手续费 `8219.94`，账户未触发模拟爆仓，但因可用余额不足停止继续开仓。该结果确认卖侧链路已经解锁，同时表明当前退出与成本模型不具备策略有效性。
- 2026-07-12 使用 ETHUSDT、7 天、3m 入场周期和 5m/10m/15m/30m 确认周期完成性能回放：共读取 8732 根 K 线并生成 3360 个 Context。Context 即时消费和增量模拟执行版本在一次同机对比中由约 132.6 秒降至约 104.6 秒；后续单次运行受机器负载影响可波动到约 133 秒，因此墙钟数据仅作趋势参考，优化判断以 benchmark、CPU profile 和回测结果一致性共同确认。

### 仓位和执行路由

- `position-engine` 已支持 NATS JetStream 长驻消费、dead-letter 和 result-level 幂等。
- paper route 已接入公共 paper handler，支持开仓、平仓、减仓、止盈、止损、移动止损和分批退出。
- paper、testnet 和 live 当前持仓 scanner 已接入，可按最新价格滚动检查退出规则；testnet/live 由策略归因仓位触发退出计划，执行前按账户真实仓位重新校验方向和数量。
- paper 和 backtest 使用本地策略仓位，不依赖交易所账户仓位。
- `pkg/executionadapter` 已提供 Binance、Bitget、Gate、WEEX、Deepcoin、Hotcoin 的统一执行适配器，覆盖账户、仓位、挂单、合约能力、下单、撤单和按客户端订单号恢复。WEEX 支持官方 demo 路由；Deepcoin 和 Hotcoin 官方 API 未提供独立测试网，适配器会拒绝 `testnet` 环境，避免误连实盘。
- `execution-engine` 已支持 `paper` / `testnet` / `live`、环境变量凭证、多账户执行路由、启动连接检查、客户端订单号反查、submitted 意图恢复和私有状态 Redis 当前态。WEEX live 与 Deepcoin live 接入私有 WebSocket，包含重连、重新鉴权/订阅和心跳处理；所有账户同时运行周期 REST 账户、仓位、挂单对账。Hotcoin 官方合约 API 未公开私有 WebSocket，按配置 symbol 使用 REST 对账。
- position-engine 的 testnet/live route 会发布账户无关仓位计划；execution-engine 按账户 `strategies`、`symbols` / `symbol_map`、固定保证金或权益比例、杠杆、最大仓位、最大保证金占用和禁空规则生成独立账户意图。反向信号会先读取各账户真实仓位，已有同向仓位跳过，反向仓位先平仓且不在同批次反手；单账户执行失败不会停止其他账户。
- execution-engine 会发布带成交状态、累计成交量和更新时间的执行回报；position-engine 只用最终成交回报更新 testnet/live 策略归因仓位，中间状态直接确认。意图和回报均按配置的最大投递次数进入独立 dead-letter subject；dead-letter 发布失败时保留原消息重试。
- testnet/live 策略归因仓位 Redis key 已加入策略名，兼容旧 key 的读取、去重和惰性迁移，避免同账户同交易对的多策略仓位互相覆盖。

### 持久化

- ClickHouse `strategy_events` 保存策略事件和模拟成交事件。
- ClickHouse `backtest_trades` 保存由回测成交事件配对生成的交易明细。
- ClickHouse `backtest_run_summary` 保存回测 run 级摘要。
- Redis 继续作为当前活跃状态、恢复缓存和观测缓存层，不作为长期分析存储。
- NATS JetStream 用于服务间通信队列；`ALPHAFLOW_MARKET` 承载 market snapshot，`ALPHAFLOW_STRATEGY` 承载策略决策。服务内自产自销队列由对应服务进程内部约定，不暴露 stream/subject 命名配置。
- Redis Stream 队列已迁移到 NATS JetStream。Redis 只承担缓存和当前态。
- `market-data` 的 ClickHouse pending 重试和异步 K 线 backfill 已使用 NATS JetStream；默认由 `market-data` 进程内 worker 消费，不单独拆服务。
- 本地 Docker Compose 已加入 NATS JetStream，文件存储目录为 `data/nats`。
- 最近一次清空 Redis 和 ClickHouse 后的全链路本地验证已跑通：`market-data` 能重新写入 Redis 缓存和 ClickHouse K 线，NATS strategy bus smoke 通过。
- `strategy-engine` 读取 Redis snapshot 时已接入 health gate，用于启动恢复；运行期 market snapshot 过期、过旧或版本倒退时不会覆盖内存态。
- 已新增 `queue-status` 和 `market-health` 本地观测命令：前者查看 NATS JetStream stream/consumer 积压，后者合并展示 Redis health/cursor、`DECISION_READY` 和队列状态。

## 关键决策

- K 线维护仍是批处理/任务形态，不需要做成长驻在线服务；回测需要的是可重复、可校验、可补数的历史数据。
- 策略代码放在 Go 公共包，在线引擎和回测引擎共用。
- 在线策略引擎可以同时跑多个策略。
- 在线策略引擎运行期主数据源是 NATS market snapshot 和内存市场态；Redis 特征 hash 是启动恢复、故障恢复、观测和兼容缓存。
- 离线回测一般一次只回测一个策略；批量回测应显式生成多个 run。
- 在线与回测共享 `CalculationWindow`、连续指标状态、窗口语义和 `strategyframe` 上下文协议；生命周期不同，但不再维护回测专用批量指标公式。
- 上线或下线策略优先通过策略 registry 和配置控制，不在多个服务里分别硬编码。
- 策略算法由 Go 代码实现并注册；后台只维护参数、版本、可见范围和发布状态，不执行任意代码。
- 已发布策略版本不可修改，新配置从已发布版本复制为独立草稿。
- 回测仓位应独立于在线 paper 仓位，使用 `bt` scope 和 run id 隔离。
- `paper` / `bt` 是本地策略仓位；`testnet` / `live` 使用交易所账户真实仓位约束订单，并通过内部账本维护策略归因。

## 已知问题

- 回测还没有图表报告和结果查询 API。
- 回测还没有参数化批量运行和策略参数配置入口。
- 2025-08 至 2025-11 的 ETHUSDT 3m 训练研究获得 1,550 个 `supertrend_flip`。原始信号的全部止盈止损组合均为负；训练区间扫描得到的 `sr_position == mid AND wavetrend_momentum == weakening` 候选在多个 300% 止盈组合中转正，但这是训练结果。
- 冻结上述候选后，2026-03 至 2026-06 的独立区间获得 1,518 个 `supertrend_flip` 和 101 个候选信号。固定 70% / 300% 的平均净收益为 `-21.58%`、Profit Factor `0.658`，2.5 ATR / 300% 为 `-9.87%`、Profit Factor `0.798`；候选未通过样本外验证，应判定为训练区间拟合，不得进入正式策略。
- `wick_reclaim` 当前无法产生研究信号：策略语义读取窗口 `high` / `low`，但底层指标快照目前只写入 `close`。在补齐字段并重新生成研究数据前，现有 3,068 个信号只能代表 `supertrend_flip`，不能称为全部 Supertrend 触发。
- 回测爆仓当前按账户权益归零处理，还没有接入交易所维持保证金、标记价格和阶梯强平公式。
- 回测滑点当前是固定 bps 模型，还没有按盘口深度、成交量、波动率或订单大小动态估算。
- position-engine 还没有独立 `backtest` / `notify` handler；testnet/live 已通过账户无关计划接入 execution-engine。
- 真实执行代码已装配，但尚未使用用户真实凭证完成交易所端到端联调；不能把 mock 测试视为实盘验收。
- 交易所 symbol capability 目前来自静态配置，尚未接交易所 API 自动同步。
- 执行意图恢复和成交回报幂等已落 Redis；成交应用与幂等标记仍不是单个原子事务，进程在两步之间异常时需要依赖重复投递恢复，后续应补事务化边界。
- 账户级实时风控尚未实现。
- HTTP 健康检查接口尚未实现。
- Control API 和前端基础链路已实现；账户挂载、订阅、订单、用户管理和审计查询页面仍待补齐。
- ClickHouse 表当前通过 `CREATE TABLE IF NOT EXISTS` 初始化，后续字段变更需要单独迁移策略。
- top500 场景下的 Redis ops、NATS market snapshot 积压、NATS strategy decision 积压、ClickHouse 写入和实时采集延迟仍需要长时间全链路压测确认；不能把最近一次小样本压测结果当作生产 SLA。
- 冷启动 snapshot 数量仍随 `tasks * window-lookback` 增长，但同一任务的指标历史已合并为一次批量 Redis pipeline；top500 长跑后再根据 Redis ops、payload 大小和队列积压判断是否需要异步刷 Redis 或降低写入频率。
- Redis 指标写入当前仍保留同步路径；已收盘异步刷 Redis、未收盘定期刷 Redis 还未实现。
- `market-health` 当前主要观测 Redis health/cursor 和队列状态，还没有完整覆盖 `strategy-engine` 内存市场态和 market snapshot 消费延迟。

## 建议下一步

1. 先补齐指标快照中的 `high` / `low` 并验证 `wick_reclaim` 语义，再将 flip 与 reclaim 分开生成研究数据，不能混成单一信号总体。
2. 统一研究查询口径：指标发现使用“每个信号 × 每种止损一行”的最高止盈潜力标签；固定 TP/SL 表只用于验证可执行退出，不再混用累计命中和互斥最高档分组。
3. 停止沿用未通过 OOS 的 `sr_position == mid AND wavetrend_momentum == weakening` 候选。下一轮重新划分训练与冻结验证区间，并限制预先声明的指标集合和组合数量。
4. 固定入口与过滤器后，再独立比较现有自适应退出和更简单的止损/止盈方案，避免入口与退出同时变化。
5. 只有 3m 新基线成立后，才接入 1m 精确执行；1m 只在 3m 已产生进出场意图时读取，不参与策略方向判断。
6. 做一次 top500 长时间全链路压测，观察 Redis、NATS market snapshot、NATS strategy decision、ClickHouse、market-data 和策略链路积压；期间用 `make queue-status` 和 `make market-health` 辅助判断队列和可决策状态。
7. 补回测图表报告、结果查询入口和参数化批量运行。
8. 实现 position-engine 的 notify handler。
9. 增加交易所 symbol capability 自动同步和缓存。
10. 明确过期策略反向退出但无 exit rule 时的 action 协议。
11. 使用真实 demo/testnet 凭证完成 execution-engine 端到端验收，再使用最小订单完成 live 安全验收。

## 验证状态

最近一轮 Go 全量测试已通过：

```sh
GOCACHE=/private/tmp/alphaflow-go-cache GO111MODULE=on go test ./...
```

最近一轮还通过了 `go vet ./...` 和执行链关键包的 race 测试。本地 Redis/NATS 集成验证覆盖了旧仓位 key 迁移、部分成交回报去重、意图/回报 dead-letter、异常 JSON，以及实际 position-engine 消费最终成交回报并写入策略归因仓位。该验证未连接交易所或发送真实订单，不能替代 demo/testnet 和 live 验收。

最近几轮更新包含 NATS JetStream 队列替换、指标流式计算优化、market snapshot bus、strategy-engine 内存市场态、在线/回测统一策略上下文、多策略错误隔离、回测数据完整性检查、多周期流式回放、交易级诊断和独立信号研究。Supertrend flip 已完成一轮训练与冻结样本外验证，首个指标组合未通过 OOS；下一步先修复 wick reclaim 数据基础和研究口径，再开展新的有限假设实验。
