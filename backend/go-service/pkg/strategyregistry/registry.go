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
		"min_reward_risk_ratio":       {Type: "number", Description: "最小预期盈亏比，0 表示禁用"},
		"max_stop_loss_bps":           {Type: "number", Description: "最大固定止损距离（基点），0 表示禁用"},
		"exit_mode":                   {Type: "string", Description: "全仓出场模式：structure 或 trailing"},
		"trailing_stop_pct":           {Type: "number", Description: "全仓跟踪止损距离（百分比），仅 trailing 模式使用"},
		"profit_guard_activation_bps": {Type: "number", Description: "保盈保护激活距离（基点），仅 trailing 模式使用"},
		"profit_guard_floor_bps":      {Type: "number", Description: "保盈保护最低利润（基点），仅 trailing 模式使用"},
		"profit_decay_activation_bps": {Type: "number", Description: "指标衰减退出激活距离（基点），仅 trailing 模式使用"},
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
	case supertrend.ExitModeTrailing:
		if config.TrailingStopPct <= 0 {
			return nil, fmt.Errorf("trailing_stop_pct must be positive when exit_mode is %q", supertrend.ExitModeTrailing)
		}
		if config.MinTakeProfitBps > 0 || config.MinRewardRiskRatio > 0 {
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
	default:
		return nil, fmt.Errorf("exit_mode must be %q or %q", supertrend.ExitModeStructure, supertrend.ExitModeTrailing)
	}
	return supertrend.New(config), nil
}

func parseBasisPoints(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 || parsed >= 10000 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid basis points")
	}
	return parsed, nil
}
