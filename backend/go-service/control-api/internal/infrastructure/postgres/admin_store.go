package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAlreadyInitialized = errors.New("control-api is already initialized")

type AdminStore struct {
	pool *pgxpool.Pool
}

func NewAdminStore(pool *pgxpool.Pool) *AdminStore {
	return &AdminStore{pool: pool}
}

func (s *AdminStore) CreateInitialAdmin(ctx context.Context, input domain.InitialAdmin) (domain.InitialAdminResult, error) {
	normalized, err := normalizeEmail(input.Email)
	if err != nil {
		return domain.InitialAdminResult{}, err
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || input.PasswordHash == "" {
		return domain.InitialAdminResult{}, fmt.Errorf("display name and password hash are required")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.InitialAdminResult{}, fmt.Errorf("begin initial admin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var initialized bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users)").Scan(&initialized); err != nil {
		return domain.InitialAdminResult{}, fmt.Errorf("check existing users: %w", err)
	}
	if initialized {
		return domain.InitialAdminResult{}, ErrAlreadyInitialized
	}

	result := domain.InitialAdminResult{UserID: uuid.New(), TradingAccountID: uuid.New()}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO users
(id, email, email_normalized, display_name, password_hash, role, password_changed_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,'admin',$6,$6,$6)`, result.UserID, strings.TrimSpace(input.Email), normalized, input.DisplayName, input.PasswordHash, now); err != nil {
		return domain.InitialAdminResult{}, fmt.Errorf("insert initial admin: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trading_accounts
(id, owner_user_id, mode, account_key, display_name, status, trading_enabled, created_at, updated_at)
VALUES ($1,$2,'paper','default','Default Paper Account','active',TRUE,$3,$3)`, result.TradingAccountID, result.UserID, now); err != nil {
		return domain.InitialAdminResult{}, fmt.Errorf("insert initial paper account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.InitialAdminResult{}, fmt.Errorf("commit initial admin: %w", err)
	}
	return result, nil
}

var _ repository.AdminRepository = (*AdminStore)(nil)

func normalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", fmt.Errorf("invalid email address")
	}
	return normalized, nil
}
