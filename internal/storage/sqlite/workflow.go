package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

var workflowCommit = regexp.MustCompile(`^[a-f0-9]{40}(?:[a-f0-9]{24})?$`)

func (s *Store) CompletePhase(ctx context.Context, request storage.PhaseCompletion) (jobs.Job, error) {
	if request.JobID == "" || request.ExpectedVersion <= 0 || request.PhaseState == "" ||
		!json.Valid(normalizeJSON(request.Details)) || !json.Valid(request.Outcome) || len(request.Outcome) > 2<<20 {
		return jobs.Job{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("begin phase completion: %w", err)
	}
	defer tx.Rollback()
	current, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id = ?", request.JobID))
	if err != nil {
		return jobs.Job{}, err
	}
	if current.Version != request.ExpectedVersion || current.State != request.PhaseState {
		return jobs.Job{}, storage.ErrConflict
	}
	if err := jobs.ValidateTransition(current.State, request.To); err != nil {
		return jobs.Job{}, fmt.Errorf("%w: %v", storage.ErrInvalid, err)
	}
	updated, err := applyMetadata(current, request.Metadata)
	if err != nil {
		return jobs.Job{}, err
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state = ?, base_sha = ?, result_sha = ?,
		acceptance_criteria = ?, acceptance_criteria_hash = ?, review_cycle = ?,
		version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		request.To, updated.BaseSHA, updated.ResultSHA, string(updated.AcceptanceCriteria),
		updated.AcceptanceCriteriaHash, updated.ReviewCycle, now.Format(timestampFormat),
		request.JobID, current.Version)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("update completed phase: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return jobs.Job{}, storage.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_phase_records(
		job_id, phase_state, phase_version, outcome, created_at) VALUES(?, ?, ?, ?, ?)`,
		request.JobID, request.PhaseState, current.Version, string(request.Outcome), now.Format(timestampFormat)); err != nil {
		return jobs.Job{}, fmt.Errorf("record completed phase: %w", err)
	}
	details := normalizeJSON(request.Details)
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_transitions(
		job_id, from_state, to_state, actor_id, reason, details, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, request.JobID, current.State, request.To,
		required(request.ActorID, "workflow-engine"), request.Reason, string(details), now.Format(timestampFormat)); err != nil {
		return jobs.Job{}, fmt.Errorf("record completed-phase transition: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: required(request.ActorID, "workflow-engine"), ActorRole: "system",
		Action: "workflow.phase.complete", TargetType: "job", TargetID: request.JobID, Details: details,
	}); err != nil {
		return jobs.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, fmt.Errorf("commit completed phase: %w", err)
	}
	return s.GetJob(ctx, request.JobID)
}

func applyMetadata(current jobs.Job, patch storage.JobMetadataPatch) (jobs.Job, error) {
	updated := current
	if patch.BaseSHA != nil {
		if !workflowCommit.MatchString(*patch.BaseSHA) || (current.BaseSHA != "" && current.BaseSHA != *patch.BaseSHA) {
			return jobs.Job{}, storage.ErrInvalid
		}
		updated.BaseSHA = *patch.BaseSHA
	}
	if patch.ResultSHA != nil {
		if *patch.ResultSHA != "" && !workflowCommit.MatchString(*patch.ResultSHA) {
			return jobs.Job{}, storage.ErrInvalid
		}
		updated.ResultSHA = *patch.ResultSHA
	}
	if patch.AcceptanceCriteria != nil || patch.AcceptanceCriteriaHash != nil {
		if patch.AcceptanceCriteria == nil || patch.AcceptanceCriteriaHash == nil ||
			len(*patch.AcceptanceCriteria) == 0 || len(*patch.AcceptanceCriteria) > 64<<10 || !json.Valid(*patch.AcceptanceCriteria) {
			return jobs.Job{}, storage.ErrInvalid
		}
		var criteria []json.RawMessage
		if err := json.Unmarshal(*patch.AcceptanceCriteria, &criteria); err != nil || len(criteria) == 0 || len(criteria) > 64 {
			return jobs.Job{}, storage.ErrInvalid
		}
		digest := sha256.Sum256(*patch.AcceptanceCriteria)
		if *patch.AcceptanceCriteriaHash != hex.EncodeToString(digest[:]) ||
			(current.AcceptanceCriteriaHash != "" && current.AcceptanceCriteriaHash != *patch.AcceptanceCriteriaHash) {
			return jobs.Job{}, storage.ErrInvalid
		}
		updated.AcceptanceCriteria = append(json.RawMessage(nil), (*patch.AcceptanceCriteria)...)
		updated.AcceptanceCriteriaHash = *patch.AcceptanceCriteriaHash
	}
	if patch.ReviewCycle != nil {
		if *patch.ReviewCycle < current.ReviewCycle || *patch.ReviewCycle > current.ReviewCycle+1 || *patch.ReviewCycle > 10 {
			return jobs.Job{}, storage.ErrInvalid
		}
		updated.ReviewCycle = *patch.ReviewCycle
	}
	return updated, nil
}

func (s *Store) ListPhaseRecords(ctx context.Context, jobID string, limit int) ([]storage.PhaseRecord, error) {
	if jobID == "" {
		return nil, storage.ErrInvalid
	}
	limit = boundedLimit(limit, 100, 500)
	rows, err := s.db.QueryContext(ctx, `SELECT job_id, phase_state, phase_version, outcome, created_at
		FROM workflow_phase_records WHERE job_id = ? ORDER BY phase_version LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, fmt.Errorf("list phase records: %w", err)
	}
	defer rows.Close()
	items := make([]storage.PhaseRecord, 0)
	for rows.Next() {
		var item storage.PhaseRecord
		var state, outcome, created string
		if err := rows.Scan(&item.JobID, &state, &item.PhaseVersion, &outcome, &created); err != nil {
			return nil, err
		}
		item.PhaseState = jobs.State(state)
		item.Outcome = json.RawMessage(outcome)
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
