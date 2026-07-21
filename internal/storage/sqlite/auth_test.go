package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/auth"
)

func TestAuthenticationStorePersistsOneTimeBootstrapAndAuditedSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	user := auth.User{ID: "user_admin", Username: "admin", DisplayName: "Administrator", Role: auth.RoleAdministrator, CreatedAt: now, UpdatedAt: now}
	session := auth.Session{
		ID: "session_bootstrap", TokenHash: bytes.Repeat([]byte{1}, 32), CSRFHash: bytes.Repeat([]byte{2}, 32),
		User: user, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	complete, err := store.BootstrapStatus(ctx)
	if err != nil || complete {
		t.Fatalf("unexpected initial status: complete=%v err=%v", complete, err)
	}
	if err := store.BootstrapAdministrator(ctx, user, "encoded-hash", session, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := store.BootstrapAdministrator(ctx, user, "encoded-hash", session, "127.0.0.1"); !errors.Is(err, auth.ErrAlreadyBootstrapped) {
		t.Fatalf("expected one-time bootstrap conflict, got %v", err)
	}
	loadedUser, passwordHash, err := store.FindUserByUsername(ctx, "ADMIN")
	if err != nil || loadedUser.ID != user.ID || passwordHash != "encoded-hash" {
		t.Fatalf("unexpected user: %+v %q %v", loadedUser, passwordHash, err)
	}
	loaded, err := store.SessionByTokenHash(ctx, session.TokenHash)
	if err != nil || loaded.User.ID != user.ID || !bytes.Equal(loaded.CSRFHash, session.CSRFHash) {
		t.Fatalf("unexpected session: %+v %v", loaded, err)
	}
	newCSRF := bytes.Repeat([]byte{3}, 32)
	if err := store.RotateSessionCSRF(ctx, session.ID, newCSRF, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionReauthenticated(ctx, session.ID, now.Add(6*time.Minute), now.Add(time.Minute), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(ctx, session.ID, now.Add(2*time.Minute), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.SessionByTokenHash(ctx, session.TokenHash)
	if err != nil || loaded.RevokedAt.IsZero() || loaded.ReauthenticatedUntil.IsZero() || !bytes.Equal(loaded.CSRFHash, newCSRF) {
		t.Fatalf("session lifecycle was not persisted: %+v %v", loaded, err)
	}
	events, err := store.ListAudit(ctx, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"auth.bootstrap": false, "auth.reauthenticate": false, "auth.logout": false}
	for _, event := range events {
		if _, ok := want[event.Action]; ok {
			want[event.Action] = true
		}
	}
	for action, observed := range want {
		if !observed {
			t.Fatalf("missing audit action %s: %+v", action, events)
		}
	}
}
