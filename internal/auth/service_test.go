package auth

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu           sync.Mutex
	bootstrapped bool
	user         User
	passwordHash string
	sessions     map[string]Session
}

func newMemoryStore() *memoryStore { return &memoryStore{sessions: make(map[string]Session)} }

func (m *memoryStore) BootstrapStatus(context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bootstrapped, nil
}

func (m *memoryStore) BootstrapAdministrator(_ context.Context, user User, passwordHash string, session Session, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bootstrapped {
		return ErrAlreadyBootstrapped
	}
	m.bootstrapped, m.user, m.passwordHash = true, user, passwordHash
	m.sessions[session.ID] = session
	return nil
}

func (m *memoryStore) FindUserByUsername(_ context.Context, username string) (User, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.bootstrapped || username != m.user.Username {
		return User{}, "", ErrNotFound
	}
	return m.user, m.passwordHash, nil
}

func (m *memoryStore) CreateSession(_ context.Context, session Session, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = session
	return nil
}

func (m *memoryStore) SessionByTokenHash(_ context.Context, hash []byte) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if bytes.Equal(session.TokenHash, hash) {
			return session, nil
		}
	}
	return Session{}, ErrNotFound
}

func (m *memoryStore) RotateSessionCSRF(_ context.Context, id string, hash []byte, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.CSRFHash, session.LastSeenAt = hash, now
	m.sessions[id] = session
	return nil
}

func (m *memoryStore) MarkSessionReauthenticated(_ context.Context, id string, until, now time.Time, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.ReauthenticatedUntil, session.LastSeenAt = until, now
	m.sessions[id] = session
	return nil
}

func (m *memoryStore) RevokeSession(_ context.Context, id string, now time.Time, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.RevokedAt = now
	m.sessions[id] = session
	return nil
}

func TestServiceBootstrapLoginCSRFReauthenticationAndLogout(t *testing.T) {
	store := newMemoryStore()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.passwords = passwordParameters{memory: 8 * 1024, iterations: 1, parallelism: 1, keyLength: 32}
	service.dummyHash, err = service.hashPassword("dummy-password-never-used")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.Bootstrap(context.Background(), Credentials{
		Username: "Admin_One", DisplayName: "Local Administrator", Password: "correct horse battery staple",
	}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Principal.User.Username != "admin_one" || result.Principal.User.Role != RoleAdministrator {
		t.Fatalf("unexpected bootstrap principal: %+v", result.Principal)
	}
	if _, err := service.Bootstrap(context.Background(), Credentials{Username: "other", Password: "another excellent passphrase"}, "127.0.0.1"); err != ErrAlreadyBootstrapped {
		t.Fatalf("expected one-time bootstrap rejection, got %v", err)
	}
	principal, err := service.Authenticate(context.Background(), result.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !service.VerifyCSRF(principal, result.CSRFToken) {
		t.Fatal("bootstrap CSRF token did not verify")
	}
	rotatedToken, principal, err := service.RotateCSRF(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	if rotatedToken == result.CSRFToken || service.VerifyCSRF(principal, result.CSRFToken) || !service.VerifyCSRF(principal, rotatedToken) {
		t.Fatal("CSRF rotation did not invalidate the prior token")
	}
	principal, err = service.Reauthenticate(context.Background(), principal, "correct horse battery staple", "127.0.0.1")
	if err != nil || !principal.RecentlyReauthenticated(now) {
		t.Fatalf("reauthentication failed: %v", err)
	}
	if err := service.Logout(context.Background(), principal, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), result.Token); err != ErrSessionExpired {
		t.Fatalf("expected revoked session, got %v", err)
	}

	login, err := service.Login(context.Background(), Credentials{Username: "ADMIN_ONE", Password: "correct horse battery staple"}, "127.0.0.1")
	if err != nil || login.Token == "" {
		t.Fatalf("login failed: %v", err)
	}
}

func TestServiceRateLimitsFailuresAndRolesAreNotAccidentallyHierarchical(t *testing.T) {
	store := newMemoryStore()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.passwords = passwordParameters{memory: 8 * 1024, iterations: 1, parallelism: 1, keyLength: 32}
	service.dummyHash, _ = service.hashPassword("dummy-password-never-used")
	for attempt := 0; attempt < 5; attempt++ {
		_, err = service.Login(context.Background(), Credentials{Username: "unknown", Password: "long enough invalid password"}, "192.0.2.1")
		if err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err = service.Login(context.Background(), Credentials{Username: "unknown", Password: "long enough invalid password"}, "192.0.2.1"); err != ErrRateLimited {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if RoleReviewer.Allows(PermissionOperate) || RoleOperator.Allows(PermissionReview) || !RoleAdministrator.Allows(PermissionAdminister) {
		t.Fatal("role permissions broadened across operator and reviewer boundaries")
	}
}
