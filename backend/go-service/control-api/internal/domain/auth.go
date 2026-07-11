package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInvalidSession = errors.New("invalid session")
var ErrRateLimited = errors.New("login rate limited")
var ErrForbidden = errors.New("forbidden")

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	Permissions  []string  `json:"permissions"`
	PasswordHash string    `json:"-"`
}

type Session struct {
	ID                uuid.UUID
	User              User
	CSRFTokenHash     [32]byte
	AbsoluteExpiresAt time.Time
}

type NewSession struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	TokenHash         [32]byte
	CSRFTokenHash     [32]byte
	UserAgent         string
	IPAddress         string
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

type InitialAdmin struct {
	Email, DisplayName, PasswordHash string
}

type InitialAdminResult struct {
	UserID, TradingAccountID uuid.UUID
}

type AuditEvent struct {
	ID                                                           uuid.UUID
	UserID                                                       *uuid.UUID
	EventType, Outcome, Subject, IPAddress, UserAgent, RequestID string
	CreatedAt                                                    time.Time
}
