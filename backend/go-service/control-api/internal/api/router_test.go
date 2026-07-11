package api

import (
	"alphaflow/go-service/control-api/internal/domain"
	passwordinfra "alphaflow/go-service/control-api/internal/infrastructure/password"
	"alphaflow/go-service/control-api/internal/repository"
	"alphaflow/go-service/control-api/internal/service"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type healthChecker struct{ err error }

func (h healthChecker) Ping(context.Context) error { return h.err }

type sessionStore struct{}
type loginLimiter struct{}

func (loginLimiter) Allow(context.Context, string, string) (bool, error) { return true, nil }
func (loginLimiter) ResetEmail(context.Context, string) error            { return nil }

type auditStore struct{}

func (auditStore) Record(context.Context, domain.AuditEvent) error { return nil }

func (sessionStore) FindUserForLogin(context.Context, string) (domain.User, error) {
	return domain.User{}, domain.ErrInvalidCredentials
}
func (sessionStore) CreateSession(context.Context, domain.NewSession) error { return nil }
func (sessionStore) FindSession(context.Context, [32]byte, time.Time, time.Duration, time.Duration) (domain.Session, error) {
	return domain.Session{}, domain.ErrInvalidSession
}
func (sessionStore) RevokeSession(context.Context, uuid.UUID, time.Time) error { return nil }

func testAuth(t *testing.T) AuthOptions {
	t.Helper()
	var store repository.SessionRepository = sessionStore{}
	authService, err := service.NewAuthService(store, passwordinfra.Hasher{}, loginLimiter{}, auditStore{}, time.Hour, 24*time.Hour, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return AuthOptions{Service: authService, CookieName: "session", CSRFCookieName: "csrf"}
}

func testRouter(t *testing.T, health healthChecker) http.Handler {
	t.Helper()
	strategyRepository := &adminStrategyAPIRepository{}
	return NewRouter("test", health, testAuth(t), nil, nil, service.NewAdminStrategyService(strategyRepository), SecurityOptions{AllowedOrigins: []string{"http://localhost:5173"}, MaxBodyBytes: 1024})
}

type adminStrategyAPIRepository struct{}

func (*adminStrategyAPIRepository) ListAdminStrategies(context.Context) ([]domain.AdminStrategy, error) {
	return []domain.AdminStrategy{}, nil
}

func (*adminStrategyAPIRepository) FindAdminStrategy(context.Context, uuid.UUID) (domain.AdminStrategy, error) {
	return domain.AdminStrategy{}, domain.ErrStrategyNotFound
}

func (*adminStrategyAPIRepository) CreateAdminStrategy(_ context.Context, strategy domain.AdminStrategy, _ domain.AuditEvent) (domain.AdminStrategy, error) {
	return strategy, nil
}

func (*adminStrategyAPIRepository) UpdateAdminStrategy(_ context.Context, strategy domain.AdminStrategy, _ domain.AuditEvent) (domain.AdminStrategy, error) {
	return strategy, nil
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID is empty")
	}
}

func TestNotFoundUsesErrorContract(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-ID", "test-request")
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	var body errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" || body.Error.RequestID != "test-request" {
		t.Fatalf("unexpected error body: %#v", body)
	}
}

func TestReadyReturnsUnavailableWhenDatabaseIsDown(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{err: context.DeadlineExceeded}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestSecurityHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{}).ServeHTTP(recorder, request)
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store header")
	}
}

func TestLoginRejectsUntrustedOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@b.com","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	body := `{"email":"a@b.com","password":"` + strings.Repeat("x", 2048) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:5173")
	recorder := httptest.NewRecorder()
	testRouter(t, healthChecker{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", recorder.Code)
	}
}
