package service

import (
	"context"
	"testing"
	"time"

	"alphaflow/go-service/control-api/internal/domain"
	passwordinfra "alphaflow/go-service/control-api/internal/infrastructure/password"
	"github.com/google/uuid"
)

type fakeSessionStore struct {
	user    domain.User
	created domain.NewSession
}
type fakeLimiter struct{ allowed bool }

func (f fakeLimiter) Allow(context.Context, string, string) (bool, error) { return f.allowed, nil }
func (fakeLimiter) ResetEmail(context.Context, string) error              { return nil }

type fakeAudit struct{}

func (fakeAudit) Record(context.Context, domain.AuditEvent) error { return nil }

func (s *fakeSessionStore) FindUserForLogin(context.Context, string) (domain.User, error) {
	return s.user, nil
}
func (s *fakeSessionStore) CreateSession(_ context.Context, item domain.NewSession) error {
	s.created = item
	return nil
}
func (s *fakeSessionStore) FindSession(context.Context, [32]byte, time.Time, time.Duration, time.Duration) (domain.Session, error) {
	return domain.Session{}, domain.ErrInvalidSession
}
func (s *fakeSessionStore) RevokeSession(context.Context, uuid.UUID, time.Time) error { return nil }

func TestServiceLoginCreatesHashedSession(t *testing.T) {
	hash, err := passwordinfra.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeSessionStore{user: domain.User{ID: uuid.New(), Email: "admin@example.com", PasswordHash: hash}}
	svc, err := NewAuthService(store, passwordinfra.Hasher{}, fakeLimiter{allowed: true}, fakeAudit{}, time.Hour, 24*time.Hour, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	result, err := svc.Login(context.Background(), "admin@example.com", "correct horse battery staple", "test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatal("generated tokens are empty")
	}
	if store.created.TokenHash == [32]byte{} || store.created.CSRFTokenHash == [32]byte{} {
		t.Fatal("stored token hashes are empty")
	}
	if result.User.PasswordHash != "" {
		t.Fatal("password hash leaked")
	}
	if !VerifyCSRF(domain.Session{CSRFTokenHash: store.created.CSRFTokenHash}, result.CSRFToken) {
		t.Fatal("csrf token did not verify")
	}
}
