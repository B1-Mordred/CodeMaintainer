package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/forges"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

const forgeProfileSelect = `SELECT project_id,provider,endpoint,endpoint_allowlist_json,repository,credential_reference,credential_status,webhook_status,sync_direction,polling_minutes,branch_convention,change_request_convention,label_mapping_json,ci_artifact_policy,release_policy,submodules_enabled,enabled,revision,updated_at FROM forge_profiles`

func (s *Store) GetForgeProfile(ctx context.Context, projectID string) (forges.Profile, error) {
	return scanForgeProfile(s.db.QueryRowContext(ctx, forgeProfileSelect+" WHERE project_id=?", projectID))
}
func scanForgeProfile(row scanner) (forges.Profile, error) {
	var item forges.Profile
	var allowlist, labels, updated string
	var submodules, enabled int
	if err := row.Scan(&item.ProjectID, &item.Provider, &item.Endpoint, &allowlist, &item.Repository, &item.CredentialReference, &item.CredentialStatus, &item.WebhookStatus, &item.SyncDirection, &item.PollingMinutes, &item.BranchConvention, &item.ChangeRequestConvention, &labels, &item.CIArtifactPolicy, &item.ReleasePolicy, &submodules, &enabled, &item.Revision, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if err := json.Unmarshal([]byte(allowlist), &item.EndpointAllowlist); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(labels), &item.LabelMapping); err != nil {
		return item, err
	}
	item.SubmodulesEnabled = submodules == 1
	item.Enabled = enabled == 1
	parsed, err := time.Parse(timestampFormat, updated)
	item.UpdatedAt = parsed
	return item, err
}
func (s *Store) SaveForgeProfile(ctx context.Context, request forges.SaveProfileRequest) (forges.Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return forges.Profile{}, err
	}
	defer tx.Rollback()
	var revision int64
	var previousCredential string
	err = tx.QueryRowContext(ctx, "SELECT revision,credential_reference FROM forge_profiles WHERE project_id=?", request.Profile.ProjectID).Scan(&revision, &previousCredential)
	if errors.Is(err, sql.ErrNoRows) {
		revision = 0
	} else if err != nil {
		return forges.Profile{}, err
	}
	if revision != request.ExpectedRevision {
		return forges.Profile{}, storage.ErrConflict
	}
	request.Profile.Revision = revision + 1
	request.Profile.UpdatedAt = s.now()
	allowlist, _ := json.Marshal(request.Profile.EndpointAllowlist)
	labels, _ := json.Marshal(request.Profile.LabelMapping)
	_, err = tx.ExecContext(ctx, `INSERT INTO forge_profiles(project_id,provider,endpoint,endpoint_allowlist_json,repository,credential_reference,credential_status,webhook_status,sync_direction,polling_minutes,branch_convention,change_request_convention,label_mapping_json,ci_artifact_policy,release_policy,submodules_enabled,enabled,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET provider=excluded.provider,endpoint=excluded.endpoint,endpoint_allowlist_json=excluded.endpoint_allowlist_json,repository=excluded.repository,credential_reference=excluded.credential_reference,credential_status=excluded.credential_status,webhook_status=excluded.webhook_status,sync_direction=excluded.sync_direction,polling_minutes=excluded.polling_minutes,branch_convention=excluded.branch_convention,change_request_convention=excluded.change_request_convention,label_mapping_json=excluded.label_mapping_json,ci_artifact_policy=excluded.ci_artifact_policy,release_policy=excluded.release_policy,submodules_enabled=excluded.submodules_enabled,enabled=excluded.enabled,revision=excluded.revision,updated_at=excluded.updated_at`, request.Profile.ProjectID, request.Profile.Provider, request.Profile.Endpoint, string(allowlist), request.Profile.Repository, request.Profile.CredentialReference, request.Profile.CredentialStatus, request.Profile.WebhookStatus, request.Profile.SyncDirection, request.Profile.PollingMinutes, request.Profile.BranchConvention, request.Profile.ChangeRequestConvention, string(labels), request.Profile.CIArtifactPolicy, request.Profile.ReleasePolicy, capabilityBoolInt(request.Profile.SubmodulesEnabled), capabilityBoolInt(request.Profile.Enabled), request.Profile.Revision, request.Profile.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return forges.Profile{}, err
	}
	details, _ := json.Marshal(map[string]any{"provider": request.Profile.Provider, "endpoint": request.Profile.Endpoint, "revision": request.Profile.Revision, "credential_reference_changed": previousCredential != request.Profile.CredentialReference})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "administrator", Action: "forge.profile_save", TargetType: "forge_profile", TargetID: request.Profile.ProjectID, Details: details}); err != nil {
		return forges.Profile{}, err
	}
	if err := tx.Commit(); err != nil {
		return forges.Profile{}, err
	}
	return s.GetForgeProfile(ctx, request.Profile.ProjectID)
}
func (s *Store) ListForgeProfiles(ctx context.Context, limit int) ([]forges.Profile, error) {
	rows, err := s.db.QueryContext(ctx, forgeProfileSelect+" ORDER BY project_id LIMIT ?", boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []forges.Profile{}
	for rows.Next() {
		item, err := scanForgeProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) SaveForgeSync(ctx context.Context, page forges.SyncPage, key, actorID string) (forges.SyncRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return forges.SyncRun{}, err
	}
	defer tx.Rollback()
	existing, err := scanForgeRun(tx.QueryRowContext(ctx, `SELECT id,project_id,provider,input_cursor,output_cursor,idempotency_key,state,objects,partial,unsupported_json,created_at FROM forge_sync_runs WHERE idempotency_key=?`, key))
	if err == nil {
		if existing.ProjectID != page.ProjectID || existing.Provider != page.Provider || existing.InputCursor != page.Cursor {
			return forges.SyncRun{}, storage.ErrIdempotencyKey
		}
		existing.Replay = true
		return existing, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return forges.SyncRun{}, err
	}
	now := s.now()
	id, err := NewID("forgesync")
	if err != nil {
		return forges.SyncRun{}, err
	}
	unsupported, _ := json.Marshal(page.Unsupported)
	state := "complete"
	if page.Partial {
		state = "partial"
	}
	run := forges.SyncRun{ID: id, ProjectID: page.ProjectID, Provider: page.Provider, InputCursor: page.Cursor, OutputCursor: page.NextCursor, IdempotencyKey: key, State: state, Objects: len(page.Objects), Partial: page.Partial, Unsupported: page.Unsupported, CreatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO forge_sync_runs(id,project_id,provider,input_cursor,output_cursor,idempotency_key,state,objects,partial,unsupported_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.ProjectID, run.Provider, run.InputCursor, run.OutputCursor, key, state, run.Objects, capabilityBoolInt(run.Partial), string(unsupported), now.Format(timestampFormat))
	if err != nil {
		return run, err
	}
	for _, item := range page.Objects {
		raw, _ := json.Marshal(item)
		_, err = tx.ExecContext(ctx, `INSERT INTO forge_objects(project_id,provider,kind,external_id,object_json,observed_at) VALUES(?,?,?,?,?,?) ON CONFLICT(project_id,provider,kind,external_id) DO UPDATE SET object_json=excluded.object_json,observed_at=excluded.observed_at`, item.ProjectID, item.Provider, item.Kind, item.ExternalID, string(raw), now.Format(timestampFormat))
		if err != nil {
			return run, err
		}
	}
	details, _ := json.Marshal(map[string]any{"provider": page.Provider, "objects": len(page.Objects), "input_cursor": page.Cursor, "output_cursor": page.NextCursor, "partial": page.Partial})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actorID, ActorRole: "operator", Action: "forge.sync", TargetType: "project", TargetID: page.ProjectID, Details: details}); err != nil {
		return run, err
	}
	if err := tx.Commit(); err != nil {
		return run, err
	}
	return run, nil
}
func scanForgeRun(row scanner) (forges.SyncRun, error) {
	var item forges.SyncRun
	var partial int
	var unsupported, created string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Provider, &item.InputCursor, &item.OutputCursor, &item.IdempotencyKey, &item.State, &item.Objects, &partial, &unsupported, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	item.Partial = partial == 1
	if err := json.Unmarshal([]byte(unsupported), &item.Unsupported); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	item.CreatedAt = parsed
	return item, err
}
func (s *Store) ListForgeSyncRuns(ctx context.Context, projectID string, limit int) ([]forges.SyncRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,provider,input_cursor,output_cursor,idempotency_key,state,objects,partial,unsupported_json,created_at FROM forge_sync_runs WHERE project_id=? ORDER BY created_at DESC LIMIT ?`, projectID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []forges.SyncRun{}
	for rows.Next() {
		item, err := scanForgeRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) ListForgeObjects(ctx context.Context, projectID, kind string, limit int) ([]forges.Object, error) {
	query := `SELECT object_json FROM forge_objects WHERE project_id=?`
	args := []any{projectID}
	if strings.TrimSpace(kind) != "" {
		query += " AND kind=?"
		args = append(args, kind)
	}
	query += " ORDER BY kind,external_id LIMIT ?"
	args = append(args, boundedLimit(limit, 500, 1000))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []forges.Object{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item forges.Object
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
