# Supertrend 策略

## 定位

Supertrend 是策略方向和交易信号的唯一来源。其他指标只作为背景、过滤器和诊断信息，不能独立产生方向、开仓或策略反转。

策略当前仍处于研究阶段，未经新语义年度回测和样本外验证，不得进入实盘。

## 周期职责

- `3m`：生成 Supertrend 信号并执行策略判断。
- `5m`、`10m`、`15m`、`30m`：提供趋势背景和风险过滤，不产生交易信号。
- `1m`：当前尚未接入；未来只允许在已有 3m 决策后优化执行，不参与策略判断。

## 开仓信号

3m 必须出现以下至少一种 Supertrend 事件：

1. `supertrend_flip`：Supertrend 方向刚翻转。
2. `wick_reclaim`：上一根 K 线刺破 Supertrend 后重新收回原趋势方向。

没有 Supertrend 事件时，无论 SMC、趋势事件、均线交叉、MACD、成交量或 pump/dump 信号如何，都不能开仓。

## 辅助过滤

Supertrend 给出方向后，策略继续检查：

- 5m 回调是否解决。
- 10m、15m、30m 是否处于允许的趋势背景。
- 趋势反转风险、均线结构和 MACD 是否明确反向。
- 价量背离、成交量状态和动能证据。
- 假信号风险和阻断周期数量。

这些条件可以阻断或降低信号分数，但不能补出缺失的 Supertrend 信号，也不能改变 Supertrend 方向。

## 出场

- 固定止损、跟踪止损和保盈规则属于风险执行规则，可以按价格触发。
- 策略反向退出必须重新通过 3m Supertrend 事件门。
- 10m 与 15m 同时确认反向时，可以触发结构失效退出。
- `adaptive` 退出仍为独立实验模式，不代表已验证收益。

策略始终全仓进、全仓出，不做分批建仓或部分止盈。

## 已删除的实验入口

以下历史实验已经从策略和 registry 删除：

- `intraday_adaptive`
- `intraday_impulse`
- `intraday_event`
- `shock_breakout` 独立入口
- SMC BOS、趋势事件和均线交叉的独立触发权

pump/dump、趋势、均线、MACD、SMC 等派生信息仍可由指标层提供，但在本策略中不能独立决策。

## 验证

```sh
cd backend/go-service
GOCACHE=/private/tmp/alphaflow-go-cache GO111MODULE=on go test ./pkg/strategies/supertrend ./pkg/strategyregistry
GOCACHE=/private/tmp/alphaflow-go-cache GO111MODULE=on go test ./...
```

新的收益基线必须重新回测建立。旧实验 Run 和临时报告已清理，不能继续引用旧收益作为当前策略结论。
