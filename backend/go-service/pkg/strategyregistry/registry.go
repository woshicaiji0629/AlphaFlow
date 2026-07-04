package strategyregistry

import (
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/strategies/supertrend"
	"alphaflow/go-service/pkg/strategy"
)

const (
	StrategySupertrend = supertrend.Name
)

func Build(name string) (strategy.Strategy, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case StrategySupertrend:
		return supertrend.New(supertrend.Config{}), nil
	default:
		return nil, fmt.Errorf("unsupported strategy %q", name)
	}
}

func BuildSet(names []string) ([]strategy.Strategy, error) {
	strategies := make([]strategy.Strategy, 0, len(names))
	for _, name := range names {
		item, err := Build(name)
		if err != nil {
			return nil, err
		}
		strategies = append(strategies, item)
	}
	return strategies, nil
}
