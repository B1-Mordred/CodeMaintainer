package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Store) ApprovePublication(ctx context.Context, jobID string, request storage.PublicationApprovalRequest) (jobs.Job, storage.Approval, error) {
	if jobID == "" || strings.TrimSpace(request.ActorID) == "" ||
		(request.ActorRole != "reviewer" && request.ActorRole != "administrator") ||
		strings.TrimSpace(request.Rationale) == "" || len(request.Rationale) > 4000 ||
		!request.Reauthenticated || request.ExpectedVersion < 1 {
		return jobs.Job{}, storage.Approval{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id=?", jobID))
	if err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	if job.State != jobs.StateAwaitingOperator || job.Version != request.ExpectedVersion || job.ResultSHA == "" {
		return jobs.Job{}, storage.Approval{}, storage.ErrConflict
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM qc_findings WHERE job_id=? AND
		severity IN ('blocker','must_fix') AND status NOT IN (?, ?, ?)`, jobID,
		findings.StatusClosed, findings.StatusHumanWaived, findings.StatusAccepted).Scan(&unresolved); err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	if unresolved != 0 {
		return jobs.Job{}, storage.Approval{}, storage.ErrInvalid
	}
	id, err := NewID("approval")
	if err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	now := s.now()
	approval := storage.Approval{
		ID: id, JobID: jobID, Kind: "draft_publication", SubjectSHA: job.ResultSHA,
		ActorID: request.ActorID, ActorRole: request.ActorRole, Rationale: strings.TrimSpace(request.Rationale),
		Reauthenticated: true, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approvals(
		id,job_id,kind,subject_sha,actor_id,actor_role,rationale,reauthenticated,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, approval.ID, approval.JobID, approval.Kind, approval.SubjectSHA,
		approval.ActorID, approval.ActorRole, approval.Rationale, 1, now.Format(timestampFormat)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return jobs.Job{}, storage.Approval{}, storage.ErrConflict
		}
		return jobs.Job{}, storage.Approval{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
		jobs.StatePublishingBranch, now.Format(timestampFormat), jobID, job.Version)
	if err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return jobs.Job{}, storage.Approval{}, storage.ErrConflict
	}
	details, _ := json.Marshal(map[string]any{
		"approval_id": approval.ID, "subject_sha": approval.SubjectSHA, "reauthenticated": true, "rationale": approval.Rationale,
	})
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_transitions(
		job_id,from_state,to_state,actor_id,reason,details,created_at) VALUES(?,?,?,?,?,?,?)`,
		jobID, job.State, jobs.StatePublishingBranch, approval.ActorID, "draft publication approved", string(details), now.Format(timestampFormat)); err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: approval.ActorID, ActorRole: approval.ActorRole, Action: "publication.approve",
		TargetType: "job", TargetID: jobID, Details: details,
	}); err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, storage.Approval{}, err
	}
	updated, err := s.GetJob(ctx, jobID)
	return updated, approval, err
}

func (s *Store) ListApprovals(ctx context.Context, jobID string) ([]storage.Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,kind,subject_sha,actor_id,actor_role,rationale,reauthenticated,created_at
		FROM approvals WHERE job_id=? ORDER BY created_at,id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]storage.Approval, 0)
	for rows.Next() {
		var item storage.Approval
		var reauthenticated int
		var created string
		if err := rows.Scan(&item.ID, &item.JobID, &item.Kind, &item.SubjectSHA, &item.ActorID,
			&item.ActorRole, &item.Rationale, &reauthenticated, &created); err != nil {
			return nil, err
		}
		item.Reauthenticated = reauthenticated == 1
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
