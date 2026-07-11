package service

import (
	"alphaflow/go-service/control-api/internal/domain"
	"context"
	"github.com/google/uuid"
	"testing"
)

type accountRepository struct{ items []domain.TradingAccount }

func (r accountRepository) ListEnabledAccounts(context.Context, uuid.UUID) ([]domain.TradingAccount, error) {
	return r.items, nil
}

type dashboardPositionReader struct {
	calls int
	items []domain.DashboardPosition
}

func (r *dashboardPositionReader) ListPositions(context.Context, string, string) ([]domain.DashboardPosition, error) {
	r.calls++
	return r.items, nil
}
func TestDashboardReadsOnlyUserAccounts(t *testing.T) {
	positions := &dashboardPositionReader{items: []domain.DashboardPosition{{ID: "p1"}}}
	result, err := NewDashboardService(accountRepository{items: []domain.TradingAccount{{Mode: "paper", AccountKey: "default"}}}, positions).Get(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if positions.calls != 1 || len(result.Positions) != 1 {
		t.Fatalf("unexpected result %#v", result)
	}
}
func TestDashboardWithoutAccountsReturnsNoPositions(t *testing.T) {
	positions := &dashboardPositionReader{}
	result, err := NewDashboardService(accountRepository{}, positions).Get(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if positions.calls != 0 || result.DataStatus["positions"] != "no_trading_account" {
		t.Fatalf("unexpected result %#v", result)
	}
}
