package postgres

import (
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TradingAccountStore struct{ pool *pgxpool.Pool }

func NewTradingAccountStore(pool *pgxpool.Pool) *TradingAccountStore {
	return &TradingAccountStore{pool: pool}
}
func (s *TradingAccountStore) ListEnabledAccounts(ctx context.Context, userID uuid.UUID) ([]domain.TradingAccount, error) {
	rows, err := s.pool.Query(ctx, `SELECT mode,account_key,display_name FROM trading_accounts WHERE owner_user_id=$1 AND status='active' ORDER BY mode,account_key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.TradingAccount{}
	for rows.Next() {
		var item domain.TradingAccount
		if err := rows.Scan(&item.Mode, &item.AccountKey, &item.DisplayName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ repository.TradingAccountRepository = (*TradingAccountStore)(nil)
