package repository

import (
	"context"
	"time"

	"alphaflow/go-service/control-api/internal/domain"
	"github.com/google/uuid"
)

type SessionRepository interface {
	FindUserForLogin(context.Context, string) (domain.User, error)
	CreateSession(context.Context, domain.NewSession) error
	FindSession(context.Context, [32]byte, time.Time, time.Duration, time.Duration) (domain.Session, error)
	RevokeSession(context.Context, uuid.UUID, time.Time) error
}

type PasswordHasher interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
}

type AdminRepository interface {
	CreateInitialAdmin(context.Context, domain.InitialAdmin) (domain.InitialAdminResult, error)
}

type LoginLimiter interface {
	Allow(context.Context, string, string) (bool, error)
	ResetEmail(context.Context, string) error
}

type AuditRepository interface {
	Record(context.Context, domain.AuditEvent) error
}

type TradingAccountRepository interface {
	ListEnabledAccounts(context.Context, uuid.UUID) ([]domain.TradingAccount, error)
}
type PositionReader interface {
	ListPositions(context.Context, string, string) ([]domain.DashboardPosition, error)
}
type StrategyCatalogRepository interface {
	ListVisibleStrategies(context.Context, uuid.UUID, bool) ([]domain.PublishedStrategy, error)
	ListPerformance(context.Context, uuid.UUID) ([]domain.StrategyPerformance, error)
}

type AdminStrategyRepository interface {
	ListAdminStrategies(context.Context) ([]domain.AdminStrategy, error)
	FindAdminStrategy(context.Context, uuid.UUID) (domain.AdminStrategy, error)
	CreateAdminStrategy(context.Context, domain.AdminStrategy, domain.AuditEvent) (domain.AdminStrategy, error)
	UpdateAdminStrategy(context.Context, domain.AdminStrategy, domain.AuditEvent) (domain.AdminStrategy, error)
}
