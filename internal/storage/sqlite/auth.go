package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/local-code-maintainer/appliance/internal/auth"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Store) BootstrapStatus(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_bootstrap WHERE singleton = 1").Scan(&count); err != nil {
		return false, fmt.Errorf("read authentication bootstrap status: %w", err)
	}
	return count != 0, nil
}

func (s *Store) BootstrapAdministrator(ctx context.Context, user auth.User, passwordHash string, session auth.Session, remote string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator bootstrap: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_bootstrap WHERE singleton = 1").Scan(&count); err != nil {
		return fmt.Errorf("check administrator bootstrap: %w", err)
	}
	if count != 0 {
		return auth.ErrAlreadyBootstrapped
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users(
		id, username, display_name, password_hash, role, disabled, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, user.DisplayName,
		passwordHash, user.Role, user.Disabled, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create bootstrap administrator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO auth_bootstrap(singleton, completed_by, completed_at) VALUES(1, ?, ?)", user.ID, formatTime(user.CreatedAt)); err != nil {
		return fmt.Errorf("record administrator bootstrap: %w", err)
	}
	if err := insertSession(ctx, tx, session, remote); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]string{"username": user.Username})
	if err := insertAuthAudit(ctx, tx, user.ID, string(user.Role), "auth.bootstrap", "user", user.ID, remote, details, user.CreatedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit administrator bootstrap: %w", err)
	}
	return nil
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (auth.User, string, error) {
	var user auth.User
	var passwordHash, createdAt, updatedAt string
	if err := s.db.QueryRowContext(ctx, `SELECT id, username, display_name, password_hash, role,
		disabled, created_at, updated_at FROM users WHERE username = ? COLLATE NOCASE`, username).Scan(
		&user.ID, &user.Username, &user.DisplayName, &passwordHash, &user.Role,
		&user.Disabled, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.User{}, "", auth.ErrNotFound
		}
		return auth.User{}, "", fmt.Errorf("find authentication user: %w", err)
	}
	var err error
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return auth.User{}, "", err
	}
	if user.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return auth.User{}, "", err
	}
	return user, passwordHash, nil
}

func (s *Store) ListUsers(ctx context.Context, limit int) ([]auth.User, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, role, disabled, created_at, updated_at
		FROM users ORDER BY username LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list authentication users: %w", err)
	}
	defer rows.Close()
	users := []auth.User{}
	for rows.Next() {
		var user auth.User
		var createdAt, updatedAt string
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		user.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		user.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, user auth.User, passwordHash, actorID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO users(id, username, display_name, password_hash, role, disabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, 0, ?, ?)`, user.ID, user.Username, user.DisplayName, passwordHash, user.Role, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create authentication user: %w", err)
	}
	details, _ := json.Marshal(map[string]string{"username": user.Username, "role": string(user.Role)})
	if err := insertAuthAudit(ctx, tx, actorID, string(auth.RoleAdministrator), "user.create", "user", user.ID, "", details, user.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateUser(ctx context.Context, id string, request auth.UpdateUserRequest, actorID, currentUserID string) (auth.User, error) {
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback()
	var current auth.User
	if err := tx.QueryRowContext(ctx, `SELECT id, username, display_name, role, disabled, created_at, updated_at FROM users WHERE id = ?`, id).Scan(
		&current.ID, &current.Username, &current.DisplayName, &current.Role, &current.Disabled, new(string), new(string)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.User{}, auth.ErrNotFound
		}
		return auth.User{}, err
	}
	if current.Role == auth.RoleAdministrator && (request.Role != auth.RoleAdministrator || request.Disabled) {
		var activeAdministrators int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'administrator' AND disabled = 0`).Scan(&activeAdministrators); err != nil {
			return auth.User{}, err
		}
		if activeAdministrators <= 1 {
			return auth.User{}, storage.ErrConflict
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE users SET display_name = ?, role = ?, disabled = ?, updated_at = ? WHERE id = ?`, request.DisplayName, request.Role, request.Disabled, formatTime(now), id)
	if err != nil {
		return auth.User{}, err
	}
	if err := requireAffected(result, auth.ErrNotFound); err != nil {
		return auth.User{}, err
	}
	if request.Disabled || request.Role != current.Role {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, formatTime(now), id); err != nil {
			return auth.User{}, err
		}
	}
	details, _ := json.Marshal(map[string]any{"role": request.Role, "disabled": request.Disabled})
	if err := insertAuthAudit(ctx, tx, actorID, string(auth.RoleAdministrator), "user.update", "user", id, "", details, now); err != nil {
		return auth.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.User{}, err
	}
	users, err := s.ListUsers(ctx, 500)
	if err != nil {
		return auth.User{}, err
	}
	for _, user := range users {
		if user.ID == id {
			return user, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

func (s *Store) CreateSession(ctx context.Context, session auth.Session, remote string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authentication session: %w", err)
	}
	defer tx.Rollback()
	if err := insertSession(ctx, tx, session, remote); err != nil {
		return err
	}
	if err := insertAuthAudit(ctx, tx, session.User.ID, string(session.User.Role), "auth.login", "session", session.ID, remote, json.RawMessage(`{}`), session.CreatedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authentication session: %w", err)
	}
	return nil
}

func insertSession(ctx context.Context, tx *sql.Tx, session auth.Session, remote string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO auth_sessions(
		id, token_hash, csrf_hash, user_id, remote_address, created_at, last_seen_at, expires_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.TokenHash, session.CSRFHash,
		session.User.ID, remote, formatTime(session.CreatedAt), formatTime(session.LastSeenAt), formatTime(session.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create authentication session: %w", err)
	}
	return nil
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (auth.Session, error) {
	var session auth.Session
	var createdAt, lastSeenAt, expiresAt, userCreatedAt, userUpdatedAt string
	var reauthenticatedUntil, revokedAt sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT s.id, s.token_hash, s.csrf_hash,
		s.created_at, s.last_seen_at, s.expires_at, s.reauthenticated_until, s.revoked_at,
		u.id, u.username, u.display_name, u.role, u.disabled, u.created_at, u.updated_at
		FROM auth_sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash).Scan(
		&session.ID, &session.TokenHash, &session.CSRFHash, &createdAt, &lastSeenAt, &expiresAt,
		&reauthenticatedUntil, &revokedAt, &session.User.ID, &session.User.Username,
		&session.User.DisplayName, &session.User.Role, &session.User.Disabled,
		&userCreatedAt, &userUpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.Session{}, auth.ErrNotFound
		}
		return auth.Session{}, fmt.Errorf("read authentication session: %w", err)
	}
	var err error
	if session.CreatedAt, err = parseTime(createdAt); err != nil {
		return auth.Session{}, err
	}
	if session.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return auth.Session{}, err
	}
	if session.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return auth.Session{}, err
	}
	if session.User.CreatedAt, err = parseTime(userCreatedAt); err != nil {
		return auth.Session{}, err
	}
	if session.User.UpdatedAt, err = parseTime(userUpdatedAt); err != nil {
		return auth.Session{}, err
	}
	if reauthenticatedUntil.Valid {
		if session.ReauthenticatedUntil, err = parseTime(reauthenticatedUntil.String); err != nil {
			return auth.Session{}, err
		}
	}
	if revokedAt.Valid {
		if session.RevokedAt, err = parseTime(revokedAt.String); err != nil {
			return auth.Session{}, err
		}
	}
	return session, nil
}

func (s *Store) RotateSessionCSRF(ctx context.Context, sessionID string, csrfHash []byte, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET csrf_hash = ?, last_seen_at = ?
		WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`, csrfHash, formatTime(now), sessionID, formatTime(now))
	if err != nil {
		return fmt.Errorf("rotate session CSRF token: %w", err)
	}
	return requireAffected(result, auth.ErrSessionExpired)
}

func (s *Store) MarkSessionReauthenticated(ctx context.Context, sessionID string, until, now time.Time, remote string) error {
	return s.sessionMutationWithAudit(ctx, sessionID, now, remote, "auth.reauthenticate",
		"UPDATE auth_sessions SET reauthenticated_until = ?, last_seen_at = ? WHERE id = ? AND revoked_at IS NULL AND expires_at > ?",
		formatTime(until), formatTime(now), sessionID, formatTime(now))
}

func (s *Store) RevokeSession(ctx context.Context, sessionID string, now time.Time, remote string) error {
	return s.sessionMutationWithAudit(ctx, sessionID, now, remote, "auth.logout",
		"UPDATE auth_sessions SET revoked_at = ?, last_seen_at = ? WHERE id = ? AND revoked_at IS NULL",
		formatTime(now), formatTime(now), sessionID)
}

func (s *Store) sessionMutationWithAudit(ctx context.Context, sessionID string, now time.Time, remote, action, statement string, arguments ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session mutation: %w", err)
	}
	defer tx.Rollback()
	var userID, role string
	if err := tx.QueryRowContext(ctx, `SELECT u.id, u.role FROM auth_sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, sessionID).Scan(&userID, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		return fmt.Errorf("read session principal: %w", err)
	}
	result, err := tx.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return fmt.Errorf("mutate authentication session: %w", err)
	}
	if err := requireAffected(result, auth.ErrSessionExpired); err != nil {
		return err
	}
	if err := insertAuthAudit(ctx, tx, userID, role, action, "session", sessionID, remote, json.RawMessage(`{}`), now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session mutation: %w", err)
	}
	return nil
}

func insertAuthAudit(ctx context.Context, tx *sql.Tx, actorID, actorRole, action, targetType, targetID, remote string, details json.RawMessage, now time.Time) error {
	id, err := NewID("audit")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(
		id, actor_id, actor_role, action, target_type, target_id, remote_address, details, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, actorID, actorRole, action,
		targetType, targetID, remote, string(normalizeJSON(details)), formatTime(now))
	if err != nil {
		return fmt.Errorf("append authentication audit event: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(timestampFormat) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(timestampFormat, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse durable timestamp: %w", err)
	}
	return parsed, nil
}

func requireAffected(result sql.Result, failure error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected authentication rows: %w", err)
	}
	if affected != 1 {
		return failure
	}
	return nil
}
