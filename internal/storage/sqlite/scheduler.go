package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/scheduler"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

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
	rejected, _ := json.Marshal(decision.RejectedJobIDs)
	deferred, _ := json.Marshal(decision.DeferredJobIDs)
	_, err := s.db.ExecContext(ctx, `INSERT INTO scheduler_decisions(
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
