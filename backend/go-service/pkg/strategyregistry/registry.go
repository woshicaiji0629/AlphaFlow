package strategyregistry

import (
	"fmt"
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
		"entry_threshold":         {Type: "number", Description: "开仓置信度阈值，范围(0,1]"},
		"max_blocking_timeframes": {Type: "integer", Description: "允许阻塞的确认周期数量，必须为正整数"},
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
		default:
			return nil, fmt.Errorf("unknown parameter %q", key)
		}
	}
	return supertrend.New(config), nil
}
