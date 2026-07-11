package postgres

import (
	"context"
	"net"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditStore struct{ pool *pgxpool.Pool }

func NewAuditStore(pool *pgxpool.Pool) *AuditStore { return &AuditStore{pool: pool} }
func (s *AuditStore) Record(ctx context.Context, event domain.AuditEvent) error {
	var ip net.IP
	if event.IPAddress != "" {
		ip = net.ParseIP(event.IPAddress)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_logs(id,user_id,event_type,outcome,subject,ip_address,user_agent,request_id,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, event.ID, event.UserID, event.EventType, event.Outcome, event.Subject, ip, event.UserAgent, event.RequestID, event.CreatedAt)
	return err
}

var _ repository.AuditRepository = (*AuditStore)(nil)
