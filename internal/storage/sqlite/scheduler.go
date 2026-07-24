package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/scheduler"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

type schedulerDecisionExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) RecordSchedulerDecision(ctx context.Context, decision scheduler.Decision) (scheduler.Decision, error) {
	if decision.ID == "" {
		id, err := NewID("scheduler")
		if err != nil {
			return scheduler.Decision{}, err
		}
		decision.ID = id
	}
	decision.CreatedAt = s.now()
	if err := decision.Validate(); err != nil {
		return scheduler.Decision{}, storage.ErrInvalid
	}
	return insertSchedulerDecision(ctx, s.db, decision)
}

func insertSchedulerDecision(ctx context.Context, executor schedulerDecisionExecutor, decision scheduler.Decision) (scheduler.Decision, error) {
	if err := decision.Validate(); err != nil {
		return scheduler.Decision{}, storage.ErrInvalid
	}
	rejected, _ := json.Marshal(decision.RejectedJobIDs)
	deferred, _ := json.Marshal(decision.DeferredJobIDs)
	_, err := executor.ExecContext(ctx, `INSERT INTO scheduler_decisions(
		id,mode,selected_job_id,selected_project_id,selected_profile_id,status,reason,
		rejected_job_ids_json,deferred_job_ids_json,co_residence_safe,fairness_applied,
		model_batch_group,resource_summary,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		decision.ID, decision.Mode, nullEmpty(decision.SelectedJobID), nullEmpty(decision.SelectedProjectID),
		nullEmpty(decision.SelectedProfileID), decision.Status, decision.Reason, string(rejected), string(deferred),
		boolInt(decision.CoResidenceSafe), boolInt(decision.FairnessApplied), decision.ModelBatchGroup,
		decision.ResourceSummary, decision.CreatedAt.Format(timestampFormat))
	if err != nil {
		return scheduler.Decision{}, err
	}
	return decision, nil
}

func (s *Store) acquireScheduledJobLease(ctx context.Context, ownerID string, ttl time.Duration) (jobs.Job, storage.JobLease, error) {
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("begin lease acquisition: %w", err)
	}
	defer tx.Rollback()

	active, err := s.schedulerActiveJobs(ctx, tx, now)
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, err
	}
	candidates, err := s.schedulerCandidateJobs(ctx, tx, now)
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, err
	}
	if len(candidates) == 0 {
		return jobs.Job{}, storage.JobLease{}, storage.ErrNoLeaseAvailable
	}
	request := scheduler.SimulationRequest{
		Mode:              scheduler.ModeQualityLatency,
		Topology:          scheduler.DefaultTopology(),
		Profiles:          scheduler.DefaultProfiles(),
		Active:            schedulerQueueItems(active, now),
		Queued:            schedulerQueueItems(candidates, now),
		MaintenanceWindow: true,
		FairnessWindow:    1,
	}
	decision, err := scheduler.Simulate(request, now)
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("simulate scheduler lease decision: %w", err)
	}
	if decision.ID == "" {
		id, err := NewID("scheduler")
		if err != nil {
			return jobs.Job{}, storage.JobLease{}, err
		}
		decision.ID = id
	}
	decision.CreatedAt = now
	if decision.Status != scheduler.DecisionScheduled {
		if _, err := insertSchedulerDecision(ctx, tx, decision); err != nil {
			return jobs.Job{}, storage.JobLease{}, err
		}
		if err := tx.Commit(); err != nil {
			return jobs.Job{}, storage.JobLease{}, fmt.Errorf("commit scheduler deferral: %w", err)
		}
		return jobs.Job{}, storage.JobLease{}, storage.ErrNoLeaseAvailable
	}
	selected, ok := findJob(candidates, decision.SelectedJobID)
	if !ok {
		return jobs.Job{}, storage.JobLease{}, storage.ErrNoLeaseAvailable
	}
	lease := storage.JobLease{
		JobID: selected.ID, OwnerID: ownerID, AcquiredAt: now,
		HeartbeatAt: now, ExpiresAt: now.Add(ttl),
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO job_leases(
		job_id, owner_id, acquired_at, heartbeat_at, expires_at) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET owner_id=excluded.owner_id,
			acquired_at=excluded.acquired_at, heartbeat_at=excluded.heartbeat_at,
			expires_at=excluded.expires_at
		WHERE job_leases.expires_at <= ?`, lease.JobID, lease.OwnerID,
		lease.AcquiredAt.Format(timestampFormat), lease.HeartbeatAt.Format(timestampFormat),
		lease.ExpiresAt.Format(timestampFormat), now.Format(timestampFormat))
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("acquire job lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("inspect lease acquisition: %w", err)
	}
	if changed != 1 {
		return jobs.Job{}, storage.JobLease{}, storage.ErrNoLeaseAvailable
	}
	if _, err := insertSchedulerDecision(ctx, tx, decision); err != nil {
		return jobs.Job{}, storage.JobLease{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("commit lease acquisition: %w", err)
	}
	return selected, lease, nil
}

func (s *Store) schedulerActiveJobs(ctx context.Context, tx *sql.Tx, now time.Time) ([]jobs.Job, error) {
	rows, err := tx.QueryContext(ctx, jobSelect+` AS j JOIN job_leases AS l ON l.job_id = j.id WHERE l.expires_at > ? ORDER BY j.created_at ASC`,
		now.Format(timestampFormat))
	if err != nil {
		return nil, fmt.Errorf("list active scheduler jobs: %w", err)
	}
	defer rows.Close()
	return scanJobs(rows)
}

func (s *Store) schedulerCandidateJobs(ctx context.Context, tx *sql.Tx, now time.Time) ([]jobs.Job, error) {
	resumable := make([]jobs.State, 0)
	for _, state := range jobs.AllStates() {
		if state.Resumable() {
			resumable = append(resumable, state)
		}
	}
	placeholders := make([]string, len(resumable))
	arguments := make([]any, 0, len(resumable)+1)
	for index, state := range resumable {
		placeholders[index] = "?"
		arguments = append(arguments, state)
	}
	arguments = append(arguments, now.Format(timestampFormat))
	rows, err := tx.QueryContext(ctx, jobSelect+` AS j LEFT JOIN job_leases AS l ON l.job_id = j.id
		WHERE j.state IN (`+strings.Join(placeholders, ",")+`)
		AND (l.job_id IS NULL OR l.expires_at <= ?)
		ORDER BY j.created_at ASC LIMIT 128`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list scheduler candidates: %w", err)
	}
	defer rows.Close()
	return scanJobs(rows)
}

func scanJobs(rows *sql.Rows) ([]jobs.Job, error) {
	items := []jobs.Job{}
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func schedulerQueueItems(items []jobs.Job, now time.Time) []scheduler.QueueItem {
	result := make([]scheduler.QueueItem, 0, len(items))
	for _, item := range items {
		profileID := scheduler.ProfileIDForState(string(item.State))
		result = append(result, scheduler.QueueItem{
			JobID:      item.ID,
			ProjectID:  item.ProjectID,
			State:      string(item.State),
			Priority:   scheduler.PriorityForDeadline(now, item.DeadlineAt, item.CreatedAt),
			ProfileID:  profileID,
			CreatedAt:  item.CreatedAt,
			DeadlineAt: item.DeadlineAt,
			ModelID:    scheduler.ModelIDForProfile(profileID),
			RunnerID:   scheduler.RunnerIDForProfile(profileID),
			Reason:     "durable lease scheduler candidate",
		})
	}
	return result
}

func findJob(items []jobs.Job, id string) (jobs.Job, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return jobs.Job{}, false
}

func (s *Store) ListSchedulerDecisions(ctx context.Context, limit int) ([]scheduler.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,mode,selected_job_id,selected_project_id,selected_profile_id,status,reason,
		rejected_job_ids_json,deferred_job_ids_json,co_residence_safe,fairness_applied,
		model_batch_group,resource_summary,created_at
		FROM scheduler_decisions ORDER BY created_at DESC,id DESC LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []scheduler.Decision{}
	for rows.Next() {
		item, err := scanSchedulerDecision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSchedulerDecision(row scanner) (scheduler.Decision, error) {
	var item scheduler.Decision
	var selectedJob, selectedProject, selectedProfile sql.NullString
	var rejected, deferred, created string
	var safe, fairness int
	err := row.Scan(&item.ID, &item.Mode, &selectedJob, &selectedProject, &selectedProfile, &item.Status, &item.Reason,
		&rejected, &deferred, &safe, &fairness, &item.ModelBatchGroup, &item.ResourceSummary, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduler.Decision{}, storage.ErrNotFound
	}
	if err != nil {
		return scheduler.Decision{}, err
	}
	item.SelectedJobID = selectedJob.String
	item.SelectedProjectID = selectedProject.String
	item.SelectedProfileID = selectedProfile.String
	item.CoResidenceSafe = safe == 1
	item.FairnessApplied = fairness == 1
	if err := json.Unmarshal([]byte(rejected), &item.RejectedJobIDs); err != nil {
		return scheduler.Decision{}, err
	}
	if err := json.Unmarshal([]byte(deferred), &item.DeferredJobIDs); err != nil {
		return scheduler.Decision{}, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return scheduler.Decision{}, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func nullEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
