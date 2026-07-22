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

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

const installationSelect = `SELECT pack_id, pack_version, checksum_sha256, state, pinned, revision, previous_version, updated_at FROM capability_installations`

func (s *Store) GetCapabilityInstallation(ctx context.Context, packID string) (capabilities.Installation, error) {
	if strings.TrimSpace(packID) == "" {
		return capabilities.Installation{}, storage.ErrInvalid
	}
	return scanInstallation(s.db.QueryRowContext(ctx, installationSelect+" WHERE pack_id=?", packID))
}

func (s *Store) ListCapabilityInstallations(ctx context.Context, limit int) ([]capabilities.Installation, error) {
	rows, err := s.db.QueryContext(ctx, installationSelect+" ORDER BY pack_id LIMIT ?", boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []capabilities.Installation{}
	for rows.Next() {
		item, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanInstallation(row scanner) (capabilities.Installation, error) {
	var item capabilities.Installation
	var pinned int
	var updated string
	if err := row.Scan(&item.PackID, &item.PackVersion, &item.Checksum, &item.State, &pinned, &item.Revision, &item.Previous, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	item.Pinned = pinned == 1
	parsed, err := time.Parse(timestampFormat, updated)
	item.UpdatedAt = parsed
	return item, err
}

func (s *Store) TransitionCapability(ctx context.Context, request capabilities.TransitionRequest) (capabilities.Installation, capabilities.LifecycleEvent, error) {
	if strings.TrimSpace(request.PackID) == "" || strings.TrimSpace(request.ActorID) == "" || strings.TrimSpace(request.Reason) == "" {
		return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capabilities.Installation{}, capabilities.LifecycleEvent{}, err
	}
	defer tx.Rollback()
	current, currentErr := scanInstallation(tx.QueryRowContext(ctx, installationSelect+" WHERE pack_id=?", request.PackID))
	now := s.now()
	next := current
	switch request.Action {
	case "install":
		if currentErr == nil {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrConflict
		}
		if !errors.Is(currentErr, storage.ErrNotFound) || request.ExpectedRevision != 0 {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrConflict
		}
		next = capabilities.Installation{PackID: request.PackID, PackVersion: request.TargetVersion, Checksum: request.Checksum, State: "enabled", Revision: 1, UpdatedAt: now}
	case "upgrade", "rollback":
		if currentErr != nil {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, currentErr
		}
		if current.Revision != request.ExpectedRevision || current.Pinned || request.TargetVersion == current.PackVersion {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrConflict
		}
		if request.Action == "rollback" {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM capability_lifecycle_events WHERE pack_id=? AND (from_version=? OR to_version=?)`, request.PackID, request.TargetVersion, request.TargetVersion).Scan(&count); err != nil {
				return next, capabilities.LifecycleEvent{}, err
			}
			if count == 0 {
				return next, capabilities.LifecycleEvent{}, storage.ErrInvalid
			}
		}
		next.PackVersion = request.TargetVersion
		next.Checksum = request.Checksum
		next.Previous = current.PackVersion
		next.Revision++
		next.UpdatedAt = now
	case "enable", "disable", "pin", "unpin":
		if currentErr != nil {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, currentErr
		}
		if current.Revision != request.ExpectedRevision {
			return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrConflict
		}
		if request.Action == "enable" {
			next.State = "enabled"
		}
		if request.Action == "disable" {
			next.State = "disabled"
		}
		if request.Action == "pin" {
			next.Pinned = true
		}
		if request.Action == "unpin" {
			next.Pinned = false
		}
		next.Revision++
		next.UpdatedAt = now
	default:
		return capabilities.Installation{}, capabilities.LifecycleEvent{}, storage.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO capability_installations(pack_id,pack_version,checksum_sha256,state,pinned,revision,previous_version,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(pack_id) DO UPDATE SET pack_version=excluded.pack_version,checksum_sha256=excluded.checksum_sha256,state=excluded.state,pinned=excluded.pinned,revision=excluded.revision,previous_version=excluded.previous_version,updated_at=excluded.updated_at`, next.PackID, next.PackVersion, next.Checksum, next.State, capabilityBoolInt(next.Pinned), next.Revision, next.Previous, now.Format(timestampFormat))
	if err != nil {
		return next, capabilities.LifecycleEvent{}, err
	}
	eventID, err := NewID("packevt")
	if err != nil {
		return next, capabilities.LifecycleEvent{}, err
	}
	event := capabilities.LifecycleEvent{ID: eventID, PackID: request.PackID, Action: request.Action, ActorID: request.ActorID, Reason: request.Reason, CreatedAt: now}
	if currentErr == nil {
		event.FromVersion = current.PackVersion
	}
	event.ToVersion = next.PackVersion
	if _, err := tx.ExecContext(ctx, `INSERT INTO capability_lifecycle_events(id,pack_id,from_version,to_version,action,actor_id,reason,created_at) VALUES(?,?,?,?,?,?,?,?)`, event.ID, event.PackID, event.FromVersion, event.ToVersion, event.Action, event.ActorID, event.Reason, now.Format(timestampFormat)); err != nil {
		return next, event, err
	}
	details, _ := json.Marshal(map[string]any{"action": event.Action, "from_version": event.FromVersion, "to_version": event.ToVersion, "revision": next.Revision, "checksum": next.Checksum})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "administrator", Action: "capability_pack." + request.Action, TargetType: "capability_pack", TargetID: request.PackID, Details: details}); err != nil {
		return next, event, err
	}
	if err := tx.Commit(); err != nil {
		return next, event, err
	}
	return next, event, nil
}

func (s *Store) ListCapabilityEvents(ctx context.Context, packID string, limit int) ([]capabilities.LifecycleEvent, error) {
	query := `SELECT id,pack_id,from_version,to_version,action,actor_id,reason,created_at FROM capability_lifecycle_events`
	args := []any{}
	if packID != "" {
		query += " WHERE pack_id=?"
		args = append(args, packID)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []capabilities.LifecycleEvent{}
	for rows.Next() {
		var item capabilities.LifecycleEvent
		var created string
		if err := rows.Scan(&item.ID, &item.PackID, &item.FromVersion, &item.ToVersion, &item.Action, &item.ActorID, &item.Reason, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListCapabilityAssignments(ctx context.Context, projectID string, limit int) ([]capabilities.Assignment, error) {
	query := `SELECT project_id,pack_id,pack_version,enabled,config_json,revision,updated_at FROM capability_assignments`
	args := []any{}
	if projectID != "" {
		query += " WHERE project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY project_id,pack_id LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []capabilities.Assignment{}
	for rows.Next() {
		item, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func scanAssignment(row scanner) (capabilities.Assignment, error) {
	var item capabilities.Assignment
	var enabled int
	var raw, updated string
	if err := row.Scan(&item.ProjectID, &item.PackID, &item.PackVersion, &enabled, &raw, &item.Revision, &updated); err != nil {
		return item, err
	}
	item.Enabled = enabled == 1
	item.Config = json.RawMessage(raw)
	parsed, err := time.Parse(timestampFormat, updated)
	item.UpdatedAt = parsed
	return item, err
}

func (s *Store) UpdateCapabilityAssignmentConfiguration(ctx context.Context, request capabilities.AssignmentConfigurationRequest) (capabilities.Assignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capabilities.Assignment{}, err
	}
	defer tx.Rollback()
	var assignment capabilities.Assignment
	var enabled int
	var raw, updated string
	err = tx.QueryRowContext(ctx, `SELECT project_id,pack_id,pack_version,enabled,config_json,revision,updated_at FROM capability_assignments WHERE project_id=? AND pack_id=?`, request.ProjectID, request.PackID).Scan(&assignment.ProjectID, &assignment.PackID, &assignment.PackVersion, &enabled, &raw, &assignment.Revision, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return assignment, storage.ErrNotFound
	}
	if err != nil {
		return assignment, err
	}
	if assignment.Revision != request.ExpectedRevision {
		return assignment, storage.ErrConflict
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE capability_assignments SET config_json=?,revision=revision+1,updated_at=? WHERE project_id=? AND pack_id=? AND revision=?`, string(request.Config), now.Format(timestampFormat), request.ProjectID, request.PackID, request.ExpectedRevision)
	if err != nil {
		return assignment, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return assignment, storage.ErrConflict
	}
	assignment.Config = append(json.RawMessage(nil), request.Config...)
	assignment.Enabled = enabled == 1
	assignment.Revision++
	assignment.UpdatedAt = now
	digest := sha256.Sum256(request.Config)
	details, _ := json.Marshal(map[string]any{"revision": assignment.Revision, "configuration_sha256": hex.EncodeToString(digest[:]), "reason": request.Reason})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "administrator", Action: "capability_pack.configuration", TargetType: "capability_assignment", TargetID: request.ProjectID + ":" + request.PackID, Details: details}); err != nil {
		return assignment, err
	}
	if err := tx.Commit(); err != nil {
		return assignment, err
	}
	return assignment, nil
}

func (s *Store) SaveRepoDoctorScan(ctx context.Context, scan capabilities.Scan, actorID string) (capabilities.Scan, error) {
	if len(scan.Proposals) > 1000 || len(scan.Findings) > 1000 {
		return scan, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return scan, err
	}
	defer tx.Rollback()
	scan.CreatedAt = s.now()
	findings, _ := json.Marshal(scan.Findings)
	drift, _ := json.Marshal(scan.Drift)
	_, err = tx.ExecContext(ctx, `INSERT INTO repo_doctor_scans(id,project_id,repository,revision,state,findings_json,drift_json,files_observed,excluded_files,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, scan.ID, scan.ProjectID, scan.Repository, scan.Revision, scan.State, string(findings), string(drift), scan.FilesObserved, scan.ExcludedFiles, scan.CreatedAt.Format(timestampFormat))
	if err != nil {
		return scan, err
	}
	for _, proposal := range scan.Proposals {
		value := proposal.Value
		if len(value) == 0 {
			value = json.RawMessage(`{}`)
		}
		evidence, _ := json.Marshal(proposal.Evidence)
		_, err = tx.ExecContext(ctx, `INSERT INTO repo_doctor_proposals(id,scan_id,project_id,kind,proposal_key,value_json,confidence,evidence_json,state,version) VALUES(?,?,?,?,?,?,?,?,?,?)`, proposal.ID, scan.ID, scan.ProjectID, proposal.Kind, proposal.Key, string(value), proposal.Confidence, string(evidence), "pending", 1)
		if err != nil {
			return scan, err
		}
	}
	details, _ := json.Marshal(map[string]any{"revision": scan.Revision, "files_observed": scan.FilesObserved, "excluded_files": scan.ExcludedFiles, "findings": len(scan.Findings), "proposals": len(scan.Proposals)})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: required(actorID, "system"), ActorRole: "operator", Action: "repo_doctor.scan", TargetType: "project", TargetID: scan.ProjectID, Details: details}); err != nil {
		return scan, err
	}
	if err := tx.Commit(); err != nil {
		return scan, err
	}
	return s.GetRepoDoctorScan(ctx, scan.ProjectID, scan.ID)
}

func (s *Store) GetRepoDoctorScan(ctx context.Context, projectID, scanID string) (capabilities.Scan, error) {
	var item capabilities.Scan
	var findings, drift, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,repository,revision,state,findings_json,drift_json,files_observed,excluded_files,created_at FROM repo_doctor_scans WHERE id=? AND project_id=?`, scanID, projectID).Scan(&item.ID, &item.ProjectID, &item.Repository, &item.Revision, &item.State, &findings, &drift, &item.FilesObserved, &item.ExcludedFiles, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return item, storage.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(findings), &item.Findings); err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(drift), &item.Drift); err != nil {
		return item, err
	}
	item.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.Proposals, err = s.listRepoDoctorProposals(ctx, item.ID)
	return item, err
}
func (s *Store) listRepoDoctorProposals(ctx context.Context, scanID string) ([]capabilities.Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scan_id,project_id,kind,proposal_key,value_json,confidence,evidence_json,state,version,reason,reviewed_by,reviewed_at FROM repo_doctor_proposals WHERE scan_id=? ORDER BY id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []capabilities.Proposal{}
	for rows.Next() {
		var item capabilities.Proposal
		var value, evidence string
		var reviewed sql.NullString
		if err := rows.Scan(&item.ID, &item.ScanID, &item.ProjectID, &item.Kind, &item.Key, &value, &item.Confidence, &evidence, &item.State, &item.Version, &item.Reason, &item.ReviewedBy, &reviewed); err != nil {
			return nil, err
		}
		item.Value = json.RawMessage(value)
		if err := json.Unmarshal([]byte(evidence), &item.Evidence); err != nil {
			return nil, err
		}
		if reviewed.Valid {
			parsed, err := time.Parse(timestampFormat, reviewed.String)
			if err != nil {
				return nil, err
			}
			item.ReviewedAt = &parsed
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) ListRepoDoctorScans(ctx context.Context, projectID string, limit int) ([]capabilities.Scan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM repo_doctor_scans WHERE project_id=? ORDER BY created_at DESC LIMIT ?`, projectID, boundedLimit(limit, 50, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]capabilities.Scan, 0, len(ids))
	for _, id := range ids {
		item, err := s.GetRepoDoctorScan(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) ReviewRepoDoctorProposal(ctx context.Context, request capabilities.ReviewRequest) (capabilities.Proposal, *capabilities.Assignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capabilities.Proposal{}, nil, err
	}
	defer tx.Rollback()
	var proposal capabilities.Proposal
	var value, evidence string
	err = tx.QueryRowContext(ctx, `SELECT id,scan_id,project_id,kind,proposal_key,value_json,confidence,evidence_json,state,version FROM repo_doctor_proposals WHERE id=? AND scan_id=? AND project_id=?`, request.ProposalID, request.ScanID, request.ProjectID).Scan(&proposal.ID, &proposal.ScanID, &proposal.ProjectID, &proposal.Kind, &proposal.Key, &value, &proposal.Confidence, &evidence, &proposal.State, &proposal.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return proposal, nil, storage.ErrNotFound
	}
	if err != nil {
		return proposal, nil, err
	}
	if proposal.Version != request.ExpectedVersion || proposal.State != "pending" {
		return proposal, nil, storage.ErrConflict
	}
	proposal.Value = json.RawMessage(value)
	_ = json.Unmarshal([]byte(evidence), &proposal.Evidence)
	if request.Config == nil {
		request.Config = json.RawMessage(`{}`)
	}
	newState := "rejected"
	if request.Accept {
		newState = "accepted"
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE repo_doctor_proposals SET state=?,version=version+1,reason=?,reviewed_by=?,reviewed_at=? WHERE id=? AND version=? AND state='pending'`, newState, request.Reason, request.ActorID, now.Format(timestampFormat), proposal.ID, request.ExpectedVersion)
	if err != nil {
		return proposal, nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return proposal, nil, storage.ErrConflict
	}
	proposal.State = newState
	proposal.Version++
	proposal.Reason = request.Reason
	proposal.ReviewedBy = request.ActorID
	proposal.ReviewedAt = &now
	var assignment *capabilities.Assignment
	if request.Accept && proposal.Kind == "capability_pack" {
		var selected struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(proposal.Value, &selected); err != nil {
			return proposal, nil, storage.ErrInvalid
		}
		var installedVersion, state string
		if err := tx.QueryRowContext(ctx, `SELECT pack_version,state FROM capability_installations WHERE pack_id=?`, proposal.Key).Scan(&installedVersion, &state); errors.Is(err, sql.ErrNoRows) {
			return proposal, nil, fmt.Errorf("exact proposed pack must be installed before assignment: %w", storage.ErrConflict)
		} else if err != nil {
			return proposal, nil, err
		}
		if installedVersion != selected.Version || state != "enabled" {
			return proposal, nil, fmt.Errorf("exact proposed pack version is not enabled: %w", storage.ErrConflict)
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM capability_assignments WHERE project_id=? AND pack_id=?`, proposal.ProjectID, proposal.Key).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			revision = 0
		} else if err != nil {
			return proposal, nil, err
		}
		revision++
		_, err = tx.ExecContext(ctx, `INSERT INTO capability_assignments(project_id,pack_id,pack_version,enabled,config_json,revision,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(project_id,pack_id) DO UPDATE SET pack_version=excluded.pack_version,enabled=excluded.enabled,config_json=excluded.config_json,revision=excluded.revision,updated_at=excluded.updated_at`, proposal.ProjectID, proposal.Key, selected.Version, 1, string(request.Config), revision, now.Format(timestampFormat))
		if err != nil {
			return proposal, nil, err
		}
		assigned := capabilities.Assignment{ProjectID: proposal.ProjectID, PackID: proposal.Key, PackVersion: selected.Version, Enabled: true, Config: request.Config, Revision: revision, UpdatedAt: now}
		assignment = &assigned
	}
	details, _ := json.Marshal(map[string]any{"accepted": request.Accept, "kind": proposal.Kind, "key": proposal.Key, "proposal_version": proposal.Version})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "operator", Action: "repo_doctor.proposal_review", TargetType: "repo_doctor_proposal", TargetID: proposal.ID, Details: details}); err != nil {
		return proposal, nil, err
	}
	if err := tx.Commit(); err != nil {
		return proposal, nil, err
	}
	return proposal, assignment, nil
}

func capabilityBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
