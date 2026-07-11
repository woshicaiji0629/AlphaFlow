package postgres

import (
	"context"
	"fmt"
	"net"
	"time"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionStore struct{ pool *pgxpool.Pool }

func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

func (s *SessionStore) FindUserForLogin(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `SELECT id,email,display_name,role,password_hash FROM users
	WHERE email_normalized=$1 AND status='active'`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.PasswordHash)
	if err != nil {
		return domain.User{}, err
	}
	return s.loadPermissions(ctx, user)
}

func (s *SessionStore) CreateSession(ctx context.Context, item domain.NewSession) error {
	var ip net.IP
	if item.IPAddress != "" {
		ip = net.ParseIP(item.IPAddress)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions
(id,user_id,token_hash,csrf_token_hash,user_agent,ip_address,last_seen_at,idle_expires_at,absolute_expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, item.UserID, item.TokenHash[:], item.CSRFTokenHash[:], item.UserAgent, ip, item.LastSeenAt, item.IdleExpiresAt, item.AbsoluteExpiresAt)
	return err
}

func (s *SessionStore) FindSession(ctx context.Context, tokenHash [32]byte, now time.Time, idleTimeout, refreshInterval time.Duration) (domain.Session, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Session{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var session domain.Session
	var csrf []byte
	var lastSeen time.Time
	err = tx.QueryRow(ctx, `SELECT s.id,u.id,u.email,u.display_name,u.role,s.csrf_token_hash,s.absolute_expires_at,s.last_seen_at
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.idle_expires_at>$2 AND s.absolute_expires_at>$2 AND u.status='active'
		FOR UPDATE OF s`, tokenHash[:], now).Scan(&session.ID, &session.User.ID, &session.User.Email, &session.User.DisplayName, &session.User.Role, &csrf, &session.AbsoluteExpiresAt, &lastSeen)
	if err != nil {
		return domain.Session{}, err
	}
	if len(csrf) != 32 {
		return domain.Session{}, fmt.Errorf("invalid csrf hash length")
	}
	copy(session.CSRFTokenHash[:], csrf)
	rows, err := tx.Query(ctx, `SELECT permission FROM user_permissions WHERE user_id=$1 AND (expires_at IS NULL OR expires_at>$2) ORDER BY permission`, session.User.ID, now)
	if err != nil {
		return domain.Session{}, err
	}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			rows.Close()
			return domain.Session{}, err
		}
		session.User.Permissions = append(session.User.Permissions, permission)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return domain.Session{}, err
	}
	if now.Sub(lastSeen) >= refreshInterval {
		idleExpires := now.Add(idleTimeout)
		if idleExpires.After(session.AbsoluteExpiresAt) {
			idleExpires = session.AbsoluteExpiresAt
		}
		if _, err := tx.Exec(ctx, "UPDATE sessions SET last_seen_at=$2,idle_expires_at=$3 WHERE id=$1", session.ID, now, idleExpires); err != nil {
			return domain.Session{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

func (s *SessionStore) loadPermissions(ctx context.Context, user domain.User) (domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT permission FROM user_permissions WHERE user_id=$1 AND (expires_at IS NULL OR expires_at>NOW()) ORDER BY permission`, user.ID)
	if err != nil {
		return domain.User{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return domain.User{}, err
		}
		user.Permissions = append(user.Permissions, permission)
	}
	return user, rows.Err()
}

func (s *SessionStore) RevokeSession(ctx context.Context, id uuid.UUID, now time.Time) error {
	result, err := s.pool.Exec(ctx, "UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL", id, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

var _ repository.SessionRepository = (*SessionStore)(nil)
