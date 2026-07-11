# Polymarket 事件合约研究

`backend/go-service/polymarket-research` 是独立的 Go 研究采集服务，当前聚焦加密货币涨跌事件合约。它不负责 Polymarket 下单，也不进入 AlphaFlow 的在线策略执行链路。

## 研究范围

默认配置覆盖：

- 币种：BTC、ETH、SOL、XRP、DOGE、BNB、HYPE。
- 周期：5 分钟和 15 分钟。

实际市场覆盖取决于 Polymarket Gamma 返回的活跃事件。配置可增加币种或周期，但只有命名规则能够被当前解析器识别的市场才会进入采集范围。

## 数据源与数据流

```text
Gamma API
  -> 市场发现、时间范围、token、状态和元数据

CLOB Market WebSocket
  -> best bid/ask、spread、最新成交、结算事件

RTDS WebSocket
  -> BTC、ETH、SOL、XRP 参考价格

以上数据
  -> 内存批处理
  -> ClickHouse
  -> polymarket-research-report
```

Gamma 默认每 15 秒同步一次市场。CLOB 只在目标 token 集合实际变化时更新订阅，避免周期性无效重连。ClickHouse 写入失败的批次会保留在内存中继续重试。

## ClickHouse 表

| 表 | 内容 |
| --- | --- |
| `polymarket_markets` | Gamma 市场、condition、event、币种、周期、起止时间、token 和状态 |
| `polymarket_book_ticks` | 每个 token 的 best bid、best ask 和 spread |
| `polymarket_trades` | CLOB 最新成交事件、方向、价格、数量和费率字段 |
| `polymarket_reference_prices` | RTDS 币种参考价格 |
| `polymarket_resolutions` | 结算时间、获胜 token 和标准化方向 |

Resolution 收到后通过获胜 token 映射为 Gamma market ID。研究查询使用 token ID 判断胜负，不依赖 `Yes/No` 或 `Up/Down` 的文本大小写。

## 配置

本地配置位于：

```text
backend/go-service/configs/polymarket-research.local.toml
```

主要配置段：

- `gamma`：Gamma API 地址、轮询间隔和分页大小。
- `research`：目标币种和周期。
- `realtime`：CLOB、RTDS WebSocket 地址及重连等待时间。
- `batch`：批量大小、刷新周期、队列容量和健康日志周期。
- `clickhouse`：连接地址、数据库、认证和超时。

凭证不要提交到仓库。生产或共享环境应通过项目既有的安全配置方式注入。

## 启动采集

先启动 ClickHouse：

```sh
make infra-up
```

然后启动采集器：

```sh
cd backend/go-service
go run ./polymarket-research/cmd/polymarket-research \
  -config configs/polymarket-research.local.toml
```

启动时服务会初始化 ClickHouse 表、执行一次 Gamma 市场发现，然后启动 Gamma 轮询、CLOB 和 RTDS 实时连接。

## 运行研究报表

```sh
cd backend/go-service
go run ./polymarket-research/cmd/polymarket-research-report \
  -config configs/polymarket-research.local.toml \
  -start 202607110000 \
  -end 202607120000 \
  -entry-seconds 300
```

参数说明：

- `start`、`end` 使用 `YYYYMMDDHHmm`，查询范围为左闭右开。
- `entry-seconds` 表示目标入场点距离市场到期的秒数；例如 `300` 表示到期前 5 分钟。
- 查询会选择目标入场点之前最近的一条盘口，不使用到期后的数据。
- Up 和 Down 分开汇总，避免把同时买入两边误认为方向策略。

输出字段：

| 字段 | 含义 |
| --- | --- |
| `symbol` / `duration` | 币种和事件周期 |
| `outcome` | up 或 down |
| `seconds_to_expiry` | 本次研究指定的到期前秒数 |
| `samples` / `wins` / `win_rate` | 样本数、获胜数和胜率 |
| `avg_entry` / `avg_spread` | 平均 ask 入场价和平均价差 |
| `gross_pnl` | 每份合约按二元结算计算的毛收益 |

可以分别运行 `60`、`300`、`600`、`1800` 等入场点，再比较币种、周期和方向的统计差异。

## 数据质量与限制

- 采集器只能记录启动之后收到的实时盘口和成交，历史 Gamma 市场元数据不等于完整历史盘口。
- 旧数据如果使用过早期错误的 resolution market ID，不会自动迁移，需要重新采集或单独修复。
- 报表只统计已经存在 resolution 且目标时间点之前存在盘口的市场；没有输出不一定代表程序错误。
- `gross_pnl` 尚未扣除手续费、滑点和深度影响，不代表真实可成交收益。
- 当前报表是描述性研究，不包含方向信号或自动交易决策。
- ClickHouse 短暂不可用时失败批次会继续重试；长时间不可用会增加进程内存积压，目前没有本地 WAL。
- RTDS 当前只标准化 BTC、ETH、SOL、XRP 参考价格；其他币种仍可采集 Polymarket 市场和盘口，但不一定有对应参考价格序列。

## 常见排查

报表只有表头时，依次检查：

1. 查询时间范围内是否存在 `polymarket_markets`。
2. 对应市场是否存在 `polymarket_book_ticks`。
3. 是否已经收到并写入 `polymarket_resolutions`。
4. 目标 `entry-seconds` 之前是否已有盘口。
5. market ID 与 resolution market ID 是否一致。

运行模块测试：

```sh
cd backend/go-service
go test ./polymarket-research/...
```

实现入口：

- `polymarket-research/internal/gamma`：市场发现。
- `polymarket-research/internal/clob`：CLOB 盘口、成交和结算。
- `polymarket-research/internal/rtds`：参考价格。
- `polymarket-research/internal/store`：ClickHouse 与批量写入。
- `polymarket-research/internal/research`：研究查询和汇总。
