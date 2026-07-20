package auth

import (
	"context"
	"errors"
	"time"
)

type Role string

const (
	RoleViewer        Role = "viewer"
	RoleOperator      Role = "operator"
	RoleReviewer      Role = "reviewer"
	RoleAdministrator Role = "administrator"
)

var (
	ErrNotFound            = errors.New("authentication record not found")
	ErrAlreadyBootstrapped = errors.New("administrator bootstrap is already complete")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidInput        = errors.New("invalid authentication input")
	ErrRateLimited         = errors.New("authentication attempts are rate limited")
	ErrSessionExpired      = errors.New("session is expired or revoked")
)

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        Role      `json:"role"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Session struct {
	ID                   string
	TokenHash            []byte
	CSRFHash             []byte
	User                 User
	CreatedAt            time.Time
	LastSeenAt           time.Time
	ExpiresAt            time.Time
	ReauthenticatedUntil time.Time
	RevokedAt            time.Time
}

type Store interface {
	BootstrapStatus(context.Context) (bool, error)
	BootstrapAdministrator(context.Context, User, string, Session, string) error
	FindUserByUsername(context.Context, string) (User, string, error)
	CreateSession(context.Context, Session, string) error
	SessionByTokenHash(context.Context, []byte) (Session, error)
	RotateSessionCSRF(context.Context, string, []byte, time.Time) error
	MarkSessionReauthenticated(context.Context, string, time.Time, time.Time, string) error
	RevokeSession(context.Context, string, time.Time, string) error
}

type Principal struct {
	User                 User      `json:"user"`
	SessionID            string    `json:"-"`
	ExpiresAt            time.Time `json:"expires_at"`
	ReauthenticatedUntil time.Time `json:"reauthenticated_until,omitempty"`
	CSRFHash             []byte    `json:"-"`
}

func (p Principal) RecentlyReauthenticated(now time.Time) bool {
	return !p.ReauthenticatedUntil.IsZero() && now.Before(p.ReauthenticatedUntil)
}

type Permission string

const (
	PermissionRead       Permission = "read"
	PermissionOperate    Permission = "operate"
	PermissionReview     Permission = "review"
	PermissionAdminister Permission = "administer"
)

func (r Role) Allows(permission Permission) bool {
	switch r {
	case RoleAdministrator:
		return true
	case RoleReviewer:
		return permission == PermissionRead || permission == PermissionReview
	case RoleOperator:
		return permission == PermissionRead || permission == PermissionOperate
	case RoleViewer:
		return permission == PermissionRead
	default:
		return false
	}
}
