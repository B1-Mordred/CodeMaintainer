package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migration001 string

const timestampFormat = time.RFC3339Nano

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required: %w", storage.ErrInvalid)
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		dsn = "file:" + filepath.ToSlash(path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure sqlite with %s: %w", pragma, err)
		}
	}
	store := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, migration001); err != nil {
		return fmt.Errorf("apply migration 1: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)",
		s.now().Format(timestampFormat)); err != nil {
		return fmt.Errorf("record migration 1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration 1: %w", err)
	}
	return nil
}

func NewID(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}

func normalizeJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return json.RawMessage(`{}`)
	}
	return value
}

func (s *Store) CreateJob(ctx context.Context, params storage.CreateJobParams) (jobs.Job, error) {
	if params.ProjectID == "" || params.Repository == "" || strings.TrimSpace(params.Task) == "" {
		return jobs.Job{}, fmt.Errorf("project_id, repository, and task are required: %w", storage.ErrInvalid)
	}
	if params.ID == "" {
		id, err := NewID("job")
		if err != nil {
			return jobs.Job{}, err
		}
		params.ID = id
	}
	now := s.now()
	job := jobs.Job{
		ID: params.ID, ProjectID: params.ProjectID, Repository: params.Repository,
		Task: params.Task, IssueNumber: params.IssueNumber, State: jobs.StateQueued,
		AcceptanceCriteria: json.RawMessage(`[]`), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("begin create job: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs(id, project_id, repository, task, issue_number, state,
			acceptance_criteria, version, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.ProjectID, job.Repository, job.Task, job.IssueNumber,
		job.State, string(job.AcceptanceCriteria), job.Version,
		now.Format(timestampFormat), now.Format(timestampFormat))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return jobs.Job{}, storage.ErrConflict
		}
		return jobs.Job{}, fmt.Errorf("insert job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_transitions(job_id, from_state, to_state, actor_id, reason, details, created_at)
		VALUES(?, NULL, ?, ?, ?, ?, ?)`,
		job.ID, job.State, required(params.ActorID, "system"), "job submitted",
		string(normalizeJSON(params.Details)), now.Format(timestampFormat)); err != nil {
		return jobs.Job{}, fmt.Errorf("insert initial transition: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: required(params.ActorID, "system"), ActorRole: "operator",
		Action: "job.create", TargetType: "job", TargetID: job.ID,
		Details: normalizeJSON(params.Details),
	}); err != nil {
		return jobs.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, fmt.Errorf("commit create job: %w", err)
	}
	return job, nil
}

func required(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Store) GetJob(ctx context.Context, id string) (jobs.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, jobSelect+" WHERE id = ?", id))
}

const jobSelect = `SELECT id, project_id, repository, task, issue_number, state,
	base_sha, result_sha, acceptance_criteria, acceptance_criteria_hash,
	review_cycle, version, created_at, updated_at FROM jobs`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (jobs.Job, error) {
	var job jobs.Job
	var issue sql.NullInt64
	var state, criteria, created, updated string
	err := row.Scan(
		&job.ID, &job.ProjectID, &job.Repository, &job.Task, &issue, &state,
		&job.BaseSHA, &job.ResultSHA, &criteria, &job.AcceptanceCriteriaHash,
		&job.ReviewCycle, &job.Version, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, storage.ErrNotFound
	}
	if err != nil {
		return jobs.Job{}, fmt.Errorf("scan job: %w", err)
	}
	job.State = jobs.State(state)
	job.AcceptanceCriteria = json.RawMessage(criteria)
	if issue.Valid {
		job.IssueNumber = &issue.Int64
	}
	job.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("parse job created_at: %w", err)
	}
	job.UpdatedAt, err = time.Parse(timestampFormat, updated)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("parse job updated_at: %w", err)
	}
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context, limit, offset int) ([]jobs.Job, error) {
	limit = boundedLimit(limit, 50, 200)
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, jobSelect+" ORDER BY created_at DESC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	result := make([]jobs.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *Store) TransitionJob(ctx context.Context, id string, request jobs.TransitionRequest) (jobs.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("begin transition: %w", err)
	}
	defer tx.Rollback()
	current, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id = ?", id))
	if err != nil {
		return jobs.Job{}, err
	}
	if request.ExpectedVersion > 0 && request.ExpectedVersion != current.Version {
		return jobs.Job{}, storage.ErrConflict
	}
	if err := jobs.ValidateTransition(current.State, request.To); err != nil {
		return jobs.Job{}, fmt.Errorf("%w: %v", storage.ErrInvalid, err)
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET state = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, request.To, now.Format(timestampFormat), id, current.Version)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("update job state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return jobs.Job{}, fmt.Errorf("check job update: %w", err)
	}
	if rows != 1 {
		return jobs.Job{}, storage.ErrConflict
	}
	details := normalizeJSON(request.Details)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_transitions(job_id, from_state, to_state, actor_id, reason, details, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, id, current.State, request.To,
		required(request.ActorID, "system"), request.Reason, string(details), now.Format(timestampFormat)); err != nil {
		return jobs.Job{}, fmt.Errorf("insert transition: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: required(request.ActorID, "system"), ActorRole: "system",
		Action: "job.transition", TargetType: "job", TargetID: id,
		Details: details,
	}); err != nil {
		return jobs.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, fmt.Errorf("commit transition: %w", err)
	}
	return s.GetJob(ctx, id)
}

func (s *Store) ListTransitions(ctx context.Context, jobID string, after int64, limit int) ([]jobs.Transition, error) {
	limit = boundedLimit(limit, 100, 500)
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, job_id, from_state, to_state, actor_id, reason, details, created_at
		FROM job_transitions WHERE job_id = ? AND sequence > ?
		ORDER BY sequence ASC LIMIT ?`, jobID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list transitions: %w", err)
	}
	defer rows.Close()
	result := make([]jobs.Transition, 0)
	for rows.Next() {
		var item jobs.Transition
		var from sql.NullString
		var to, details, created string
		if err := rows.Scan(&item.Sequence, &item.JobID, &from, &to, &item.ActorID,
			&item.Reason, &details, &created); err != nil {
			return nil, fmt.Errorf("scan transition: %w", err)
		}
		if from.Valid {
			value := jobs.State(from.String)
			item.From = &value
		}
		item.To = jobs.State(to)
		item.Details = json.RawMessage(details)
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, fmt.Errorf("parse transition timestamp: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func boundedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, now func() time.Time, request audit.AppendRequest) error {
	id, err := NewID("audit")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events(id, actor_id, actor_role, action, target_type, target_id,
			correlation_id, remote_address, details, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id,
		required(request.ActorID, "system"), required(request.ActorRole, "system"),
		request.Action, request.TargetType, request.TargetID, request.CorrelationID,
		request.RemoteAddress, string(normalizeJSON(request.Details)), now().Format(timestampFormat))
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func (s *Store) AppendAudit(ctx context.Context, request audit.AppendRequest) (audit.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return audit.Event{}, fmt.Errorf("begin audit append: %w", err)
	}
	defer tx.Rollback()
	if err := appendAuditTx(ctx, tx, s.now, request); err != nil {
		return audit.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return audit.Event{}, fmt.Errorf("commit audit event: %w", err)
	}
	items, err := s.ListAudit(ctx, 0, 1)
	if err != nil || len(items) == 0 {
		return audit.Event{}, err
	}
	return items[0], nil
}

func (s *Store) ListAudit(ctx context.Context, after int64, limit int) ([]audit.Event, error) {
	limit = boundedLimit(limit, 100, 500)
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, id, actor_id, actor_role, action, target_type, target_id,
			correlation_id, remote_address, details, created_at
		FROM audit_events WHERE sequence > ? ORDER BY sequence DESC LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	result := make([]audit.Event, 0)
	for rows.Next() {
		var item audit.Event
		var details, created string
		if err := rows.Scan(&item.Sequence, &item.ID, &item.ActorID, &item.ActorRole,
			&item.Action, &item.TargetType, &item.TargetID, &item.CorrelationID,
			&item.RemoteAddress, &details, &created); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		item.Details = json.RawMessage(details)
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, fmt.Errorf("parse audit timestamp: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CurrentConfig(ctx context.Context) (appconfig.Revision, error) {
	return scanRevision(s.db.QueryRowContext(ctx, revisionSelect+" ORDER BY sequence DESC LIMIT 1"))
}

const revisionSelect = `SELECT id, sequence, actor_id, schema_version, before_document,
	after_document, document_diff, validation_result, rollback_of, created_at FROM config_revisions`

func scanRevision(row scanner) (appconfig.Revision, error) {
	var revision appconfig.Revision
	var before, after, diff, validation, created string
	err := row.Scan(&revision.ID, &revision.Sequence, &revision.ActorID,
		&revision.SchemaVersion, &before, &after, &diff, &validation,
		&revision.RollbackOf, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return appconfig.Revision{}, storage.ErrNotFound
	}
	if err != nil {
		return appconfig.Revision{}, fmt.Errorf("scan configuration revision: %w", err)
	}
	revision.Before = json.RawMessage(before)
	revision.After = json.RawMessage(after)
	revision.Diff = json.RawMessage(diff)
	revision.ValidationResult = json.RawMessage(validation)
	revision.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return appconfig.Revision{}, fmt.Errorf("parse configuration timestamp: %w", err)
	}
	return revision, nil
}

func (s *Store) CreateConfigRevision(ctx context.Context, revision appconfig.Revision) (appconfig.Revision, error) {
	if revision.SchemaVersion != appconfig.SchemaVersion || !json.Valid(revision.After) {
		return appconfig.Revision{}, storage.ErrInvalid
	}
	if revision.ID == "" {
		id, err := NewID("config")
		if err != nil {
			return appconfig.Revision{}, err
		}
		revision.ID = id
	}
	revision.CreatedAt = s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return appconfig.Revision{}, fmt.Errorf("begin configuration revision: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO config_revisions(id, actor_id, schema_version, before_document,
			after_document, document_diff, validation_result, rollback_of, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, revision.ID,
		required(revision.ActorID, "system"), revision.SchemaVersion,
		string(normalizeJSON(revision.Before)), string(revision.After),
		string(normalizeJSON(revision.Diff)), string(normalizeJSON(revision.ValidationResult)),
		revision.RollbackOf, revision.CreatedAt.Format(timestampFormat))
	if err != nil {
		return appconfig.Revision{}, fmt.Errorf("insert configuration revision: %w", err)
	}
	revision.Sequence, err = result.LastInsertId()
	if err != nil {
		return appconfig.Revision{}, fmt.Errorf("read configuration sequence: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: required(revision.ActorID, "system"), ActorRole: "administrator",
		Action: "config.revise", TargetType: "config_revision", TargetID: revision.ID,
		Details: json.RawMessage(fmt.Sprintf(`{"schema_version":%d}`, revision.SchemaVersion)),
	}); err != nil {
		return appconfig.Revision{}, err
	}
	if err := tx.Commit(); err != nil {
		return appconfig.Revision{}, fmt.Errorf("commit configuration revision: %w", err)
	}
	return revision, nil
}

func (s *Store) ListConfigRevisions(ctx context.Context, limit int) ([]appconfig.Revision, error) {
	limit = boundedLimit(limit, 50, 200)
	rows, err := s.db.QueryContext(ctx, revisionSelect+" ORDER BY sequence DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list configuration revisions: %w", err)
	}
	defer rows.Close()
	result := make([]appconfig.Revision, 0)
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	return result, rows.Err()
}
