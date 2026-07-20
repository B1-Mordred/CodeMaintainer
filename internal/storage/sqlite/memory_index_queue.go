package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/memory"
)

func (s *Store) EnqueueProjectMemoryRebuild(ctx context.Context, scope memory.ProjectScope, actor string) (int, error) {
	if !scope.Valid() || strings.TrimSpace(actor) == "" {
		return 0, memory.ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, version FROM memory_records
		WHERE owner = ? AND repository = ? AND status = 'canonical' ORDER BY id`, scope.Owner, scope.Repository)
	if err != nil {
		return 0, err
	}
	type item struct {
		id      string
		version int64
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.version); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, value)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, value := range items {
		id, err := NewID("memoryindex")
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO memory_index_operations(id, record_id, owner, repository,
			action, record_version, state, next_attempt_at, created_at, updated_at)
			VALUES(?, ?, ?, ?, 'upsert', ?, 'pending', ?, ?, ?)
			ON CONFLICT(record_id, record_version, action) DO UPDATE SET state = 'pending', attempts = 0,
			last_error = '', next_attempt_at = excluded.next_attempt_at, lease_owner = '', lease_expires_at = NULL,
			updated_at = excluded.updated_at`, id, value.id, scope.Owner, scope.Repository, value.version,
			formatTime(now), formatTime(now), formatTime(now))
		if err != nil {
			return 0, err
		}
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: actor, ActorRole: "administrator", Action: "memory.rebuild_queued", TargetType: "memory_namespace",
		TargetID: scope.Namespace(),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (s *Store) ClaimMemoryIndexOperation(ctx context.Context, owner string, leaseDuration time.Duration) (memory.IndexOperation, error) {
	if strings.TrimSpace(owner) == "" || leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return memory.IndexOperation{}, memory.ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.IndexOperation{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `SELECT id, record_id, owner, repository, action, record_version,
		state, attempts, last_error, lease_owner, lease_expires_at, created_at, updated_at
		FROM memory_index_operations
		WHERE ((state IN ('pending', 'failed') AND next_attempt_at <= ?)
			OR (state = 'running' AND lease_expires_at <= ?))
		ORDER BY sequence LIMIT 1`, formatTime(now), formatTime(now))
	operation, err := scanMemoryIndexOperation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.IndexOperation{}, memory.ErrNoIndexOperation
	}
	if err != nil {
		return memory.IndexOperation{}, err
	}
	expires := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `UPDATE memory_index_operations SET state = 'running', attempts = attempts + 1,
		lease_owner = ?, lease_expires_at = ?, updated_at = ? WHERE id = ? AND
		(((state IN ('pending', 'failed')) AND next_attempt_at <= ?) OR (state = 'running' AND lease_expires_at <= ?))`,
		owner, formatTime(expires), formatTime(now), operation.ID, formatTime(now), formatTime(now))
	if err != nil {
		return memory.IndexOperation{}, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return memory.IndexOperation{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.IndexOperation{}, err
	}
	operation.State, operation.Attempts, operation.LeaseOwner, operation.LeaseExpires, operation.UpdatedAt = "running", operation.Attempts+1, owner, expires, now
	return operation, nil
}

func (s *Store) CompleteMemoryIndexOperation(ctx context.Context, id, owner string) error {
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE memory_index_operations SET state = 'completed', last_error = '',
		lease_owner = '', lease_expires_at = NULL, updated_at = ? WHERE id = ? AND state = 'running' AND lease_owner = ?`,
		formatTime(now), id, owner)
	if err != nil {
		return err
	}
	return requireMemoryAffected(result)
}

func (s *Store) FailMemoryIndexOperation(ctx context.Context, id, owner, failure string, retryAfter time.Duration) error {
	if retryAfter < time.Second || retryAfter > time.Hour {
		return memory.ErrInvalid
	}
	now := s.now()
	failure = sanitizeIndexFailure(failure)
	result, err := s.db.ExecContext(ctx, `UPDATE memory_index_operations SET state = 'failed', last_error = ?,
		next_attempt_at = ?, lease_owner = '', lease_expires_at = NULL, updated_at = ?
		WHERE id = ? AND state = 'running' AND lease_owner = ?`, failure, formatTime(now.Add(retryAfter)),
		formatTime(now), id, owner)
	if err != nil {
		return err
	}
	return requireMemoryAffected(result)
}

func scanMemoryIndexOperation(scanner interface{ Scan(...any) error }) (memory.IndexOperation, error) {
	var operation memory.IndexOperation
	var lease sql.NullString
	var created, updated string
	err := scanner.Scan(&operation.ID, &operation.RecordID, &operation.Scope.Owner, &operation.Scope.Repository,
		&operation.Action, &operation.RecordVersion, &operation.State, &operation.Attempts, &operation.LastError,
		&operation.LeaseOwner, &lease, &created, &updated)
	if err != nil {
		return memory.IndexOperation{}, err
	}
	operation.CreatedAt, err = parseTime(created)
	if err != nil {
		return memory.IndexOperation{}, err
	}
	operation.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return memory.IndexOperation{}, err
	}
	if lease.Valid {
		operation.LeaseExpires, err = parseTime(lease.String)
		if err != nil {
			return memory.IndexOperation{}, err
		}
	}
	return operation, nil
}

func sanitizeIndexFailure(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		value = value[:512]
	}
	if value == "" {
		return "index operation failed"
	}
	return value
}
