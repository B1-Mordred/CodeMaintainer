package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) ReserveJobTokens(ctx context.Context, jobID string, phaseVersion int64, phaseState jobs.State, amount int) (jobs.Job, error) {
	if jobID == "" || phaseVersion < 1 || !phaseState.Valid() || amount < 1 || amount > 1_000_000 {
		return jobs.Job{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id = ?", jobID))
	if err != nil {
		return jobs.Job{}, err
	}
	if job.Version != phaseVersion || job.State != phaseState {
		return jobs.Job{}, storage.ErrConflict
	}
	var existing int
	err = tx.QueryRowContext(ctx, "SELECT reserved_tokens FROM job_token_reservations WHERE job_id = ? AND phase_version = ?", jobID, phaseVersion).Scan(&existing)
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, err
	}
	if job.ReservedTokens+amount > job.MaxTokens {
		return jobs.Job{}, storage.ErrBudgetExceeded
	}
	now := s.now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_token_reservations(job_id, phase_version, phase_state, reserved_tokens, created_at)
		VALUES(?, ?, ?, ?, ?)`, jobID, phaseVersion, phaseState, amount, formatTime(now)); err != nil {
		return jobs.Job{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET reserved_tokens = reserved_tokens + ?, updated_at = ? WHERE id = ? AND version = ?", amount, formatTime(now), jobID, phaseVersion); err != nil {
		return jobs.Job{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: "workflow-engine", ActorRole: "system", Action: "job.tokens_reserve", TargetType: "job", TargetID: jobID}); err != nil {
		return jobs.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, err
	}
	return s.GetJob(ctx, jobID)
}
