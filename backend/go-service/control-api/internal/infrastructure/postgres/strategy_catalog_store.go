package postgres

import (
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"context"
	"encoding/json"
	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StrategyCatalogStore struct{ pool *pgxpool.Pool }

func NewStrategyCatalogStore(pool *pgxpool.Pool) *StrategyCatalogStore {
	return &StrategyCatalogStore{pool: pool}
}

func (s *StrategyCatalogStore) FindAdminStrategy(ctx context.Context, id uuid.UUID) (domain.AdminStrategy, error) {
	var item domain.AdminStrategy
	var parameters []byte
	err := s.pool.QueryRow(ctx, `SELECT id,code,name,description,version,parameters,status,visibility,risk_level,paper_enabled,live_enabled,created_at,updated_at FROM strategies WHERE id=$1`, id).Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Version, &parameters, &item.Status, &item.Visibility, &item.RiskLevel, &item.PaperEnabled, &item.LiveEnabled, &item.CreatedAt, &item.UpdatedAt)
	if err == pgx.ErrNoRows {
		return domain.AdminStrategy{}, domain.ErrStrategyNotFound
	}
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	if err := json.Unmarshal(parameters, &item.Parameters); err != nil {
		return domain.AdminStrategy{}, err
	}
	return item, nil
}
func (s *StrategyCatalogStore) ListVisibleStrategies(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]domain.PublishedStrategy, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,code,name,description,version,risk_level,paper_enabled,live_enabled FROM strategies st WHERE status='published' AND ($2 OR visibility='public' OR EXISTS(SELECT 1 FROM strategy_entitlements e WHERE e.strategy_id=st.id AND e.user_id=$1 AND (e.expires_at IS NULL OR e.expires_at>NOW()))) ORDER BY name,version`, userID, isAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.PublishedStrategy{}
	for rows.Next() {
		var item domain.PublishedStrategy
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Version, &item.RiskLevel, &item.PaperEnabled, &item.LiveEnabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *StrategyCatalogStore) ListPerformance(ctx context.Context, strategyID uuid.UUID) ([]domain.StrategyPerformance, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,strategy_id,strategy_version,exchange,market,symbol_scope,interval,period_start,period_end,metrics,equity_points,published_at FROM strategy_performance_publications WHERE strategy_id=$1 ORDER BY published_at DESC`, strategyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.StrategyPerformance{}
	for rows.Next() {
		var item domain.StrategyPerformance
		if err := rows.Scan(&item.ID, &item.StrategyID, &item.StrategyVersion, &item.Exchange, &item.Market, &item.SymbolScope, &item.Interval, &item.PeriodStart, &item.PeriodEnd, &item.Metrics, &item.EquityPoints, &item.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *StrategyCatalogStore) ListAdminStrategies(ctx context.Context) ([]domain.AdminStrategy, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,code,name,description,version,parameters,status,visibility,risk_level,paper_enabled,live_enabled,created_at,updated_at FROM strategies ORDER BY name,version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []domain.AdminStrategy{}
	for rows.Next() {
		var item domain.AdminStrategy
		var parameters []byte
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Version, &parameters, &item.Status, &item.Visibility, &item.RiskLevel, &item.PaperEnabled, &item.LiveEnabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(parameters, &item.Parameters); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *StrategyCatalogStore) CreateAdminStrategy(ctx context.Context, strategy domain.AdminStrategy, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	parameters, err := json.Marshal(strategy.Parameters)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `INSERT INTO strategies (id,code,name,description,version,parameters,status,visibility,risk_level,paper_enabled,live_enabled) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING created_at,updated_at`, strategy.ID, strategy.Code, strategy.Name, strategy.Description, strategy.Version, parameters, strategy.Status, strategy.Visibility, strategy.RiskLevel, strategy.PaperEnabled, strategy.LiveEnabled).Scan(&strategy.CreatedAt, &strategy.UpdatedAt)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	var ip net.IP
	if audit.IPAddress != "" {
		ip = net.ParseIP(audit.IPAddress)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(id,user_id,event_type,outcome,subject,ip_address,user_agent,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, audit.ID, audit.UserID, audit.EventType, audit.Outcome, audit.Subject, ip, audit.UserAgent, audit.RequestID, audit.CreatedAt)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AdminStrategy{}, err
	}
	return strategy, nil
}

func (s *StrategyCatalogStore) UpdateAdminStrategy(ctx context.Context, strategy domain.AdminStrategy, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	parameters, err := json.Marshal(strategy.Parameters)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE strategies SET name=$2,description=$3,version=$4,parameters=$5,status=$6,visibility=$7,risk_level=$8,paper_enabled=$9,live_enabled=FALSE,updated_at=NOW() WHERE id=$1 AND status='draft' RETURNING created_at,updated_at`, strategy.ID, strategy.Name, strategy.Description, strategy.Version, parameters, strategy.Status, strategy.Visibility, strategy.RiskLevel, strategy.PaperEnabled).Scan(&strategy.CreatedAt, &strategy.UpdatedAt)
	if err == pgx.ErrNoRows {
		return domain.AdminStrategy{}, domain.ErrStrategyNotEditable
	}
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	var ip net.IP
	if audit.IPAddress != "" {
		ip = net.ParseIP(audit.IPAddress)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs(id,user_id,event_type,outcome,subject,ip_address,user_agent,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, audit.ID, audit.UserID, audit.EventType, audit.Outcome, audit.Subject, ip, audit.UserAgent, audit.RequestID, audit.CreatedAt); err != nil {
		return domain.AdminStrategy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AdminStrategy{}, err
	}
	return strategy, nil
}

var _ repository.StrategyCatalogRepository = (*StrategyCatalogStore)(nil)
var _ repository.AdminStrategyRepository = (*StrategyCatalogStore)(nil)
