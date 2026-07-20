package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func validateStoredScope(scope appconfig.ScopeRef) error {
	if err := scope.Validate(); err != nil {
		return storage.ErrInvalid
	}
	if scope.Kind == appconfig.ScopeBuiltIn {
		return storage.ErrInvalid
	}
	return nil
}

func (s *Store) GetConfigScope(ctx context.Context, scope appconfig.ScopeRef) (appconfig.ScopeState, error) {
	if err := validateStoredScope(scope); err != nil {
		return appconfig.ScopeState{}, err
	}
	state := appconfig.ScopeState{Scope: scope, Values: []appconfig.StoredValue{}}
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT version, revision_id, updated_at
		FROM config_scope_heads WHERE scope_kind = ? AND scope_id = ?`, scope.Kind, scope.ID).
		Scan(&state.Version, &state.RevisionID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("read configuration scope head: %w", err)
	}
	state.UpdatedAt, err = time.Parse(timestampFormat, updated)
	if err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("parse configuration scope timestamp: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key, value_json, configured, secret,
		version, revision_id, updated_at FROM config_scope_values
		WHERE scope_kind = ? AND scope_id = ? ORDER BY setting_key`, scope.Kind, scope.ID)
	if err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("list configuration scope values: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value appconfig.StoredValue
		var raw sql.NullString
		var configured, secret bool
		var valueUpdated string
		if err := rows.Scan(&value.Key, &raw, &configured, &secret, &value.Version, &value.RevisionID, &valueUpdated); err != nil {
			return appconfig.ScopeState{}, fmt.Errorf("scan configuration scope value: %w", err)
		}
		value.Scope = scope
		value.Configured = configured
		value.Secret = secret
		if raw.Valid && !secret {
			value.Value = json.RawMessage(raw.String)
		}
		value.UpdatedAt, err = time.Parse(timestampFormat, valueUpdated)
		if err != nil {
			return appconfig.ScopeState{}, fmt.Errorf("parse configuration value timestamp: %w", err)
		}
		state.Values = append(state.Values, value)
	}
	if err := rows.Err(); err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("list configuration scope values: %w", err)
	}
	return state, nil
}

func (s *Store) ApplyConfigScope(ctx context.Context, request appconfig.ApplyScopeRequest) (appconfig.RegistryRevision, appconfig.ScopeState, error) {
	if err := validateApplyScopeRequest(request); err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("begin configuration scope apply: %w", err)
	}
	defer tx.Rollback()
	currentVersion := int64(0)
	err = tx.QueryRowContext(ctx, `SELECT version FROM config_scope_heads
		WHERE scope_kind = ? AND scope_id = ?`, request.Scope.Kind, request.Scope.ID).Scan(&currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("read configuration scope version: %w", err)
	}
	if currentVersion != request.ExpectedVersion {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, storage.ErrConflict
	}
	if request.DraftID != "" {
		if err := validateAppliedDraftTx(ctx, tx, request); err != nil {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
		}
	}
	if request.RollbackOf != "" {
		var rollbackScopeKind, rollbackScopeID string
		if err := tx.QueryRowContext(ctx, `SELECT scope_kind, scope_id FROM config_registry_revisions WHERE id = ?`, request.RollbackOf).Scan(&rollbackScopeKind, &rollbackScopeID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return appconfig.RegistryRevision{}, appconfig.ScopeState{}, storage.ErrNotFound
			}
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("read rollback target: %w", err)
		}
		if rollbackScopeKind != string(request.Scope.Kind) || rollbackScopeID != request.Scope.ID {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, storage.ErrInvalid
		}
	}
	revisionID, err := NewID("configreg")
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
	}
	now := s.now()
	revision := appconfig.RegistryRevision{
		ID: revisionID, Scope: request.Scope, ScopeVersion: currentVersion + 1,
		ActorID: request.ActorID, ActorRole: request.ActorRole, Operation: request.Operation,
		Reason: request.Reason, RollbackOf: request.RollbackOf, DraftID: request.DraftID,
		Entries: make([]appconfig.RevisionEntry, 0, len(request.Changes)), CreatedAt: now,
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO config_registry_revisions(
		id, scope_kind, scope_id, scope_version, actor_id, actor_role, operation,
		reason, rollback_of, draft_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.Scope.Kind, revision.Scope.ID, revision.ScopeVersion,
		revision.ActorID, revision.ActorRole, revision.Operation, revision.Reason,
		revision.RollbackOf, revision.DraftID, now.Format(timestampFormat))
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("insert configuration registry revision: %w", err)
	}
	revision.Sequence, err = result.LastInsertId()
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("read configuration registry sequence: %w", err)
	}
	for _, change := range request.Changes {
		entry, existingVersion, entryErr := applyConfigChangeTx(ctx, tx, revision, change, now)
		if entryErr != nil {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, entryErr
		}
		_ = existingVersion
		revision.Entries = append(revision.Entries, entry)
	}
	if request.Scope.Kind == appconfig.ScopeSystem {
		if err := appendLegacyConfigProjectionTx(ctx, tx, request, now); err != nil {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_scope_heads(scope_kind, scope_id, version, revision_id, updated_at)
		VALUES(?, ?, ?, ?, ?) ON CONFLICT(scope_kind, scope_id) DO UPDATE SET
		version = excluded.version, revision_id = excluded.revision_id, updated_at = excluded.updated_at`,
		request.Scope.Kind, request.Scope.ID, revision.ScopeVersion, revision.ID, now.Format(timestampFormat))
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("update configuration scope head: %w", err)
	}
	if request.DraftID != "" {
		result, updateErr := tx.ExecContext(ctx, `UPDATE config_drafts SET state = 'applied',
			version = version + 1, applied_revision_id = ?, updated_at = ?
			WHERE id = ? AND state = 'reviewed' AND version = ?`, revision.ID,
			now.Format(timestampFormat), request.DraftID, request.DraftVersion)
		if updateErr != nil {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("mark configuration draft applied: %w", updateErr)
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return appconfig.RegistryRevision{}, appconfig.ScopeState{}, storage.ErrConflict
		}
	}
	keys := make([]map[string]any, 0, len(request.Changes))
	for _, change := range request.Changes {
		keys = append(keys, map[string]any{"key": change.Key, "configured": change.Configured, "secret": change.Secret})
	}
	details, _ := json.Marshal(map[string]any{
		"scope": request.Scope, "scope_version": revision.ScopeVersion,
		"operation": revision.Operation, "reason": revision.Reason, "changes": keys,
	})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: revision.ActorID, ActorRole: revision.ActorRole,
		Action: "config_registry." + revision.Operation, TargetType: "config_registry_revision",
		TargetID: revision.ID, Details: details,
	}); err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, fmt.Errorf("commit configuration scope apply: %w", err)
	}
	state, err := s.GetConfigScope(ctx, request.Scope)
	if err != nil {
		return appconfig.RegistryRevision{}, appconfig.ScopeState{}, err
	}
	return revision, state, nil
}

func appendLegacyConfigProjectionTx(ctx context.Context, tx *sql.Tx, request appconfig.ApplyScopeRequest, now time.Time) error {
	current, err := scanRevision(tx.QueryRowContext(ctx, revisionSelect+" ORDER BY sequence DESC LIMIT 1"))
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var document appconfig.System
	if err := json.Unmarshal(current.After, &document); err != nil {
		return fmt.Errorf("decode legacy configuration projection: %w", err)
	}
	afterDocument, err := appconfig.ApplySystemChanges(document, request.Changes)
	if err != nil {
		return fmt.Errorf("apply legacy configuration projection: %w", err)
	}
	after, err := json.Marshal(afterDocument)
	if err != nil {
		return fmt.Errorf("encode legacy configuration projection: %w", err)
	}
	diff, err := appconfig.Diff(current.After, after)
	if err != nil {
		return err
	}
	if string(diff) == "[]" {
		return nil
	}
	id, err := NewID("config")
	if err != nil {
		return err
	}
	validation := `{"valid":true,"errors":[]}`
	if _, err := tx.ExecContext(ctx, `INSERT INTO config_revisions(id, actor_id, schema_version,
		before_document, after_document, document_diff, validation_result, rollback_of, reason, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, '', ?, ?)`, id, request.ActorID, appconfig.SchemaVersion,
		string(current.After), string(after), string(diff), validation, request.Reason,
		now.Format(timestampFormat)); err != nil {
		return fmt.Errorf("insert legacy configuration projection: %w", err)
	}
	return nil
}

func validateApplyScopeRequest(request appconfig.ApplyScopeRequest) error {
	if err := validateStoredScope(request.Scope); err != nil || request.ExpectedVersion < 0 {
		return storage.ErrInvalid
	}
	if strings.TrimSpace(request.ActorID) == "" || strings.TrimSpace(request.ActorRole) == "" || len(request.ActorID) > 256 || len(request.ActorRole) > 64 {
		return storage.ErrInvalid
	}
	if len(strings.TrimSpace(request.Reason)) < 1 || len(request.Reason) > 1000 {
		return storage.ErrInvalid
	}
	switch request.Operation {
	case "apply", "rollback", "import", "reset":
	default:
		return storage.ErrInvalid
	}
	if len(request.Changes) < 1 || len(request.Changes) > 500 || (request.DraftID == "" && request.DraftVersion != 0) || (request.DraftID != "" && request.DraftVersion < 1) {
		return storage.ErrInvalid
	}
	seen := make(map[string]bool, len(request.Changes))
	for _, change := range request.Changes {
		if strings.TrimSpace(change.Key) == "" || len(change.Key) > 256 || seen[change.Key] {
			return storage.ErrInvalid
		}
		seen[change.Key] = true
		if change.Secret {
			if len(change.Value) != 0 {
				return storage.ErrInvalid
			}
		} else if change.Configured {
			if len(change.Value) == 0 || len(change.Value) > 1<<20 || !json.Valid(change.Value) {
				return storage.ErrInvalid
			}
		} else if len(change.Value) != 0 {
			return storage.ErrInvalid
		}
	}
	return nil
}

func validateAppliedDraftTx(ctx context.Context, tx *sql.Tx, request appconfig.ApplyScopeRequest) error {
	var scopeKind, scopeID, state string
	var baseVersion, draftVersion int64
	err := tx.QueryRowContext(ctx, `SELECT scope_kind, scope_id, state, base_scope_version, version
		FROM config_drafts WHERE id = ?`, request.DraftID).
		Scan(&scopeKind, &scopeID, &state, &baseVersion, &draftVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read applied configuration draft: %w", err)
	}
	if scopeKind != string(request.Scope.Kind) || scopeID != request.Scope.ID || state != string(appconfig.DraftReviewed) || baseVersion != request.ExpectedVersion || draftVersion != request.DraftVersion {
		return storage.ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT setting_key, value_json, reset_value, secret, configured
		FROM config_draft_entries WHERE draft_id = ? ORDER BY setting_key`, request.DraftID)
	if err != nil {
		return fmt.Errorf("read applied configuration draft entries: %w", err)
	}
	defer rows.Close()
	draftEntries := make(map[string]appconfig.DraftEntry)
	for rows.Next() {
		var entry appconfig.DraftEntry
		var value sql.NullString
		if err := rows.Scan(&entry.Key, &value, &entry.Reset, &entry.Secret, &entry.Configured); err != nil {
			return fmt.Errorf("scan applied configuration draft entry: %w", err)
		}
		if value.Valid && !entry.Secret {
			entry.Value = json.RawMessage(value.String)
		}
		draftEntries[entry.Key] = entry
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read applied configuration draft entries: %w", err)
	}
	if len(draftEntries) != len(request.Changes) {
		return storage.ErrInvalid
	}
	for _, change := range request.Changes {
		entry, ok := draftEntries[change.Key]
		if !ok || entry.Secret != change.Secret || entry.Configured != change.Configured || entry.Reset != !change.Configured || string(entry.Value) != string(change.Value) {
			return storage.ErrInvalid
		}
	}
	return nil
}

func applyConfigChangeTx(ctx context.Context, tx *sql.Tx, revision appconfig.RegistryRevision, change appconfig.ScopeChange, now time.Time) (appconfig.RevisionEntry, int64, error) {
	entry := appconfig.RevisionEntry{Key: change.Key, AfterConfigured: change.Configured, Secret: change.Secret}
	var before sql.NullString
	var beforeConfigured, beforeSecret bool
	var currentValueVersion int64
	err := tx.QueryRowContext(ctx, `SELECT value_json, configured, secret, version FROM config_scope_values
		WHERE setting_key = ? AND scope_kind = ? AND scope_id = ?`, change.Key, revision.Scope.Kind, revision.Scope.ID).
		Scan(&before, &beforeConfigured, &beforeSecret, &currentValueVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appconfig.RevisionEntry{}, 0, fmt.Errorf("read configuration value %s: %w", change.Key, err)
	}
	if err == nil && beforeSecret != change.Secret {
		return appconfig.RevisionEntry{}, 0, storage.ErrInvalid
	}
	entry.BeforeConfigured = beforeConfigured
	if before.Valid && !beforeSecret {
		entry.BeforeValue = json.RawMessage(before.String)
	}
	if change.Configured && !change.Secret {
		entry.AfterValue = append(json.RawMessage(nil), change.Value...)
	}
	var beforeValue, afterValue any
	if len(entry.BeforeValue) != 0 {
		beforeValue = string(entry.BeforeValue)
	}
	if len(entry.AfterValue) != 0 {
		afterValue = string(entry.AfterValue)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_registry_revision_entries(
		revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, revision.ID, change.Key, beforeValue, afterValue,
		entry.BeforeConfigured, entry.AfterConfigured, change.Secret)
	if err != nil {
		return appconfig.RevisionEntry{}, 0, fmt.Errorf("insert configuration revision entry %s: %w", change.Key, err)
	}
	if !change.Configured {
		if _, err := tx.ExecContext(ctx, `DELETE FROM config_scope_values
			WHERE setting_key = ? AND scope_kind = ? AND scope_id = ?`, change.Key, revision.Scope.Kind, revision.Scope.ID); err != nil {
			return appconfig.RevisionEntry{}, 0, fmt.Errorf("reset configuration value %s: %w", change.Key, err)
		}
		return entry, currentValueVersion, nil
	}
	valueVersion := currentValueVersion + 1
	var storedValue any
	if !change.Secret {
		storedValue = string(change.Value)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_scope_values(
		setting_key, scope_kind, scope_id, value_json, configured, secret, version, revision_id, updated_at)
		VALUES(?, ?, ?, ?, 1, ?, ?, ?, ?) ON CONFLICT(setting_key, scope_kind, scope_id) DO UPDATE SET
		value_json = excluded.value_json, configured = 1, secret = excluded.secret,
		version = excluded.version, revision_id = excluded.revision_id, updated_at = excluded.updated_at`,
		change.Key, revision.Scope.Kind, revision.Scope.ID, storedValue, change.Secret,
		valueVersion, revision.ID, now.Format(timestampFormat))
	if err != nil {
		return appconfig.RevisionEntry{}, 0, fmt.Errorf("store configuration value %s: %w", change.Key, err)
	}
	return entry, valueVersion, nil
}

const registryRevisionSelect = `SELECT sequence, id, scope_kind, scope_id, scope_version,
	actor_id, actor_role, operation, reason, rollback_of, draft_id, created_at
	FROM config_registry_revisions`

func (s *Store) GetConfigRegistryRevision(ctx context.Context, id string) (appconfig.RegistryRevision, error) {
	if strings.TrimSpace(id) == "" {
		return appconfig.RegistryRevision{}, storage.ErrInvalid
	}
	revision, err := scanRegistryRevision(s.db.QueryRowContext(ctx, registryRevisionSelect+" WHERE id = ?", id))
	if err != nil {
		return appconfig.RegistryRevision{}, err
	}
	entries, err := s.registryRevisionEntries(ctx, revision.ID)
	if err != nil {
		return appconfig.RegistryRevision{}, err
	}
	revision.Entries = entries
	return revision, nil
}

func (s *Store) ListConfigRegistryRevisions(ctx context.Context, scope appconfig.ScopeRef, limit int) ([]appconfig.RegistryRevision, error) {
	if err := validateStoredScope(scope); err != nil {
		return nil, err
	}
	limit = boundedLimit(limit, 50, 200)
	rows, err := s.db.QueryContext(ctx, registryRevisionSelect+` WHERE scope_kind = ? AND scope_id = ?
		ORDER BY sequence DESC LIMIT ?`, scope.Kind, scope.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list configuration registry revisions: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.RegistryRevision, 0)
	for rows.Next() {
		revision, scanErr := scanRegistryRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list configuration registry revisions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close configuration registry revision rows: %w", err)
	}
	for index := range result {
		result[index].Entries, err = s.registryRevisionEntries(ctx, result[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) GetConfigScopeAtRevision(ctx context.Context, revisionID string) (appconfig.ScopeState, error) {
	target, err := s.GetConfigRegistryRevision(ctx, revisionID)
	if err != nil {
		return appconfig.ScopeState{}, err
	}
	state := appconfig.ScopeState{
		Scope: target.Scope, Version: target.ScopeVersion, RevisionID: target.ID,
		Values: []appconfig.StoredValue{}, UpdatedAt: target.CreatedAt,
	}
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (
		SELECT e.setting_key, e.after_value, e.after_configured, e.secret,
			r.scope_version, r.id AS source_revision, r.created_at,
			row_number() OVER(PARTITION BY e.setting_key ORDER BY r.scope_version DESC) AS position
		FROM config_registry_revision_entries e
		JOIN config_registry_revisions r ON r.id = e.revision_id
		WHERE r.scope_kind = ? AND r.scope_id = ? AND r.scope_version <= ?
	)
	SELECT setting_key, after_value, after_configured, secret, scope_version,
		source_revision, created_at FROM ranked WHERE position = 1 AND after_configured = 1
	ORDER BY setting_key`, target.Scope.Kind, target.Scope.ID, target.ScopeVersion)
	if err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("reconstruct historical configuration scope: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value appconfig.StoredValue
		var raw sql.NullString
		var created string
		if err := rows.Scan(&value.Key, &raw, &value.Configured, &value.Secret,
			&value.Version, &value.RevisionID, &created); err != nil {
			return appconfig.ScopeState{}, fmt.Errorf("scan historical configuration value: %w", err)
		}
		value.Scope = target.Scope
		if raw.Valid && !value.Secret {
			value.Value = json.RawMessage(raw.String)
		}
		value.UpdatedAt, _ = time.Parse(timestampFormat, created)
		state.Values = append(state.Values, value)
	}
	if err := rows.Err(); err != nil {
		return appconfig.ScopeState{}, fmt.Errorf("reconstruct historical configuration scope: %w", err)
	}
	return state, nil
}

func scanRegistryRevision(row scanner) (appconfig.RegistryRevision, error) {
	var revision appconfig.RegistryRevision
	var scopeKind, created string
	if err := row.Scan(&revision.Sequence, &revision.ID, &scopeKind, &revision.Scope.ID,
		&revision.ScopeVersion, &revision.ActorID, &revision.ActorRole, &revision.Operation,
		&revision.Reason, &revision.RollbackOf, &revision.DraftID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appconfig.RegistryRevision{}, storage.ErrNotFound
		}
		return appconfig.RegistryRevision{}, fmt.Errorf("scan configuration registry revision: %w", err)
	}
	revision.Scope.Kind = appconfig.ScopeKind(scopeKind)
	revision.CreatedAt, _ = time.Parse(timestampFormat, created)
	return revision, nil
}

func (s *Store) registryRevisionEntries(ctx context.Context, id string) ([]appconfig.RevisionEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key, before_value, after_value,
		before_configured, after_configured, secret FROM config_registry_revision_entries
		WHERE revision_id = ? ORDER BY setting_key`, id)
	if err != nil {
		return nil, fmt.Errorf("list configuration registry revision entries: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.RevisionEntry, 0)
	for rows.Next() {
		var entry appconfig.RevisionEntry
		var before, after sql.NullString
		if err := rows.Scan(&entry.Key, &before, &after, &entry.BeforeConfigured, &entry.AfterConfigured, &entry.Secret); err != nil {
			return nil, fmt.Errorf("scan configuration registry revision entry: %w", err)
		}
		if before.Valid && !entry.Secret {
			entry.BeforeValue = json.RawMessage(before.String)
		}
		if after.Valid && !entry.Secret {
			entry.AfterValue = json.RawMessage(after.String)
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *Store) SaveJobConfigSnapshot(ctx context.Context, snapshot appconfig.JobSnapshot) (appconfig.JobSnapshot, error) {
	if snapshot.JobID == "" || snapshot.SchemaVersion < 1 || !validHexDigest(snapshot.RegistryHash) || !validHexDigest(snapshot.SHA256) || len(snapshot.Document) == 0 || len(snapshot.Document) > 4<<20 || !json.Valid(snapshot.Document) {
		return appconfig.JobSnapshot{}, storage.ErrInvalid
	}
	existing, err := s.GetJobConfigSnapshot(ctx, snapshot.JobID)
	if err == nil {
		if existing.SHA256 == snapshot.SHA256 && existing.RegistryHash == snapshot.RegistryHash {
			return existing, nil
		}
		return appconfig.JobSnapshot{}, storage.ErrConflict
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return appconfig.JobSnapshot{}, err
	}
	snapshot.CreatedAt = s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.JobSnapshot{}, fmt.Errorf("begin job configuration snapshot: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO job_config_snapshots(job_id, schema_version,
		registry_hash, snapshot_sha256, document, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		snapshot.JobID, snapshot.SchemaVersion, snapshot.RegistryHash, snapshot.SHA256,
		string(snapshot.Document), snapshot.CreatedAt.Format(timestampFormat))
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return appconfig.JobSnapshot{}, storage.ErrNotFound
		}
		return appconfig.JobSnapshot{}, fmt.Errorf("insert job configuration snapshot: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: "controller", ActorRole: "system", Action: "config_registry.snapshot",
		TargetType: "job", TargetID: snapshot.JobID,
		Details: json.RawMessage(fmt.Sprintf(`{"registry_hash":%q,"snapshot_sha256":%q}`, snapshot.RegistryHash, snapshot.SHA256)),
	}); err != nil {
		return appconfig.JobSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.JobSnapshot{}, fmt.Errorf("commit job configuration snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *Store) GetJobConfigSnapshot(ctx context.Context, jobID string) (appconfig.JobSnapshot, error) {
	if strings.TrimSpace(jobID) == "" {
		return appconfig.JobSnapshot{}, storage.ErrInvalid
	}
	var snapshot appconfig.JobSnapshot
	var document, created string
	err := s.db.QueryRowContext(ctx, `SELECT job_id, schema_version, registry_hash,
		snapshot_sha256, document, created_at FROM job_config_snapshots WHERE job_id = ?`, jobID).
		Scan(&snapshot.JobID, &snapshot.SchemaVersion, &snapshot.RegistryHash, &snapshot.SHA256, &document, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return appconfig.JobSnapshot{}, storage.ErrNotFound
	}
	if err != nil {
		return appconfig.JobSnapshot{}, fmt.Errorf("read job configuration snapshot: %w", err)
	}
	snapshot.Document = json.RawMessage(document)
	snapshot.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return appconfig.JobSnapshot{}, fmt.Errorf("parse job configuration snapshot timestamp: %w", err)
	}
	return snapshot, nil
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
