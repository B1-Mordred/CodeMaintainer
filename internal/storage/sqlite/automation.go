package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/jobs"
)

func (s *Store) SaveSchedule(ctx context.Context, request automation.ScheduleRequest, actor string) (automation.Schedule, error) {
	if err := request.Validate(); err != nil || strings.TrimSpace(actor) == "" {
		return automation.Schedule{}, automation.ErrInvalid
	}
	if _, err := s.GetProject(ctx, request.ProjectID); err != nil {
		return automation.Schedule{}, err
	}
	if request.ID == "" {
		id, err := NewID("schedule")
		if err != nil {
			return automation.Schedule{}, err
		}
		request.ID = id
	}
	now := s.now()
	if request.NextRunAt.IsZero() {
		request.NextRunAt = now.Add(time.Duration(request.IntervalSeconds) * time.Second)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.Schedule{}, err
	}
	defer tx.Rollback()
	var currentVersion int64
	var created string
	err = tx.QueryRowContext(ctx, "SELECT version, created_at FROM schedules WHERE id = ?", request.ID).Scan(&currentVersion, &created)
	createdAt := now
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if request.ExpectedVersion != 0 {
			return automation.Schedule{}, automation.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO schedules(id, project_id, name, task_type, task,
			interval_seconds, window_start_minute, window_end_minute, max_wall_seconds, max_tokens,
			enabled, next_run_at, version, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			request.ID, request.ProjectID, strings.TrimSpace(request.Name), request.TaskType, strings.TrimSpace(request.Task),
			request.IntervalSeconds, request.WindowStartMinute, request.WindowEndMinute, request.MaxWallSeconds,
			request.MaxTokens, request.Enabled, formatTime(request.NextRunAt), formatTime(now), formatTime(now))
		currentVersion = 1
	case err != nil:
		return automation.Schedule{}, err
	default:
		if request.ExpectedVersion != currentVersion {
			return automation.Schedule{}, automation.ErrConflict
		}
		createdAt, err = parseTime(created)
		if err != nil {
			return automation.Schedule{}, err
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE schedules SET project_id = ?, name = ?, task_type = ?, task = ?,
			interval_seconds = ?, window_start_minute = ?, window_end_minute = ?, max_wall_seconds = ?, max_tokens = ?,
			enabled = ?, next_run_at = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
			request.ProjectID, strings.TrimSpace(request.Name), request.TaskType, strings.TrimSpace(request.Task), request.IntervalSeconds,
			request.WindowStartMinute, request.WindowEndMinute, request.MaxWallSeconds, request.MaxTokens, request.Enabled,
			formatTime(request.NextRunAt), formatTime(now), request.ID, request.ExpectedVersion)
		if updateErr != nil {
			return automation.Schedule{}, updateErr
		}
		if err := requireMemoryAffected(result); err != nil {
			return automation.Schedule{}, automation.ErrConflict
		}
		currentVersion++
	}
	if err != nil {
		return automation.Schedule{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actor, ActorRole: "administrator", Action: "schedule.save", TargetType: "schedule", TargetID: request.ID}); err != nil {
		return automation.Schedule{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.Schedule{}, err
	}
	return automation.Schedule{
		ID: request.ID, ProjectID: request.ProjectID, Name: strings.TrimSpace(request.Name), TaskType: request.TaskType,
		Task: strings.TrimSpace(request.Task), IntervalSeconds: request.IntervalSeconds, WindowStartMinute: request.WindowStartMinute,
		WindowEndMinute: request.WindowEndMinute, MaxWallSeconds: request.MaxWallSeconds, MaxTokens: request.MaxTokens,
		Enabled: request.Enabled, NextRunAt: request.NextRunAt.UTC(), Version: currentVersion, CreatedAt: createdAt, UpdatedAt: now,
	}, nil
}

func (s *Store) GetSchedule(ctx context.Context, id string) (automation.Schedule, error) {
	return scanSchedule(s.db.QueryRowContext(ctx, scheduleSelect+" WHERE id = ?", id))
}

func (s *Store) ListSchedules(ctx context.Context, limit int) ([]automation.Schedule, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, scheduleSelect+" ORDER BY name, id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.Schedule{}
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const scheduleSelect = `SELECT id, project_id, name, task_type, task, interval_seconds,
	window_start_minute, window_end_minute, max_wall_seconds, max_tokens, enabled,
	next_run_at, version, created_at, updated_at FROM schedules`

func scanSchedule(scanner interface{ Scan(...any) error }) (automation.Schedule, error) {
	var item automation.Schedule
	var next, created, updated string
	err := scanner.Scan(&item.ID, &item.ProjectID, &item.Name, &item.TaskType, &item.Task, &item.IntervalSeconds,
		&item.WindowStartMinute, &item.WindowEndMinute, &item.MaxWallSeconds, &item.MaxTokens, &item.Enabled,
		&next, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return automation.Schedule{}, automation.ErrNotFound
	}
	if err != nil {
		return automation.Schedule{}, err
	}
	item.NextRunAt, err = parseTime(next)
	if err != nil {
		return automation.Schedule{}, err
	}
	item.CreatedAt, err = parseTime(created)
	if err != nil {
		return automation.Schedule{}, err
	}
	item.UpdatedAt, err = parseTime(updated)
	return item, err
}

func (s *Store) DispatchDueSchedule(ctx context.Context, actor string) (automation.ScheduleRun, error) {
	if strings.TrimSpace(actor) == "" {
		return automation.ScheduleRun{}, automation.ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.ScheduleRun{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, scheduleSelect+" WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at, id LIMIT 1", formatTime(now))
	schedule, err := scanSchedule(row)
	if errors.Is(err, automation.ErrNotFound) {
		return automation.ScheduleRun{}, automation.ErrNoDueSchedule
	}
	if err != nil {
		return automation.ScheduleRun{}, err
	}
	dueAt := schedule.NextRunAt
	next := dueAt
	interval := time.Duration(schedule.IntervalSeconds) * time.Second
	for !next.After(now) {
		next = next.Add(interval)
	}
	runID, err := NewID("schedulerun")
	if err != nil {
		return automation.ScheduleRun{}, err
	}
	run := automation.ScheduleRun{ID: runID, ScheduleID: schedule.ID, DueAt: dueAt, CreatedAt: now}
	if !withinScheduleWindow(now, schedule.WindowStartMinute, schedule.WindowEndMinute) {
		run.Status, run.Reason = "skipped", "outside configured UTC maintenance window"
		_, err = tx.ExecContext(ctx, `INSERT INTO schedule_runs(id, schedule_id, status, reason, due_at, created_at)
			VALUES(?, ?, ?, ?, ?, ?)`, run.ID, run.ScheduleID, run.Status, run.Reason, formatTime(run.DueAt), formatTime(now))
	} else {
		project, projectErr := scanProject(tx.QueryRowContext(ctx, projectSelect+" WHERE id = ? AND enabled = 1", schedule.ProjectID))
		if projectErr != nil {
			return automation.ScheduleRun{}, projectErr
		}
		jobID, idErr := NewID("job")
		if idErr != nil {
			return automation.ScheduleRun{}, idErr
		}
		run.JobID, run.Status, run.Reason = jobID, "enqueued", "scheduled task entered the serial controller queue"
		_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id, project_id, repository, task, state, acceptance_criteria,
			version, max_wall_seconds, deadline_at, max_tokens, reserved_tokens, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, '[]', 1, ?, ?, ?, 0, ?, ?)`, jobID, project.ID,
			project.Repository, schedule.Task, jobs.StateQueued, schedule.MaxWallSeconds,
			formatTime(now.Add(time.Duration(schedule.MaxWallSeconds)*time.Second)), schedule.MaxTokens, formatTime(now), formatTime(now))
		if err == nil {
			details, _ := json.Marshal(map[string]any{"source": "schedule", "schedule_id": schedule.ID, "task_type": schedule.TaskType, "max_wall_seconds": schedule.MaxWallSeconds, "max_tokens": schedule.MaxTokens})
			_, err = tx.ExecContext(ctx, `INSERT INTO job_transitions(job_id, from_state, to_state, actor_id, reason, details, created_at)
				VALUES(?, NULL, ?, ?, ?, ?, ?)`, jobID, jobs.StateQueued, actor, "scheduled job submitted", string(details), formatTime(now))
		}
		if err == nil {
			err = s.snapshotAcceptedJobTx(ctx, tx, jobID, project.ID, now)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schedule_runs(id, schedule_id, job_id, status, reason, due_at, created_at)
				VALUES(?, ?, ?, ?, ?, ?, ?)`, run.ID, run.ScheduleID, run.JobID, run.Status, run.Reason, formatTime(run.DueAt), formatTime(now))
		}
	}
	if err != nil {
		return automation.ScheduleRun{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET next_run_at = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		formatTime(next), formatTime(now), schedule.ID, schedule.Version)
	if err != nil {
		return automation.ScheduleRun{}, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return automation.ScheduleRun{}, automation.ErrConflict
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actor, ActorRole: "system", Action: "schedule.dispatch", TargetType: "schedule_run", TargetID: run.ID}); err != nil {
		return automation.ScheduleRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.ScheduleRun{}, err
	}
	return run, nil
}

func withinScheduleWindow(now time.Time, start, end int) bool {
	if start == end {
		return true
	}
	minute := now.UTC().Hour()*60 + now.UTC().Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func (s *Store) ListScheduleRuns(ctx context.Context, limit int) ([]automation.ScheduleRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, id, schedule_id, COALESCE(job_id, ''), status, reason,
		due_at, created_at FROM schedule_runs ORDER BY sequence DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.ScheduleRun{}
	for rows.Next() {
		var item automation.ScheduleRun
		var due, created string
		if err := rows.Scan(&item.Sequence, &item.ID, &item.ScheduleID, &item.JobID, &item.Status, &item.Reason, &due, &created); err != nil {
			return nil, err
		}
		item.DueAt, err = parseTime(due)
		if err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSkillProposal(ctx context.Context, request automation.SkillProposalRequest) (automation.SkillProposal, error) {
	if err := request.Validate(); err != nil || containsProposalSecret(request.Content) {
		return automation.SkillProposal{}, automation.ErrInvalid
	}
	id, err := NewID("skillproposal")
	if err != nil {
		return automation.SkillProposal{}, err
	}
	now := s.now()
	hash := sha256.Sum256([]byte(request.Content))
	proposal := automation.SkillProposal{ID: id, Name: request.Name, Description: strings.TrimSpace(request.Description), Content: request.Content,
		ContentHash: hex.EncodeToString(hash[:]), Status: "proposed", Version: 1, ProposedBy: request.ProposedBy, Activated: false, CreatedAt: now, UpdatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.SkillProposal{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_proposals(id, name, description, content, content_hash, status,
		version, proposed_by, created_at, updated_at) VALUES(?, ?, ?, ?, ?, 'proposed', 1, ?, ?, ?)`, proposal.ID,
		proposal.Name, proposal.Description, proposal.Content, proposal.ContentHash, proposal.ProposedBy, formatTime(now), formatTime(now))
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO skill_proposal_events(proposal_id, action, actor_id, rationale, created_at)
			VALUES(?, 'proposed', ?, 'proposal remains inert pending administrator review', ?)`, proposal.ID, proposal.ProposedBy, formatTime(now))
	}
	if err != nil {
		return automation.SkillProposal{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: proposal.ProposedBy, ActorRole: "service", Action: "skill.propose", TargetType: "skill_proposal", TargetID: proposal.ID}); err != nil {
		return automation.SkillProposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.SkillProposal{}, err
	}
	return proposal, nil
}

func (s *Store) ReviewSkillProposal(ctx context.Context, id string, request automation.SkillReviewRequest) (automation.SkillProposal, error) {
	if (request.Decision != "approve" && request.Decision != "reject") || strings.TrimSpace(request.Rationale) == "" || request.ActorID == "" {
		return automation.SkillProposal{}, automation.ErrInvalid
	}
	status := "rejected"
	if request.Decision == "approve" {
		status = "approved_inert"
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.SkillProposal{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE skill_proposals SET status = ?, reviewed_by = ?, review_reason = ?,
		version = version + 1, updated_at = ? WHERE id = ? AND status = 'proposed' AND version = ?`, status,
		request.ActorID, strings.TrimSpace(request.Rationale), formatTime(now), id, request.ExpectedVersion)
	if err != nil {
		return automation.SkillProposal{}, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return automation.SkillProposal{}, automation.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_proposal_events(proposal_id, action, actor_id, rationale, created_at)
		VALUES(?, ?, ?, ?, ?)`, id, status, request.ActorID, strings.TrimSpace(request.Rationale), formatTime(now))
	if err != nil {
		return automation.SkillProposal{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: request.ActorID, ActorRole: "administrator", Action: "skill.review", TargetType: "skill_proposal", TargetID: id}); err != nil {
		return automation.SkillProposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.SkillProposal{}, err
	}
	return s.getSkillProposal(ctx, id)
}

func (s *Store) ListSkillProposals(ctx context.Context, limit int) ([]automation.SkillProposal, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, skillProposalSelect+" ORDER BY created_at DESC, id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.SkillProposal{}
	for rows.Next() {
		item, err := scanSkillProposal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const skillProposalSelect = `SELECT id, name, description, content, content_hash, status, version,
	proposed_by, reviewed_by, review_reason, activated, created_at, updated_at FROM skill_proposals`

func (s *Store) getSkillProposal(ctx context.Context, id string) (automation.SkillProposal, error) {
	return scanSkillProposal(s.db.QueryRowContext(ctx, skillProposalSelect+" WHERE id = ?", id))
}

func scanSkillProposal(scanner interface{ Scan(...any) error }) (automation.SkillProposal, error) {
	var item automation.SkillProposal
	var created, updated string
	err := scanner.Scan(&item.ID, &item.Name, &item.Description, &item.Content, &item.ContentHash, &item.Status,
		&item.Version, &item.ProposedBy, &item.ReviewedBy, &item.ReviewReason, &item.Activated, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return automation.SkillProposal{}, automation.ErrNotFound
	}
	if err != nil {
		return automation.SkillProposal{}, err
	}
	item.CreatedAt, err = parseTime(created)
	if err != nil {
		return automation.SkillProposal{}, err
	}
	item.UpdatedAt, err = parseTime(updated)
	return item, err
}

func containsProposalSecret(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{"-----begin private key-----", "github_pat_", "ghp_", "password=", "aws_secret_access_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (s *Store) CreateApprovalRequest(ctx context.Context, jobID, kind, requestedBy, rationale string) (automation.ApprovalRequest, error) {
	if (kind != "review" && kind != "publication_approval") || requestedBy == "" || strings.TrimSpace(rationale) == "" {
		return automation.ApprovalRequest{}, automation.ErrInvalid
	}
	if _, err := s.GetJob(ctx, jobID); err != nil {
		return automation.ApprovalRequest{}, err
	}
	id, err := NewID("automationrequest")
	if err != nil {
		return automation.ApprovalRequest{}, err
	}
	now := s.now()
	request := automation.ApprovalRequest{ID: id, JobID: jobID, Kind: kind, RequestedBy: requestedBy, Rationale: strings.TrimSpace(rationale), CreatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.ApprovalRequest{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO automation_requests(id, job_id, kind, requested_by, rationale, created_at)
		VALUES(?, ?, ?, ?, ?, ?)`, request.ID, request.JobID, request.Kind, request.RequestedBy, request.Rationale, formatTime(now))
	if err != nil {
		return automation.ApprovalRequest{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: requestedBy, ActorRole: "service", Action: "automation.request_" + kind, TargetType: "job", TargetID: jobID}); err != nil {
		return automation.ApprovalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.ApprovalRequest{}, err
	}
	return request, nil
}

func (s *Store) ListApprovalRequests(ctx context.Context, limit int) ([]automation.ApprovalRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_id, kind, requested_by, rationale, created_at
		FROM automation_requests ORDER BY sequence DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.ApprovalRequest{}
	for rows.Next() {
		var item automation.ApprovalRequest
		var created string
		if err := rows.Scan(&item.ID, &item.JobID, &item.Kind, &item.RequestedBy, &item.Rationale, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const notificationSelect = `SELECT sequence, id, kind, project_id, job_id, schedule_id, title, message,
	delivery, state, created_at, read_at FROM notifications`

func (s *Store) ListNotifications(ctx context.Context, state string, limit int) ([]automation.Notification, error) {
	if state != "" && state != "delivered" && state != "read" {
		return nil, automation.ErrInvalid
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := notificationSelect
	arguments := []any{}
	if state != "" {
		query += " WHERE state = ?"
		arguments = append(arguments, state)
	}
	query += " ORDER BY sequence DESC LIMIT ?"
	arguments = append(arguments, limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []automation.Notification{}
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AcknowledgeNotification(ctx context.Context, id, actor string) (automation.Notification, error) {
	if !strings.HasPrefix(id, "notification-") || strings.TrimSpace(actor) == "" {
		return automation.Notification{}, automation.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return automation.Notification{}, err
	}
	defer tx.Rollback()
	current, err := scanNotification(tx.QueryRowContext(ctx, notificationSelect+" WHERE id = ?", id))
	if err != nil {
		return automation.Notification{}, err
	}
	if current.State == "read" {
		return current, nil
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, "UPDATE notifications SET state = 'read', read_at = ? WHERE id = ? AND state = 'delivered'", formatTime(now), id)
	if err != nil {
		return automation.Notification{}, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return automation.Notification{}, automation.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_events(notification_id, action, actor_id, created_at)
		VALUES(?, 'read', ?, ?)`, id, actor, formatTime(now)); err != nil {
		return automation.Notification{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: actor, ActorRole: "operator", Action: "notification.read", TargetType: "notification", TargetID: id,
	}); err != nil {
		return automation.Notification{}, err
	}
	if err := tx.Commit(); err != nil {
		return automation.Notification{}, err
	}
	current.State, current.ReadAt = "read", &now
	return current, nil
}

func scanNotification(scanner interface{ Scan(...any) error }) (automation.Notification, error) {
	var item automation.Notification
	var created string
	var read sql.NullString
	err := scanner.Scan(&item.Sequence, &item.ID, &item.Kind, &item.ProjectID, &item.JobID, &item.ScheduleID,
		&item.Title, &item.Message, &item.Delivery, &item.State, &created, &read)
	if errors.Is(err, sql.ErrNoRows) {
		return automation.Notification{}, automation.ErrNotFound
	}
	if err != nil {
		return automation.Notification{}, err
	}
	item.CreatedAt, err = parseTime(created)
	if err != nil {
		return automation.Notification{}, err
	}
	if read.Valid {
		value, parseErr := parseTime(read.String)
		if parseErr != nil {
			return automation.Notification{}, parseErr
		}
		item.ReadAt = &value
	}
	return item, nil
}
