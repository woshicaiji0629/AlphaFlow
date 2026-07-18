package strategyregistry

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
	"alphaflow/go-service/pkg/strategyspec"
)

type Definition struct {
	Code       string                         `json:"code"`
	Parameters map[string]ParameterDefinition `json:"parameters"`
}

type ParameterDefinition struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func Supported() []Definition {
	items := []Definition{{Code: StrategySupertrend, Parameters: map[string]ParameterDefinition{
		"entry_threshold":             {Type: "number", Description: "开仓置信度阈值，范围(0,1]"},
		"max_blocking_timeframes":     {Type: "integer", Description: "允许阻塞的确认周期数量，必须为正整数"},
		"min_take_profit_bps":         {Type: "number", Description: "最小止盈距离（基点），0 表示禁用"},
		"take_profit_cost_floor_bps":  {Type: "number", Description: "结构止盈覆盖往返成本后的最小距离（基点），0 表示禁用"},
		"min_reward_risk_ratio":       {Type: "number", Description: "最小预期盈亏比，0 表示禁用"},
		"max_stop_loss_bps":           {Type: "number", Description: "最大固定止损距离（基点），0 表示禁用"},
		"exit_mode":                   {Type: "string", Description: "全仓出场模式：structure、trailing 或 adaptive"},
		"trailing_stop_pct":           {Type: "number", Description: "全仓跟踪止损距离（百分比），仅 trailing 模式使用"},
		"profit_guard_activation_bps": {Type: "number", Description: "保盈保护激活距离（基点），仅 trailing 模式使用"},
		"profit_guard_floor_bps":      {Type: "number", Description: "保盈保护最低利润（基点），仅 trailing 模式使用"},
		"profit_decay_activation_bps": {Type: "number", Description: "指标衰减退出激活距离（基点），仅 trailing 模式使用"},
		"round_trip_cost_bps":         {Type: "number", Description: "预估往返手续费与滑点（基点），仅 adaptive 模式使用"},
		"profit_buffer_bps":           {Type: "number", Description: "覆盖交易成本后的保盈缓冲（基点），仅 adaptive 模式使用"},
		"micro_profit_quote":          {Type: "number", Description: "弱波动可接受的最小报价货币价差，仅 adaptive 模式使用"},
		"target_profit_quote":         {Type: "number", Description: "普通波动目标报价货币价差，仅 adaptive 模式使用"},
		"runner_profit_quote":         {Type: "number", Description: "强波动 runner 起始报价货币价差，仅 adaptive 模式使用"},
	}}}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

func IsSupported(code string) bool {
	_, ok := factories[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

const (
	StrategySupertrend = supertrend.Name
)

type Factory func(params map[string]string) (strategy.Strategy, error)

var factories = map[string]Factory{
	StrategySupertrend: buildSupertrend,
}

func Build(name string) (strategy.Strategy, error) {
	return BuildSpec(strategyspec.Legacy(name))
}

func BuildSet(names []string) ([]strategy.Strategy, error) {
	specs := make([]strategyspec.Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, strategyspec.Legacy(name))
	}
	return BuildSpecs(specs)
}

func BuildSpec(raw strategyspec.Spec) (strategy.Strategy, error) {
	spec := strategyspec.Normalize(raw)
	if spec.Name == "" {
		return nil, fmt.Errorf("strategy name cannot be empty")
	}
	factory, ok := factories[spec.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported strategy %q", spec.Name)
	}
	item, err := factory(spec.Params)
	if err != nil {
		return nil, fmt.Errorf("build strategy %s: %w", spec.Name, err)
	}
	return item, nil
}

func BuildSpecs(specs []strategyspec.Spec) ([]strategy.Strategy, error) {
	seen := make(map[string]struct{}, len(specs))
	strategies := make([]strategy.Strategy, 0, len(specs))
	for _, raw := range specs {
		spec := strategyspec.Normalize(raw)
		if !spec.Enabled {
			continue
		}
		if _, ok := seen[spec.Name]; ok {
			return nil, fmt.Errorf("duplicate strategy name %q", spec.Name)
		}
		item, err := BuildSpec(spec)
		if err != nil {
			return nil, err
		}
		seen[spec.Name] = struct{}{}
		strategies = append(strategies, item)
	}
	if len(strategies) == 0 {
		return nil, fmt.Errorf("at least one strategy must be enabled")
	}
	return strategies, nil
}

func buildSupertrend(params map[string]string) (strategy.Strategy, error) {
	config := supertrend.Config{}
	for key, value := range params {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "entry_threshold":
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed <= 0 || parsed > 1 {
				return nil, fmt.Errorf("entry_threshold must be a number in (0,1]")
			}
			config.EntryThreshold = parsed
		case "max_blocking_timeframes":
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("max_blocking_timeframes must be a positive integer")
			}
			config.MaxBlockingTimeframes = parsed
		case "min_take_profit_bps":
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, fmt.Errorf("min_take_profit_bps must be a non-negative finite number")
			}
			config.MinTakeProfitBps = parsed
		case "take_profit_cost_floor_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("take_profit_cost_floor_bps must be a finite number in [0,10000)")
			}
			config.TakeProfitCostFloorBps = parsed
		case "min_reward_risk_ratio":
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, fmt.Errorf("min_reward_risk_ratio must be a non-negative finite number")
			}
			config.MinRewardRiskRatio = parsed
		case "max_stop_loss_bps":
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || parsed >= 10000 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, fmt.Errorf("max_stop_loss_bps must be a finite number in [0,10000)")
			}
			config.MaxStopLossBps = parsed
		case "exit_mode":
			config.ExitMode = strings.ToLower(strings.TrimSpace(value))
		case "trailing_stop_pct":
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, fmt.Errorf("trailing_stop_pct must be a non-negative finite number")
			}
			config.TrailingStopPct = parsed
		case "profit_guard_activation_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("profit_guard_activation_bps must be a finite number in [0,10000)")
			}
			config.ProfitGuardActivationBps = parsed
		case "profit_guard_floor_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("profit_guard_floor_bps must be a finite number in [0,10000)")
			}
			config.ProfitGuardFloorBps = parsed
		case "profit_decay_activation_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("profit_decay_activation_bps must be a finite number in [0,10000)")
			}
			config.ProfitDecayActivationBps = parsed
		case "round_trip_cost_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("round_trip_cost_bps must be a finite number in [0,10000)")
			}
			config.RoundTripCostBps = parsed
		case "profit_buffer_bps":
			parsed, err := parseBasisPoints(value)
			if err != nil {
				return nil, fmt.Errorf("profit_buffer_bps must be a finite number in [0,10000)")
			}
			config.ProfitBufferBps = parsed
		case "micro_profit_quote":
			parsed, err := parseNonNegativeFinite(value)
			if err != nil {
				return nil, fmt.Errorf("micro_profit_quote must be a non-negative finite number")
			}
			config.MicroProfitQuote = parsed
		case "target_profit_quote":
			parsed, err := parseNonNegativeFinite(value)
			if err != nil {
				return nil, fmt.Errorf("target_profit_quote must be a non-negative finite number")
			}
			config.TargetProfitQuote = parsed
		case "runner_profit_quote":
			parsed, err := parseNonNegativeFinite(value)
			if err != nil {
				return nil, fmt.Errorf("runner_profit_quote must be a non-negative finite number")
			}
			config.RunnerProfitQuote = parsed
		default:
			return nil, fmt.Errorf("unknown parameter %q", key)
		}
	}
	if config.ExitMode == "" {
		config.ExitMode = supertrend.ExitModeStructure
	}
	switch config.ExitMode {
	case supertrend.ExitModeStructure:
		if config.TrailingStopPct > 0 || config.ProfitGuardActivationBps > 0 || config.ProfitGuardFloorBps > 0 || config.ProfitDecayActivationBps > 0 {
			return nil, fmt.Errorf("trailing exit parameters require exit_mode %q", supertrend.ExitModeTrailing)
		}
		if adaptiveExitParametersSet(config) {
			return nil, fmt.Errorf("adaptive exit parameters require exit_mode %q", supertrend.ExitModeAdaptive)
		}
	case supertrend.ExitModeTrailing:
		if config.TrailingStopPct <= 0 {
			return nil, fmt.Errorf("trailing_stop_pct must be positive when exit_mode is %q", supertrend.ExitModeTrailing)
		}
		if config.MinTakeProfitBps > 0 || config.TakeProfitCostFloorBps > 0 || config.MinRewardRiskRatio > 0 {
			return nil, fmt.Errorf("fixed take-profit geometry parameters cannot be used with exit_mode %q", supertrend.ExitModeTrailing)
		}
		guardActivation := config.ProfitGuardActivationBps
		if guardActivation <= 0 {
			guardActivation = supertrend.DefaultProfitGuardActivationBps
		}
		guardFloor := config.ProfitGuardFloorBps
		if guardFloor <= 0 {
			guardFloor = supertrend.DefaultProfitGuardFloorBps
		}
		decayActivation := config.ProfitDecayActivationBps
		if decayActivation <= 0 {
			decayActivation = supertrend.DefaultProfitDecayActivationBps
		}
		if guardFloor >= guardActivation {
			return nil, fmt.Errorf("profit_guard_floor_bps must be less than profit_guard_activation_bps")
		}
		if decayActivation < guardActivation {
			return nil, fmt.Errorf("profit_decay_activation_bps must be greater than or equal to profit_guard_activation_bps")
		}
		if adaptiveExitParametersSet(config) {
			return nil, fmt.Errorf("adaptive exit parameters require exit_mode %q", supertrend.ExitModeAdaptive)
		}
	case supertrend.ExitModeAdaptive:
		if config.TrailingStopPct > 0 || config.ProfitGuardActivationBps > 0 || config.ProfitGuardFloorBps > 0 || config.ProfitDecayActivationBps > 0 {
			return nil, fmt.Errorf("fixed trailing parameters cannot be used with exit_mode %q", supertrend.ExitModeAdaptive)
		}
		if config.MinTakeProfitBps > 0 || config.TakeProfitCostFloorBps > 0 || config.MinRewardRiskRatio > 0 {
			return nil, fmt.Errorf("fixed take-profit geometry parameters cannot be used with exit_mode %q", supertrend.ExitModeAdaptive)
		}
		micro := defaultPositive(config.MicroProfitQuote, supertrend.DefaultMicroProfitQuote)
		target := defaultPositive(config.TargetProfitQuote, supertrend.DefaultTargetProfitQuote)
		runner := defaultPositive(config.RunnerProfitQuote, supertrend.DefaultRunnerProfitQuote)
		if micro >= target || target >= runner {
			return nil, fmt.Errorf("adaptive quote profits must satisfy micro_profit_quote < target_profit_quote < runner_profit_quote")
		}
	default:
		return nil, fmt.Errorf("exit_mode must be %q, %q, or %q", supertrend.ExitModeStructure, supertrend.ExitModeTrailing, supertrend.ExitModeAdaptive)
	}
	return supertrend.New(config), nil
}

func adaptiveExitParametersSet(config supertrend.Config) bool {
	return config.RoundTripCostBps > 0 || config.ProfitBufferBps > 0 ||
		config.MicroProfitQuote > 0 || config.TargetProfitQuote > 0 || config.RunnerProfitQuote > 0
}

func defaultPositive(value float64, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func parseNonNegativeFinite(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid non-negative finite number")
	}
	return parsed, nil
}

func parseBasisPoints(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 || parsed >= 10000 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid basis points")
	}
	return parsed, nil
}
