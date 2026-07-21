package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) CreateConfigDraft(ctx context.Context, request appconfig.CreateDraftRequest) (appconfig.Draft, error) {
	if request.Operation == "" {
		request.Operation = "apply"
	}
	if err := validateStoredScope(request.Scope); err != nil || request.BaseScopeVersion < 0 || !validDraftActorReason(request.AuthorID, request.Reason) || validateDraftEntries(request.Entries) != nil || validateDraftUnknownEntries(request.Operation, request.UnknownEntries) != nil {
		return appconfig.Draft{}, storage.ErrInvalid
	}
	id, err := NewID("configdraft")
	if err != nil {
		return appconfig.Draft{}, err
	}
	now := s.now()
	draft := appconfig.Draft{
		ID: id, Scope: request.Scope, Operation: request.Operation, State: appconfig.DraftOpen,
		BaseScopeVersion: request.BaseScopeVersion, Version: 1, AuthorID: request.AuthorID,
		Reason: strings.TrimSpace(request.Reason), Entries: cloneDraftEntries(request.Entries),
		UnknownEntries: cloneImportValues(request.UnknownEntries),
		CreatedAt:      now, UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.Draft{}, fmt.Errorf("begin configuration draft: %w", err)
	}
	defer tx.Rollback()
	currentVersion, err := configScopeVersionTx(ctx, tx, request.Scope)
	if err != nil {
		return appconfig.Draft{}, err
	}
	if currentVersion != request.BaseScopeVersion {
		return appconfig.Draft{}, storage.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_drafts(id, scope_kind, scope_id, operation, state,
		base_scope_version, version, author_id, reason, created_at, updated_at)
		VALUES(?, ?, ?, ?, 'draft', ?, 1, ?, ?, ?, ?)`, draft.ID, draft.Scope.Kind,
		draft.Scope.ID, draft.Operation, draft.BaseScopeVersion, draft.AuthorID, draft.Reason,
		now.Format(timestampFormat), now.Format(timestampFormat))
	if err != nil {
		return appconfig.Draft{}, fmt.Errorf("insert configuration draft: %w", err)
	}
	if err := replaceDraftEntriesTx(ctx, tx, draft.ID, draft.Entries); err != nil {
		return appconfig.Draft{}, err
	}
	if err := insertDraftUnknownEntriesTx(ctx, tx, draft.ID, draft.UnknownEntries); err != nil {
		return appconfig.Draft{}, err
	}
	if err := appendDraftAuditTx(ctx, tx, s.now, draft.AuthorID, "create", draft, draft.Reason); err != nil {
		return appconfig.Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.Draft{}, fmt.Errorf("commit configuration draft: %w", err)
	}
	return draft, nil
}

func (s *Store) GetConfigDraft(ctx context.Context, id string) (appconfig.Draft, error) {
	if strings.TrimSpace(id) == "" {
		return appconfig.Draft{}, storage.ErrInvalid
	}
	draft, err := scanConfigDraft(s.db.QueryRowContext(ctx, configDraftSelect+" WHERE id = ?", id))
	if err != nil {
		return appconfig.Draft{}, err
	}
	draft.Entries, err = s.configDraftEntries(ctx, draft.ID)
	if err != nil {
		return appconfig.Draft{}, err
	}
	draft.UnknownEntries, err = s.configDraftUnknownEntries(ctx, draft.ID)
	if err != nil {
		return appconfig.Draft{}, err
	}
	return draft, nil
}

const configDraftSelect = `SELECT id, scope_kind, scope_id, operation, state, base_scope_version,
	version, author_id, reviewer_id, reason, applied_revision_id, created_at, updated_at FROM config_drafts`

func scanConfigDraft(row scanner) (appconfig.Draft, error) {
	var draft appconfig.Draft
	var scopeKind, state, created, updated string
	if err := row.Scan(&draft.ID, &scopeKind, &draft.Scope.ID, &draft.Operation, &state,
		&draft.BaseScopeVersion, &draft.Version, &draft.AuthorID, &draft.ReviewerID,
		&draft.Reason, &draft.AppliedRevisionID, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appconfig.Draft{}, storage.ErrNotFound
		}
		return appconfig.Draft{}, fmt.Errorf("scan configuration draft: %w", err)
	}
	draft.Scope.Kind = appconfig.ScopeKind(scopeKind)
	draft.State = appconfig.DraftState(state)
	draft.CreatedAt, _ = time.Parse(timestampFormat, created)
	draft.UpdatedAt, _ = time.Parse(timestampFormat, updated)
	return draft, nil
}

func (s *Store) ListConfigDrafts(ctx context.Context, scope appconfig.ScopeRef, limit int) ([]appconfig.Draft, error) {
	if err := validateStoredScope(scope); err != nil {
		return nil, err
	}
	limit = boundedLimit(limit, 50, 200)
	rows, err := s.db.QueryContext(ctx, configDraftSelect+` WHERE scope_kind = ? AND scope_id = ?
		ORDER BY updated_at DESC, id LIMIT ?`, scope.Kind, scope.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list configuration drafts: %w", err)
	}
	result := make([]appconfig.Draft, 0)
	for rows.Next() {
		draft, scanErr := scanConfigDraft(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		result = append(result, draft)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("list configuration drafts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close configuration draft rows: %w", err)
	}
	for index := range result {
		result[index].Entries, err = s.configDraftEntries(ctx, result[index].ID)
		if err != nil {
			return nil, err
		}
		result[index].UnknownEntries, err = s.configDraftUnknownEntries(ctx, result[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) UpdateConfigDraft(ctx context.Context, request appconfig.UpdateDraftRequest) (appconfig.Draft, error) {
	if strings.TrimSpace(request.ID) == "" || request.ExpectedVersion < 1 || !validDraftActorReason(request.ActorID, request.Reason) || validateDraftEntries(request.Entries) != nil {
		return appconfig.Draft{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.Draft{}, fmt.Errorf("begin update configuration draft: %w", err)
	}
	defer tx.Rollback()
	draft, err := scanConfigDraft(tx.QueryRowContext(ctx, configDraftSelect+" WHERE id = ?", request.ID))
	if err != nil {
		return appconfig.Draft{}, err
	}
	if draft.State != appconfig.DraftOpen || draft.Version != request.ExpectedVersion {
		return appconfig.Draft{}, storage.ErrConflict
	}
	currentVersion, err := configScopeVersionTx(ctx, tx, draft.Scope)
	if err != nil {
		return appconfig.Draft{}, err
	}
	if currentVersion != draft.BaseScopeVersion {
		return appconfig.Draft{}, storage.ErrConflict
	}
	draft.Version++
	draft.Reason = strings.TrimSpace(request.Reason)
	draft.Entries = cloneDraftEntries(request.Entries)
	draft.UpdatedAt = s.now()
	if _, err := tx.ExecContext(ctx, `UPDATE config_drafts SET version = ?, reason = ?, updated_at = ?
		WHERE id = ? AND state = 'draft' AND version = ?`, draft.Version, draft.Reason,
		draft.UpdatedAt.Format(timestampFormat), draft.ID, request.ExpectedVersion); err != nil {
		return appconfig.Draft{}, fmt.Errorf("update configuration draft: %w", err)
	}
	if err := replaceDraftEntriesTx(ctx, tx, draft.ID, draft.Entries); err != nil {
		return appconfig.Draft{}, err
	}
	if err := appendDraftAuditTx(ctx, tx, s.now, request.ActorID, "update", draft, draft.Reason); err != nil {
		return appconfig.Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.Draft{}, fmt.Errorf("commit configuration draft update: %w", err)
	}
	return s.GetConfigDraft(ctx, draft.ID)
}

func (s *Store) TransitionConfigDraft(ctx context.Context, request appconfig.TransitionDraftRequest) (appconfig.Draft, error) {
	if strings.TrimSpace(request.ID) == "" || request.ExpectedVersion < 1 || !validDraftActorReason(request.ActorID, request.Reason) {
		return appconfig.Draft{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.Draft{}, fmt.Errorf("begin transition configuration draft: %w", err)
	}
	defer tx.Rollback()
	draft, err := scanConfigDraft(tx.QueryRowContext(ctx, configDraftSelect+" WHERE id = ?", request.ID))
	if err != nil {
		return appconfig.Draft{}, err
	}
	if draft.Version != request.ExpectedVersion {
		return appconfig.Draft{}, storage.ErrConflict
	}
	valid := draft.State == appconfig.DraftOpen && request.Target == appconfig.DraftReviewed ||
		(draft.State == appconfig.DraftOpen || draft.State == appconfig.DraftReviewed) && request.Target == appconfig.DraftDiscarded
	if !valid {
		return appconfig.Draft{}, storage.ErrInvalid
	}
	currentVersion, err := configScopeVersionTx(ctx, tx, draft.Scope)
	if err != nil {
		return appconfig.Draft{}, err
	}
	if currentVersion != draft.BaseScopeVersion {
		return appconfig.Draft{}, storage.ErrConflict
	}
	draft.State = request.Target
	draft.Version++
	draft.Reason = strings.TrimSpace(request.Reason)
	draft.UpdatedAt = s.now()
	if request.Target == appconfig.DraftReviewed {
		draft.ReviewerID = request.ActorID
	}
	result, err := tx.ExecContext(ctx, `UPDATE config_drafts SET state = ?, version = ?, reviewer_id = ?,
		reason = ?, updated_at = ? WHERE id = ? AND version = ?`, draft.State, draft.Version,
		draft.ReviewerID, draft.Reason, draft.UpdatedAt.Format(timestampFormat), draft.ID, request.ExpectedVersion)
	if err != nil {
		return appconfig.Draft{}, fmt.Errorf("transition configuration draft: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return appconfig.Draft{}, storage.ErrConflict
	}
	if err := appendDraftAuditTx(ctx, tx, s.now, request.ActorID, string(request.Target), draft, draft.Reason); err != nil {
		return appconfig.Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.Draft{}, fmt.Errorf("commit configuration draft transition: %w", err)
	}
	draft.Entries, err = s.configDraftEntries(ctx, draft.ID)
	if err != nil {
		return appconfig.Draft{}, err
	}
	draft.UnknownEntries, err = s.configDraftUnknownEntries(ctx, draft.ID)
	if err != nil {
		return appconfig.Draft{}, err
	}
	return draft, nil
}

func validDraftActorReason(actorID, reason string) bool {
	return len(strings.TrimSpace(actorID)) >= 1 && len(actorID) <= 256 && len(strings.TrimSpace(reason)) >= 1 && len(reason) <= 1000
}

func validateDraftEntries(entries []appconfig.DraftEntry) error {
	if len(entries) < 1 || len(entries) > 500 {
		return storage.ErrInvalid
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Key) == "" || len(entry.Key) > 256 || seen[entry.Key] {
			return storage.ErrInvalid
		}
		seen[entry.Key] = true
		if entry.Reset {
			if entry.Configured || len(entry.Value) != 0 {
				return storage.ErrInvalid
			}
			continue
		}
		if !entry.Configured {
			return storage.ErrInvalid
		}
		if entry.Secret {
			if len(entry.Value) != 0 {
				return storage.ErrInvalid
			}
		} else if len(entry.Value) == 0 || len(entry.Value) > 1<<20 || !json.Valid(entry.Value) {
			return storage.ErrInvalid
		}
	}
	return nil
}

func validateDraftUnknownEntries(operation string, entries []appconfig.ImportValue) error {
	if (operation != "apply" && operation != "import") ||
		(operation != "import" && len(entries) != 0) || len(entries) > 500 {
		return storage.ErrInvalid
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		validOpaqueValue := len(entry.Value) > 0 && len(entry.Value) <= 1<<20 && json.Valid(entry.Value)
		validRedactedSecret := len(entry.Value) == 0 && entry.Secret && entry.Redacted
		if strings.TrimSpace(entry.Key) == "" || len(entry.Key) > 256 || seen[entry.Key] ||
			(!validOpaqueValue && !validRedactedSecret) || (entry.Secret && len(entry.Value) != 0) {
			return storage.ErrInvalid
		}
		seen[entry.Key] = true
	}
	return nil
}

func cloneDraftEntries(entries []appconfig.DraftEntry) []appconfig.DraftEntry {
	result := append([]appconfig.DraftEntry(nil), entries...)
	for index := range result {
		result[index].Value = append(json.RawMessage(nil), result[index].Value...)
	}
	return result
}

func cloneImportValues(entries []appconfig.ImportValue) []appconfig.ImportValue {
	result := append([]appconfig.ImportValue(nil), entries...)
	for index := range result {
		result[index].Value = append(json.RawMessage(nil), result[index].Value...)
	}
	return result
}

func replaceDraftEntriesTx(ctx context.Context, tx *sql.Tx, draftID string, entries []appconfig.DraftEntry) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM config_draft_entries WHERE draft_id = ?", draftID); err != nil {
		return fmt.Errorf("replace configuration draft entries: %w", err)
	}
	for _, entry := range entries {
		var value any
		if len(entry.Value) != 0 && !entry.Secret {
			value = string(entry.Value)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_draft_entries(
			draft_id, setting_key, value_json, reset_value, secret, configured)
			VALUES(?, ?, ?, ?, ?, ?)`, draftID, entry.Key, value, entry.Reset, entry.Secret, entry.Configured); err != nil {
			return fmt.Errorf("insert configuration draft entry %s: %w", entry.Key, err)
		}
	}
	return nil
}

func insertDraftUnknownEntriesTx(ctx context.Context, tx *sql.Tx, draftID string, entries []appconfig.ImportValue) error {
	for _, entry := range entries {
		var value any
		if len(entry.Value) != 0 {
			value = string(entry.Value)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_import_unknown_entries(
			draft_id, setting_key, value_json, configured, secret, redacted)
			VALUES(?, ?, ?, ?, ?, ?)`, draftID, entry.Key, value, entry.Configured, entry.Secret, entry.Redacted); err != nil {
			return fmt.Errorf("preserve unknown configuration import entry %s: %w", entry.Key, err)
		}
	}
	return nil
}

func (s *Store) configDraftEntries(ctx context.Context, draftID string) ([]appconfig.DraftEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key, value_json, reset_value, secret, configured
		FROM config_draft_entries WHERE draft_id = ? ORDER BY setting_key`, draftID)
	if err != nil {
		return nil, fmt.Errorf("list configuration draft entries: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.DraftEntry, 0)
	for rows.Next() {
		var entry appconfig.DraftEntry
		var value sql.NullString
		if err := rows.Scan(&entry.Key, &value, &entry.Reset, &entry.Secret, &entry.Configured); err != nil {
			return nil, fmt.Errorf("scan configuration draft entry: %w", err)
		}
		if value.Valid && !entry.Secret {
			entry.Value = json.RawMessage(value.String)
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *Store) configDraftUnknownEntries(ctx context.Context, draftID string) ([]appconfig.ImportValue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key, value_json, configured, secret, redacted FROM config_import_unknown_entries
		WHERE draft_id = ? ORDER BY setting_key`, draftID)
	if err != nil {
		return nil, fmt.Errorf("list preserved unknown configuration entries: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.ImportValue, 0)
	for rows.Next() {
		var entry appconfig.ImportValue
		var value sql.NullString
		if err := rows.Scan(&entry.Key, &value, &entry.Configured, &entry.Secret, &entry.Redacted); err != nil {
			return nil, fmt.Errorf("scan preserved unknown configuration entry: %w", err)
		}
		if value.Valid {
			entry.Value = json.RawMessage(value.String)
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func configScopeVersionTx(ctx context.Context, tx *sql.Tx, scope appconfig.ScopeRef) (int64, error) {
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT version FROM config_scope_heads
		WHERE scope_kind = ? AND scope_id = ?`, scope.Kind, scope.ID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read configuration scope version: %w", err)
	}
	return version, nil
}

func appendDraftAuditTx(ctx context.Context, tx *sql.Tx, now func() time.Time, actorID, action string, draft appconfig.Draft, reason string) error {
	keys := make([]map[string]any, 0, len(draft.Entries))
	for _, entry := range draft.Entries {
		keys = append(keys, map[string]any{"key": entry.Key, "secret": entry.Secret, "reset": entry.Reset})
	}
	details, _ := json.Marshal(map[string]any{
		"scope": draft.Scope, "state": draft.State, "version": draft.Version,
		"base_scope_version": draft.BaseScopeVersion, "reason": reason, "entries": keys,
		"preserved_unknown_count": len(draft.UnknownEntries),
	})
	return appendAuditTx(ctx, tx, now, audit.AppendRequest{
		ActorID: actorID, ActorRole: "administrator", Action: "config_draft." + action,
		TargetType: "config_draft", TargetID: draft.ID, Details: details,
	})
}

func (s *Store) SaveConfigCheck(ctx context.Context, check appconfig.CheckResult) (appconfig.CheckResult, error) {
	if strings.TrimSpace(check.DraftID) == "" || check.DraftVersion < 1 || strings.TrimSpace(check.Handler) == "" || len(check.Handler) > 256 || len(check.Result) == 0 || len(check.Result) > 1<<20 || !json.Valid(check.Result) {
		return appconfig.CheckResult{}, storage.ErrInvalid
	}
	switch check.Kind {
	case "validation", "dry_run", "prerequisite":
	default:
		return appconfig.CheckResult{}, storage.ErrInvalid
	}
	switch check.Status {
	case "passed", "failed", "unavailable":
	default:
		return appconfig.CheckResult{}, storage.ErrInvalid
	}
	if check.ID == "" {
		id, err := NewID("configcheck")
		if err != nil {
			return appconfig.CheckResult{}, err
		}
		check.ID = id
	}
	check.CreatedAt = s.now()
	var expires any
	if check.ExpiresAt != nil {
		expires = check.ExpiresAt.UTC().Format(timestampFormat)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO config_checks(id, draft_id, draft_version, kind, handler,
		status, result, created_at, expires_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, check.ID,
		check.DraftID, check.DraftVersion, check.Kind, check.Handler, check.Status, string(check.Result),
		check.CreatedAt.Format(timestampFormat), expires)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return appconfig.CheckResult{}, storage.ErrNotFound
		}
		return appconfig.CheckResult{}, fmt.Errorf("insert configuration check: %w", err)
	}
	check.Sequence, _ = result.LastInsertId()
	return check, nil
}

func (s *Store) ListConfigChecks(ctx context.Context, draftID string, limit int) ([]appconfig.CheckResult, error) {
	if strings.TrimSpace(draftID) == "" {
		return nil, storage.ErrInvalid
	}
	limit = boundedLimit(limit, 50, 200)
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, id, draft_id, draft_version, kind, handler, status,
		result, created_at, expires_at FROM config_checks WHERE draft_id = ? ORDER BY sequence DESC LIMIT ?`, draftID, limit)
	if err != nil {
		return nil, fmt.Errorf("list configuration checks: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.CheckResult, 0)
	for rows.Next() {
		var check appconfig.CheckResult
		var raw, created string
		var expires sql.NullString
		if err := rows.Scan(&check.Sequence, &check.ID, &check.DraftID, &check.DraftVersion, &check.Kind,
			&check.Handler, &check.Status, &raw, &created, &expires); err != nil {
			return nil, fmt.Errorf("scan configuration check: %w", err)
		}
		check.Result = json.RawMessage(raw)
		check.CreatedAt, _ = time.Parse(timestampFormat, created)
		if expires.Valid {
			parsed, parseErr := time.Parse(timestampFormat, expires.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse configuration check expiry: %w", parseErr)
			}
			check.ExpiresAt = &parsed
		}
		result = append(result, check)
	}
	return result, rows.Err()
}
