package service

import (
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"context"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type DashboardService struct {
	accounts  repository.TradingAccountRepository
	positions repository.PositionReader
	now       func() time.Time
}

func NewDashboardService(accounts repository.TradingAccountRepository, positions repository.PositionReader) *DashboardService {
	return &DashboardService{accounts: accounts, positions: positions, now: time.Now}
}
func (s *DashboardService) Get(ctx context.Context, userID uuid.UUID) (domain.Dashboard, error) {
	accounts, err := s.accounts.ListEnabledAccounts(ctx, userID)
	if err != nil {
		return domain.Dashboard{}, fmt.Errorf("list trading accounts: %w", err)
	}
	positions := []domain.DashboardPosition{}
	mode := "paper"
	for _, account := range accounts {
		items, err := s.positions.ListPositions(ctx, account.Mode, account.AccountKey)
		if err != nil {
			return domain.Dashboard{}, fmt.Errorf("list %s positions: %w", account.Mode, err)
		}
		positions = append(positions, items...)
		if account.Mode == "live" {
			mode = "live"
		} else if account.Mode == "testnet" && mode != "live" {
			mode = "testnet"
		}
	}
	status := map[string]string{"positions": "ready", "signals": "not_configured", "equity": "not_configured", "strategy_performance": "not_configured"}
	if len(accounts) == 0 {
		status["positions"] = "no_trading_account"
	}
	return domain.Dashboard{AsOf: s.now().UTC(), Mode: mode, Metrics: []domain.DashboardMetric{{Label: "活跃仓位", Value: fmt.Sprintf("%d", len(positions))}}, Services: []domain.ServiceHealth{}, Positions: positions, Signals: []domain.DashboardSignal{}, Equity: []domain.EquityPoint{}, DataStatus: status}, nil
}
