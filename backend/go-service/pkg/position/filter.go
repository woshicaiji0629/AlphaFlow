package position

import (
	"fmt"

	"alphaflow/go-service/pkg/strategy"
)

func validateListFilter(filter Filter) error {
	switch filter.Scope {
	case strategy.PositionScopePaper, strategy.PositionScopeTestnet, strategy.PositionScopeLive:
		return nil
	case strategy.PositionScopeBacktest:
		if filter.RunID == "" {
			return fmt.Errorf("run_id is required for backtest position listing")
		}
		return nil
	default:
		return fmt.Errorf("unsupported position scope %q for listing", filter.Scope)
	}
}

func positionMatchesFilter(currentPosition strategy.Position, filter Filter) bool {
	if filter.Scope != "" && currentPosition.Scope != filter.Scope {
		return false
	}
	if filter.RunID != "" && currentPosition.RunID != filter.RunID {
		return false
	}
	if filter.Account != "" && currentPosition.Account != filter.Account {
		return false
	}
	if filter.Exchange != "" && currentPosition.Exchange != filter.Exchange {
		return false
	}
	if filter.Market != "" && currentPosition.Market != filter.Market {
		return false
	}
	if filter.Symbol != "" && currentPosition.Symbol != filter.Symbol {
		return false
	}
	return true
}
