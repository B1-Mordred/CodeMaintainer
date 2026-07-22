package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
)

const windowsWorkerProfileSelect = `SELECT id,name,mode,endpoint,endpoint_allowlist_json,credential_reference,credential_status,health,capacity,vm_template_id,toolchains_json,allowed_job_types_json,timeout_seconds,simulator_profile_ids_json,artifact_retention_days,signing_policy_reference,manual_gates_json,enabled,revision,updated_at FROM windows_worker_profiles`

func (s *Store) GetWindowsWorkerProfile(ctx context.Context, id string) (windowsworker.Profile, error) {
	return scanWindowsWorkerProfile(s.db.QueryRowContext(ctx, windowsWorkerProfileSelect+" WHERE id=?", id))
}

func (s *Store) ListWindowsWorkerProfiles(ctx context.Context, limit int) ([]windowsworker.Profile, error) {
	rows, err := s.db.QueryContext(ctx, windowsWorkerProfileSelect+" ORDER BY id LIMIT ?", boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []windowsworker.Profile{}
	for rows.Next() {
		item, scanErr := scanWindowsWorkerProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanWindowsWorkerProfile(row scanner) (windowsworker.Profile, error) {
	var item windowsworker.Profile
	var allowlist, toolchains, jobTypes, simulators, gates, updated string
	var enabled int
	if err := row.Scan(&item.ID, &item.Name, &item.Mode, &item.Endpoint, &allowlist, &item.CredentialReference, &item.CredentialStatus, &item.Health, &item.Capacity, &item.VMTemplateID, &toolchains, &jobTypes, &item.TimeoutSeconds, &simulators, &item.ArtifactRetentionDays, &item.SigningPolicyReference, &gates, &enabled, &item.Revision, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if json.Unmarshal([]byte(allowlist), &item.EndpointAllowlist) != nil || json.Unmarshal([]byte(toolchains), &item.Toolchains) != nil || json.Unmarshal([]byte(jobTypes), &item.AllowedJobTypes) != nil || json.Unmarshal([]byte(simulators), &item.SimulatorProfileIDs) != nil || json.Unmarshal([]byte(gates), &item.ManualGates) != nil {
		return item, storage.ErrInvalid
	}
	item.Enabled = enabled == 1
	parsed, err := time.Parse(timestampFormat, updated)
	item.UpdatedAt = parsed
	return item, err
}

func (s *Store) SaveWindowsWorkerProfile(ctx context.Context, request windowsworker.SaveProfileRequest) (windowsworker.Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return windowsworker.Profile{}, err
	}
	defer tx.Rollback()
	var revision int64
	var priorCredential, priorSigning, priorGates string
	err = tx.QueryRowContext(ctx, "SELECT revision,credential_reference,signing_policy_reference,manual_gates_json FROM windows_worker_profiles WHERE id=?", request.Profile.ID).Scan(&revision, &priorCredential, &priorSigning, &priorGates)
	if errors.Is(err, sql.ErrNoRows) {
		revision = 0
	} else if err != nil {
		return windowsworker.Profile{}, err
	}
	if revision != request.ExpectedRevision {
		return windowsworker.Profile{}, storage.ErrConflict
	}
	request.Profile.Revision = revision + 1
	request.Profile.UpdatedAt = s.now()
	allowlist, _ := json.Marshal(request.Profile.EndpointAllowlist)
	toolchains, _ := json.Marshal(request.Profile.Toolchains)
	jobTypes, _ := json.Marshal(request.Profile.AllowedJobTypes)
	simulators, _ := json.Marshal(request.Profile.SimulatorProfileIDs)
	gates, _ := json.Marshal(request.Profile.ManualGates)
	_, err = tx.ExecContext(ctx, `INSERT INTO windows_worker_profiles(id,name,mode,endpoint,endpoint_allowlist_json,credential_reference,credential_status,health,capacity,vm_template_id,toolchains_json,allowed_job_types_json,timeout_seconds,simulator_profile_ids_json,artifact_retention_days,signing_policy_reference,manual_gates_json,enabled,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,mode=excluded.mode,endpoint=excluded.endpoint,endpoint_allowlist_json=excluded.endpoint_allowlist_json,credential_reference=excluded.credential_reference,credential_status=excluded.credential_status,health=excluded.health,capacity=excluded.capacity,vm_template_id=excluded.vm_template_id,toolchains_json=excluded.toolchains_json,allowed_job_types_json=excluded.allowed_job_types_json,timeout_seconds=excluded.timeout_seconds,simulator_profile_ids_json=excluded.simulator_profile_ids_json,artifact_retention_days=excluded.artifact_retention_days,signing_policy_reference=excluded.signing_policy_reference,manual_gates_json=excluded.manual_gates_json,enabled=excluded.enabled,revision=excluded.revision,updated_at=excluded.updated_at`, request.Profile.ID, request.Profile.Name, request.Profile.Mode, request.Profile.Endpoint, string(allowlist), request.Profile.CredentialReference, request.Profile.CredentialStatus, request.Profile.Health, request.Profile.Capacity, request.Profile.VMTemplateID, string(toolchains), string(jobTypes), request.Profile.TimeoutSeconds, string(simulators), request.Profile.ArtifactRetentionDays, request.Profile.SigningPolicyReference, string(gates), capabilityBoolInt(request.Profile.Enabled), request.Profile.Revision, request.Profile.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return windowsworker.Profile{}, err
	}
	details, _ := json.Marshal(map[string]any{
		"mode": request.Profile.Mode, "revision": request.Profile.Revision,
		"credential_reference_changed": priorCredential != request.Profile.CredentialReference,
		"signing_policy_changed":       priorSigning != request.Profile.SigningPolicyReference,
		"manual_gates_changed":         priorGates != string(gates),
	})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "administrator", Action: "windows_worker.profile_save", TargetType: "windows_worker_profile", TargetID: request.Profile.ID, Details: details}); err != nil {
		return windowsworker.Profile{}, err
	}
	if err := tx.Commit(); err != nil {
		return windowsworker.Profile{}, err
	}
	return s.GetWindowsWorkerProfile(ctx, request.Profile.ID)
}

func (s *Store) SaveWindowsWorkerResult(ctx context.Context, request windowsworker.RunRequest, result windowsworker.Result) (windowsworker.Result, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return windowsworker.Result{}, err
	}
	defer tx.Rollback()
	existing, err := scanWindowsWorkerResult(tx.QueryRowContext(ctx, `SELECT result_json FROM windows_worker_runs WHERE idempotency_key=?`, request.IdempotencyKey))
	if err == nil {
		if existing.ProfileID != request.ProfileID || existing.ProjectID != request.ProjectID || existing.JobID != request.JobID || existing.JobType != request.JobType || existing.InputSHA256 != windowsworker.InputSHA(request) {
			return windowsworker.Result{}, storage.ErrIdempotencyKey
		}
		existing.Replay = true
		return existing, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return windowsworker.Result{}, err
	}
	requestJSON, _ := json.Marshal(request)
	resultJSON, _ := json.Marshal(result)
	_, err = tx.ExecContext(ctx, `INSERT INTO windows_worker_runs(run_id,profile_id,project_id,job_id,job_type,input_sha256,request_json,result_json,state,idempotency_key,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, result.RunID, result.ProfileID, result.ProjectID, result.JobID, result.JobType, result.InputSHA256, string(requestJSON), string(resultJSON), result.State, result.IdempotencyKey, result.StartedAt.Format(timestampFormat), result.CompletedAt.Format(timestampFormat))
	if err != nil {
		return windowsworker.Result{}, err
	}
	details, _ := json.Marshal(map[string]any{"job_type": result.JobType, "state": result.State, "input_sha256": result.InputSHA256, "artifacts": len(result.Artifacts), "operator_gated": request.OperatorGated})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: request.ActorRole, Action: "windows_worker.run", TargetType: "windows_worker_run", TargetID: result.RunID, Details: details}); err != nil {
		return windowsworker.Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return windowsworker.Result{}, err
	}
	return result, nil
}

func (s *Store) ListWindowsWorkerResults(ctx context.Context, profileID string, limit int) ([]windowsworker.Result, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT result_json FROM windows_worker_runs WHERE profile_id=? ORDER BY started_at DESC LIMIT ?`, profileID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []windowsworker.Result{}
	for rows.Next() {
		item, scanErr := scanWindowsWorkerResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanWindowsWorkerResult(row scanner) (windowsworker.Result, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return windowsworker.Result{}, storage.ErrNotFound
		}
		return windowsworker.Result{}, err
	}
	var result windowsworker.Result
	if json.Unmarshal([]byte(raw), &result) != nil {
		return windowsworker.Result{}, storage.ErrInvalid
	}
	return result, nil
}
