package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/memory"
)

func (s *Store) PutCandidate(ctx context.Context, scope memory.ProjectScope, candidate memory.Record) (memory.Record, error) {
	record, err := memory.PrepareCandidate(scope, candidate)
	if err != nil {
		return memory.Record{}, err
	}
	now := s.now()
	record.CreatedAt, record.UpdatedAt = now, now
	paths, _ := json.Marshal(record.AffectedPaths)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Record{}, fmt.Errorf("begin memory candidate: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_records(
		id, owner, repository, namespace, kind, content, content_hash, source_uri,
		base_commit, merged_commit, affected_paths, status, verified, secret_scan_pass,
		invalidation_rule, expires_at, version, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID,
		scope.Owner, scope.Repository, record.Namespace, record.Kind, record.Content,
		record.ContentHash, record.SourceURI, record.BaseCommit, record.MergedCommit,
		string(paths), record.Status, record.Verified, record.SecretScanPass,
		record.InvalidationRule, nullableMemoryTime(record.ExpiresAt), record.Version,
		formatTime(now), formatTime(now))
	if err != nil {
		return memory.Record{}, fmt.Errorf("insert memory candidate: %w", err)
	}
	if err := appendMemoryEvent(ctx, tx, record, "candidate_created", "controller", "automatic extraction remains quarantined", json.RawMessage(`{}`), now); err != nil {
		return memory.Record{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: "controller", ActorRole: "system", Action: "memory.candidate_created", TargetType: "memory", TargetID: record.ID}); err != nil {
		return memory.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.Record{}, fmt.Errorf("commit memory candidate: %w", err)
	}
	return record, nil
}

func (s *Store) Search(ctx context.Context, scope memory.ProjectScope, query string, limit int) ([]memory.Record, error) {
	if !scope.Valid() {
		return nil, memory.ErrScope
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 4096 {
		return nil, memory.ErrInvalid
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.db.QueryContext(ctx, memorySelect+` WHERE owner = ? AND repository = ?
		AND status = 'canonical' AND (expires_at IS NULL OR expires_at > ?)
		AND lower(content) LIKE lower(?) ESCAPE '\' ORDER BY updated_at DESC, id LIMIT ?`,
		scope.Owner, scope.Repository, formatTime(s.now()), "%"+escaped+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search project memory: %w", err)
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

func (s *Store) GetMemory(ctx context.Context, scope memory.ProjectScope, id string) (memory.Record, error) {
	if !scope.Valid() {
		return memory.Record{}, memory.ErrScope
	}
	row := s.db.QueryRowContext(ctx, memorySelect+" WHERE id = ? AND owner = ? AND repository = ?", id, scope.Owner, scope.Repository)
	return scanMemoryRecord(row)
}

func (s *Store) ListMemory(ctx context.Context, scope memory.ProjectScope, status memory.Status, limit int) ([]memory.Record, error) {
	if !scope.Valid() {
		return nil, memory.ErrScope
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := memorySelect + " WHERE owner = ? AND repository = ?"
	arguments := []any{scope.Owner, scope.Repository}
	if status != "" {
		if status != memory.StatusQuarantine && status != memory.StatusCanonical && status != memory.StatusStale && status != memory.StatusDeleted {
			return nil, memory.ErrInvalid
		}
		query += " AND status = ?"
		arguments = append(arguments, status)
	}
	query += " ORDER BY updated_at DESC, id LIMIT ?"
	arguments = append(arguments, limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list project memory: %w", err)
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

func (s *Store) CorrectMemory(ctx context.Context, scope memory.ProjectScope, id string, request memory.CorrectionRequest) (memory.Record, error) {
	if request.ActorID == "" || strings.TrimSpace(request.Rationale) == "" {
		return memory.Record{}, memory.ErrInvalid
	}
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return memory.Record{}, err
	}
	prepared, err := memory.PrepareCandidate(scope, memory.Record{ID: id, Content: request.Content, Kind: current.Kind, SourceURI: current.SourceURI, BaseCommit: current.BaseCommit, MergedCommit: current.MergedCommit, AffectedPaths: request.AffectedPaths})
	if err != nil {
		return memory.Record{}, err
	}
	now := s.now()
	paths, _ := json.Marshal(prepared.AffectedPaths)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Record{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET content = ?, content_hash = ?, affected_paths = ?,
		status = 'quarantine', verified = 0, secret_scan_pass = 1, invalidation_rule = ?, version = version + 1,
		updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ? AND status != 'deleted'`,
		prepared.Content, prepared.ContentHash, string(paths), request.InvalidationRule, formatTime(now), id,
		scope.Owner, scope.Repository, request.ExpectedVersion)
	if err != nil {
		return memory.Record{}, fmt.Errorf("correct memory: %w", err)
	}
	if err := requireMemoryAffected(result); err != nil {
		return memory.Record{}, err
	}
	current.Content, current.ContentHash, current.AffectedPaths = prepared.Content, prepared.ContentHash, prepared.AffectedPaths
	current.Status, current.Verified, current.SecretScanPass = memory.StatusQuarantine, false, true
	current.InvalidationRule, current.Version, current.UpdatedAt = request.InvalidationRule, request.ExpectedVersion+1, now
	if err := appendMemoryEvent(ctx, tx, current, "corrected", request.ActorID, request.Rationale, json.RawMessage(`{}`), now); err != nil {
		return memory.Record{}, err
	}
	if err := appendMemoryAudit(ctx, tx, s.now, request.ActorID, "memory.correct", id, request.Rationale); err != nil {
		return memory.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.Record{}, err
	}
	return current, nil
}

func (s *Store) VerifyMemory(ctx context.Context, scope memory.ProjectScope, id string, request memory.VerificationRequest) (memory.Record, error) {
	if !scope.Valid() || request.ActorID == "" || request.JobID == "" || request.ArtifactID == "" || !memory.ValidCommit(request.Commit) || request.Commit == "" {
		return memory.Record{}, memory.ErrInvalid
	}
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return memory.Record{}, err
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Record{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET verified = 1, version = version + 1,
		updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ? AND status = 'quarantine'`,
		formatTime(now), id, scope.Owner, scope.Repository, request.ExpectedVersion)
	if err != nil {
		return memory.Record{}, fmt.Errorf("verify memory: %w", err)
	}
	if err := requireMemoryAffected(result); err != nil {
		return memory.Record{}, err
	}
	current.Verified, current.Version, current.UpdatedAt = true, request.ExpectedVersion+1, now
	details, _ := json.Marshal(map[string]string{"job_id": request.JobID, "artifact_id": request.ArtifactID, "commit": request.Commit})
	if err := appendMemoryEvent(ctx, tx, current, "verified", request.ActorID, "deterministic verification evidence attached", details, now); err != nil {
		return memory.Record{}, err
	}
	if err := appendMemoryAudit(ctx, tx, s.now, request.ActorID, "memory.verify", id, "deterministic verification evidence attached"); err != nil {
		return memory.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.Record{}, err
	}
	return current, nil
}

func (s *Store) PromoteMemory(ctx context.Context, scope memory.ProjectScope, id string, request memory.PromotionRequest) (memory.Record, error) {
	if request.ActorID == "" || strings.TrimSpace(request.Rationale) == "" {
		return memory.Record{}, memory.ErrInvalid
	}
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return memory.Record{}, err
	}
	eligible := current.SecretScanPass && ((request.Basis == "deterministic_verification" && current.Verified) ||
		(request.Basis == "merged_pr" && request.MergedCommit != "" && memory.ValidCommit(request.MergedCommit)) || request.Basis == "human_approval")
	if !eligible || current.Status == memory.StatusDeleted {
		return memory.Record{}, memory.ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Record{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET status = 'canonical', merged_commit = ?,
		version = version + 1, updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ? AND status = 'quarantine'`,
		request.MergedCommit, formatTime(now), id, scope.Owner, scope.Repository, request.ExpectedVersion)
	if err != nil {
		return memory.Record{}, fmt.Errorf("promote memory: %w", err)
	}
	if err := requireMemoryAffected(result); err != nil {
		return memory.Record{}, err
	}
	current.Status, current.MergedCommit, current.Version, current.UpdatedAt = memory.StatusCanonical, request.MergedCommit, request.ExpectedVersion+1, now
	details, _ := json.Marshal(map[string]string{"basis": request.Basis})
	if err := appendMemoryEvent(ctx, tx, current, "promoted", request.ActorID, request.Rationale, details, now); err != nil {
		return memory.Record{}, err
	}
	if err := appendMemoryAudit(ctx, tx, s.now, request.ActorID, "memory.promote", id, request.Rationale); err != nil {
		return memory.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.Record{}, err
	}
	return current, nil
}

func (s *Store) Invalidate(ctx context.Context, scope memory.ProjectScope, id, actor string) (memory.Record, error) {
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return memory.Record{}, err
	}
	return s.InvalidateMemory(ctx, scope, id, memory.InvalidationRequest{ActorID: actor, Rationale: "record marked stale", ExpectedVersion: current.Version})
}

func (s *Store) InvalidateMemory(ctx context.Context, scope memory.ProjectScope, id string, request memory.InvalidationRequest) (memory.Record, error) {
	if !scope.Valid() || request.ActorID == "" || strings.TrimSpace(request.Rationale) == "" {
		return memory.Record{}, memory.ErrInvalid
	}
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return memory.Record{}, err
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Record{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET status = 'stale', version = version + 1,
		updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ? AND status IN ('quarantine', 'canonical')`,
		formatTime(now), id, scope.Owner, scope.Repository, request.ExpectedVersion)
	if err != nil {
		return memory.Record{}, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return memory.Record{}, err
	}
	current.Status, current.Version, current.UpdatedAt = memory.StatusStale, request.ExpectedVersion+1, now
	if err := appendMemoryEvent(ctx, tx, current, "invalidated", request.ActorID, request.Rationale, json.RawMessage(`{}`), now); err != nil {
		return memory.Record{}, err
	}
	if err := appendMemoryAudit(ctx, tx, s.now, request.ActorID, "memory.invalidate", id, request.Rationale); err != nil {
		return memory.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.Record{}, err
	}
	return current, nil
}

func (s *Store) Delete(ctx context.Context, scope memory.ProjectScope, id, actor string) error {
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return err
	}
	return s.DeleteMemory(ctx, scope, id, memory.DeletionRequest{ActorID: actor, Rationale: "operator deleted memory content", ExpectedVersion: current.Version})
}

func (s *Store) DeleteMemory(ctx context.Context, scope memory.ProjectScope, id string, request memory.DeletionRequest) error {
	if !scope.Valid() || request.ActorID == "" || strings.TrimSpace(request.Rationale) == "" {
		return memory.ErrInvalid
	}
	current, err := s.GetMemory(ctx, scope, id)
	if err != nil {
		return err
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET content = '', status = 'deleted', deleted_at = ?,
		version = version + 1, updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ? AND status != 'deleted'`,
		formatTime(now), formatTime(now), id, scope.Owner, scope.Repository, request.ExpectedVersion)
	if err != nil {
		return err
	}
	if err := requireMemoryAffected(result); err != nil {
		return err
	}
	current.Content, current.Status, current.Version, current.UpdatedAt = "", memory.StatusDeleted, request.ExpectedVersion+1, now
	current.DeletedAt = &now
	if err := appendMemoryEvent(ctx, tx, current, "deleted", request.ActorID, request.Rationale, json.RawMessage(`{}`), now); err != nil {
		return err
	}
	if err := appendMemoryAudit(ctx, tx, s.now, request.ActorID, "memory.delete", id, request.Rationale); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListMemoryEvents(ctx context.Context, scope memory.ProjectScope, id string, limit int) ([]memory.Event, error) {
	if !scope.Valid() {
		return nil, memory.ErrScope
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, record_id, owner, repository, action,
		actor_id, rationale, details, created_at FROM memory_events WHERE owner = ? AND repository = ?
		AND (? = '' OR record_id = ?) ORDER BY sequence DESC LIMIT ?`, scope.Owner, scope.Repository, id, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []memory.Event{}
	for rows.Next() {
		var event memory.Event
		var details, created string
		if err := rows.Scan(&event.Sequence, &event.RecordID, &event.Scope.Owner, &event.Scope.Repository, &event.Action, &event.ActorID, &event.Rationale, &details, &created); err != nil {
			return nil, err
		}
		event.Details = json.RawMessage(details)
		event.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) RecordRetrieval(ctx context.Context, trace memory.RetrievalTrace) (memory.RetrievalTrace, error) {
	if !trace.Scope.Valid() || strings.TrimSpace(trace.Query) == "" || len(trace.Query) > 4096 || trace.BudgetTokens < 0 || trace.BudgetTokens > 8192 || trace.AllocatedTokens < 0 || trace.AllocatedTokens > trace.BudgetTokens {
		return memory.RetrievalTrace{}, memory.ErrInvalid
	}
	if trace.ID == "" {
		id, err := NewID("retrieval")
		if err != nil {
			return memory.RetrievalTrace{}, err
		}
		trace.ID = id
	}
	hash := sha256.Sum256([]byte(trace.Query))
	trace.QueryHash = hex.EncodeToString(hash[:])
	trace.Namespace = trace.Scope.Namespace()
	trace.CreatedAt = s.now()
	candidates, _ := json.Marshal(trace.CandidateIDs)
	selected, _ := json.Marshal(trace.SelectedIDs)
	trajectory := normalizeJSON(trace.Trajectory)
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory_retrieval_traces(id, job_id, owner, repository,
		namespace, query_text, query_hash, candidate_ids, selected_ids, budget_tokens, allocated_tokens,
		trajectory, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, trace.ID, trace.JobID,
		trace.Scope.Owner, trace.Scope.Repository, trace.Namespace, trace.Query, trace.QueryHash,
		string(candidates), string(selected), trace.BudgetTokens, trace.AllocatedTokens, string(trajectory), formatTime(trace.CreatedAt))
	if err != nil {
		return memory.RetrievalTrace{}, fmt.Errorf("record memory retrieval: %w", err)
	}
	return trace, nil
}

func (s *Store) ListRetrievals(ctx context.Context, scope memory.ProjectScope, limit int) ([]memory.RetrievalTrace, error) {
	if !scope.Valid() {
		return nil, memory.ErrScope
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_id, owner, repository, namespace, query_text, query_hash,
		candidate_ids, selected_ids, budget_tokens, allocated_tokens, trajectory, created_at
		FROM memory_retrieval_traces WHERE owner = ? AND repository = ? ORDER BY created_at DESC LIMIT ?`, scope.Owner, scope.Repository, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	traces := []memory.RetrievalTrace{}
	for rows.Next() {
		var trace memory.RetrievalTrace
		var candidateJSON, selectedJSON, trajectory, created string
		if err := rows.Scan(&trace.ID, &trace.JobID, &trace.Scope.Owner, &trace.Scope.Repository, &trace.Namespace, &trace.Query, &trace.QueryHash, &candidateJSON, &selectedJSON, &trace.BudgetTokens, &trace.AllocatedTokens, &trajectory, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(candidateJSON), &trace.CandidateIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(selectedJSON), &trace.SelectedIDs); err != nil {
			return nil, err
		}
		trace.Trajectory = json.RawMessage(trajectory)
		trace.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, rows.Err()
}

const memorySelect = `SELECT id, owner, repository, namespace, kind, content, content_hash, source_uri,
	base_commit, merged_commit, affected_paths, status, verified, secret_scan_pass, invalidation_rule,
	expires_at, deleted_at, version, created_at, updated_at FROM memory_records`

func scanMemoryRecord(scanner interface{ Scan(...any) error }) (memory.Record, error) {
	var record memory.Record
	var paths, created, updated string
	var expires, deleted sql.NullString
	err := scanner.Scan(&record.ID, &record.Scope.Owner, &record.Scope.Repository, &record.Namespace, &record.Kind,
		&record.Content, &record.ContentHash, &record.SourceURI, &record.BaseCommit, &record.MergedCommit,
		&paths, &record.Status, &record.Verified, &record.SecretScanPass, &record.InvalidationRule,
		&expires, &deleted, &record.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Record{}, memory.ErrNotFound
	}
	if err != nil {
		return memory.Record{}, fmt.Errorf("scan memory record: %w", err)
	}
	if err := json.Unmarshal([]byte(paths), &record.AffectedPaths); err != nil {
		return memory.Record{}, err
	}
	record.CreatedAt, err = parseTime(created)
	if err != nil {
		return memory.Record{}, err
	}
	record.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return memory.Record{}, err
	}
	if expires.Valid {
		value, parseErr := parseTime(expires.String)
		if parseErr != nil {
			return memory.Record{}, parseErr
		}
		record.ExpiresAt = &value
	}
	if deleted.Valid {
		value, parseErr := parseTime(deleted.String)
		if parseErr != nil {
			return memory.Record{}, parseErr
		}
		record.DeletedAt = &value
	}
	return record, nil
}

func scanMemoryRows(rows *sql.Rows) ([]memory.Record, error) {
	records := []memory.Record{}
	for rows.Next() {
		record, err := scanMemoryRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func appendMemoryEvent(ctx context.Context, tx *sql.Tx, record memory.Record, action, actor, rationale string, details json.RawMessage, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO memory_events(record_id, owner, repository, action,
		actor_id, rationale, details, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, record.ID,
		record.Scope.Owner, record.Scope.Repository, action, actor, rationale, string(normalizeJSON(details)), formatTime(now))
	if err != nil {
		return fmt.Errorf("append memory event: %w", err)
	}
	return nil
}

func appendMemoryAudit(ctx context.Context, tx *sql.Tx, now func() time.Time, actor, action, id, rationale string) error {
	details, _ := json.Marshal(map[string]string{"rationale": rationale})
	return appendAuditTx(ctx, tx, now, audit.AppendRequest{ActorID: actor, ActorRole: "operator", Action: action, TargetType: "memory", TargetID: id, Details: details})
}

func requireMemoryAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return memory.ErrConflict
	}
	return nil
}
func nullableMemoryTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}
