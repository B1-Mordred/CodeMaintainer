package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/agents"
	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Store) ObserveFindings(ctx context.Context, jobID string, cycle int, values []agents.Finding) ([]findings.Record, error) {
	if jobID == "" || cycle < 0 || cycle > 10 || len(values) > 100 {
		return nil, storage.ErrInvalid
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("invalid QC finding: %w", storage.ErrInvalid)
		}
		if _, exists := seen[value.ID]; exists {
			return nil, fmt.Errorf("duplicate QC finding ID: %w", storage.ErrInvalid)
		}
		seen[value.ID] = struct{}{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now()
	result := make([]findings.Record, 0, len(values))
	for _, value := range values {
		location, _ := json.Marshal(value.Location)
		record, err := scanFinding(tx.QueryRowContext(ctx, findingSelect+" WHERE job_id=? AND id=?", jobID, value.ID))
		switch {
		case errors.Is(err, storage.ErrNotFound):
			blocking := value.Severity == "blocker" || value.Severity == "must_fix"
			if cycle > 0 && blocking && value.Category != "regression_introduced_by_repair" && value.Category != "newly_observed_evidence" {
				return nil, fmt.Errorf("new blocking finding category is forbidden after repair: %w", storage.ErrInvalid)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO qc_findings(
				job_id,id,severity,category,claim,location,required_resolution,verification_method,status,
				first_seen_cycle,last_seen_cycle,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				jobID, value.ID, value.Severity, value.Category, value.Claim, string(location), value.RequiredResolution,
				value.VerificationMethod, findings.StatusOpen, cycle, cycle, 1, now.Format(timestampFormat), now.Format(timestampFormat))
			if err != nil {
				return nil, fmt.Errorf("insert QC finding: %w", err)
			}
			record, err = scanFinding(tx.QueryRowContext(ctx, findingSelect+" WHERE job_id=? AND id=?", jobID, value.ID))
		case err != nil:
			return nil, err
		default:
			if record.Category != value.Category || string(record.Location) != string(location) || record.Severity != value.Severity {
				return nil, fmt.Errorf("stable QC finding identity changed meaning: %w", storage.ErrConflict)
			}
			if cycle < record.LastSeenCycle {
				return nil, fmt.Errorf("QC review cycle moved backwards: %w", storage.ErrConflict)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE qc_findings SET last_seen_cycle=?, updated_at=?, version=version+1 WHERE job_id=? AND id=?`,
				cycle, now.Format(timestampFormat), jobID, value.ID); err != nil {
				return nil, err
			}
			record, err = scanFinding(tx.QueryRowContext(ctx, findingSelect+" WHERE job_id=? AND id=?", jobID, value.ID))
		}
		if err != nil {
			return nil, err
		}
		observation, _ := json.Marshal(value)
		if _, err := tx.ExecContext(ctx, `INSERT INTO qc_finding_observations(job_id,finding_id,review_cycle,report,created_at) VALUES(?,?,?,?,?)`,
			jobID, value.ID, cycle, string(observation), now.Format(timestampFormat)); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	details, _ := json.Marshal(map[string]any{"review_cycle": cycle, "finding_ids": ids})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: "qc-agent", ActorRole: "qc", Action: "qc.findings.observe", TargetType: "job", TargetID: jobID, Details: details,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) TransitionFinding(ctx context.Context, jobID, findingID string, request findings.TransitionRequest) (findings.Record, error) {
	if jobID == "" || findingID == "" || strings.TrimSpace(request.ActorID) == "" || strings.TrimSpace(request.ActorRole) == "" ||
		strings.TrimSpace(request.Rationale) == "" || request.ExpectedVersion < 1 {
		return findings.Record{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return findings.Record{}, err
	}
	defer tx.Rollback()
	current, err := scanFinding(tx.QueryRowContext(ctx, findingSelect+" WHERE job_id=? AND id=?", jobID, findingID))
	if err != nil {
		return findings.Record{}, err
	}
	if current.Version != request.ExpectedVersion {
		return findings.Record{}, storage.ErrConflict
	}
	if !findings.CanTransition(current.Status, request.To) {
		return findings.Record{}, storage.ErrInvalid
	}
	if request.To == findings.StatusHumanWaived && (current.Severity == "blocker" || current.Severity == "must_fix") {
		if !request.Reauthenticated || (request.ActorRole != "reviewer" && request.ActorRole != "administrator") {
			return findings.Record{}, storage.ErrInvalid
		}
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE qc_findings SET status=?,version=version+1,updated_at=? WHERE job_id=? AND id=? AND version=?`,
		request.To, now.Format(timestampFormat), jobID, findingID, request.ExpectedVersion)
	if err != nil {
		return findings.Record{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return findings.Record{}, storage.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO qc_finding_transitions(
		job_id,finding_id,from_status,to_status,actor_id,actor_role,rationale,reauthenticated,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		jobID, findingID, current.Status, request.To, request.ActorID, request.ActorRole, request.Rationale,
		boolInt(request.Reauthenticated), now.Format(timestampFormat)); err != nil {
		return findings.Record{}, err
	}
	details, _ := json.Marshal(map[string]any{"from": current.Status, "to": request.To, "rationale": request.Rationale, "reauthenticated": request.Reauthenticated})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: request.ActorID, ActorRole: request.ActorRole, Action: "qc.finding.transition",
		TargetType: "qc_finding", TargetID: jobID + "/" + findingID, Details: details,
	}); err != nil {
		return findings.Record{}, err
	}
	updated, err := scanFinding(tx.QueryRowContext(ctx, findingSelect+" WHERE job_id=? AND id=?", jobID, findingID))
	if err != nil {
		return findings.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return findings.Record{}, err
	}
	return updated, nil
}

func (s *Store) ListFindings(ctx context.Context, jobID string) ([]findings.Record, error) {
	rows, err := s.db.QueryContext(ctx, findingSelect+" WHERE job_id=? ORDER BY id", jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]findings.Record, 0)
	for rows.Next() {
		record, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

const findingSelect = `SELECT job_id,id,severity,category,claim,location,required_resolution,verification_method,status,
	first_seen_cycle,last_seen_cycle,version,created_at,updated_at FROM qc_findings`

func scanFinding(row scanner) (findings.Record, error) {
	var record findings.Record
	var location, status, created, updated string
	err := row.Scan(&record.JobID, &record.ID, &record.Severity, &record.Category, &record.Claim, &location,
		&record.RequiredResolution, &record.VerificationMethod, &status, &record.FirstSeenCycle, &record.LastSeenCycle,
		&record.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return findings.Record{}, storage.ErrNotFound
	}
	if err != nil {
		return findings.Record{}, err
	}
	record.Location = json.RawMessage(location)
	record.Status = findings.Status(status)
	record.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return findings.Record{}, err
	}
	record.UpdatedAt, err = time.Parse(timestampFormat, updated)
	return record, err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
