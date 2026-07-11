package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"github.com/google/uuid"
)

type AuthService struct {
	store                        repository.SessionRepository
	passwords                    repository.PasswordHasher
	idleTimeout, absoluteTimeout time.Duration
	refreshInterval              time.Duration
	limiter                      repository.LoginLimiter
	audit                        repository.AuditRepository
	now                          func() time.Time
}

func NewAuthService(store repository.SessionRepository, passwords repository.PasswordHasher, limiter repository.LoginLimiter, audit repository.AuditRepository, idle, absolute, refresh time.Duration) (*AuthService, error) {
	if store == nil || passwords == nil || limiter == nil || audit == nil || idle <= 0 || absolute <= 0 || idle > absolute || refresh <= 0 || refresh >= idle {
		return nil, fmt.Errorf("invalid session timeouts")
	}
	return &AuthService{store: store, passwords: passwords, limiter: limiter, audit: audit, idleTimeout: idle, absoluteTimeout: absolute, refreshInterval: refresh, now: time.Now}, nil
}

type LoginResult struct {
	User                    domain.User
	SessionToken, CSRFToken string
	ExpiresAt               time.Time
}

func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string) (LoginResult, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	allowed, err := s.limiter.Allow(ctx, email, ip)
	if err != nil {
		return LoginResult{}, fmt.Errorf("check login limit: %w", err)
	}
	if !allowed {
		if err := s.recordLogin(ctx, nil, "blocked", normalized, ip, userAgent); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, domain.ErrRateLimited
	}
	user, err := s.store.FindUserForLogin(ctx, normalized)
	if err != nil {
		if auditErr := s.recordLogin(ctx, nil, "failure", normalized, ip, userAgent); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	valid, err := s.passwords.Verify(user.PasswordHash, password)
	if err != nil || !valid {
		if auditErr := s.recordLogin(ctx, &user.ID, "failure", normalized, ip, userAgent); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	sessionToken, tokenHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, csrfHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	absolute := now.Add(s.absoluteTimeout)
	sessionID := uuid.New()
	if err := s.store.CreateSession(ctx, domain.NewSession{ID: sessionID, UserID: user.ID, TokenHash: tokenHash, CSRFTokenHash: csrfHash, UserAgent: userAgent, IPAddress: ip, LastSeenAt: now, IdleExpiresAt: now.Add(s.idleTimeout), AbsoluteExpiresAt: absolute}); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	user.PasswordHash = ""
	if err := s.limiter.ResetEmail(ctx, email); err != nil {
		_ = s.store.RevokeSession(ctx, sessionID, now)
		return LoginResult{}, fmt.Errorf("reset login limit: %w", err)
	}
	if err := s.recordLogin(ctx, &user.ID, "success", normalized, ip, userAgent); err != nil {
		_ = s.store.RevokeSession(ctx, sessionID, now)
		return LoginResult{}, err
	}
	return LoginResult{User: user, SessionToken: sessionToken, CSRFToken: csrfToken, ExpiresAt: absolute}, nil
}

func (s *AuthService) recordLogin(ctx context.Context, userID *uuid.UUID, outcome, subject, ip, userAgent string) error {
	return s.audit.Record(ctx, domain.AuditEvent{ID: uuid.New(), UserID: userID, EventType: "auth.login", Outcome: outcome, Subject: subject, IPAddress: ip, UserAgent: userAgent, CreatedAt: s.now().UTC()})
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (domain.Session, error) {
	if token == "" {
		return domain.Session{}, domain.ErrInvalidSession
	}
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.FindSession(ctx, hash, s.now().UTC(), s.idleTimeout, s.refreshInterval)
	if err != nil {
		return domain.Session{}, domain.ErrInvalidSession
	}
	return session, nil
}
func (s *AuthService) Logout(ctx context.Context, session domain.Session, ip, userAgent string) error {
	if err := s.store.RevokeSession(ctx, session.ID, s.now().UTC()); err != nil {
		return err
	}
	return s.audit.Record(ctx, domain.AuditEvent{ID: uuid.New(), UserID: &session.User.ID, EventType: "auth.logout", Outcome: "success", Subject: session.User.Email, IPAddress: ip, UserAgent: userAgent, CreatedAt: s.now().UTC()})
}
func VerifyCSRF(session domain.Session, value string) bool {
	actual := sha256.Sum256([]byte(value))
	return value != "" && actual == session.CSRFTokenHash
}
func newToken() (string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}
