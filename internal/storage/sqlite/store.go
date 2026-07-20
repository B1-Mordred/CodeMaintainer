package sqlite

import (
	"bytes"
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

//go:embed migrations/002_queue_leases.sql
var migration002 string

//go:embed migrations/003_artifacts.sql
var migration003 string

//go:embed migrations/004_qc_findings.sql
var migration004 string

//go:embed migrations/005_workflow_phases.sql
var migration005 string

//go:embed migrations/006_projects.sql
var migration006 string

//go:embed migrations/007_approvals.sql
var migration007 string

//go:embed migrations/008_auth.sql
var migration008 string

//go:embed migrations/009_memory.sql
var migration009 string

//go:embed migrations/010_memory_index_queue.sql
var migration010 string

//go:embed migrations/011_automation.sql
var migration011 string

//go:embed migrations/012_job_budgets.sql
var migration012 string

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
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, migration := range []struct {
		version int
		sql     string
	}{{1, migration001}, {2, migration002}, {3, migration003}, {4, migration004}, {5, migration005}, {6, migration006}, {7, migration007}, {8, migration008}, {9, migration009}, {10, migration010}, {11, migration011}, {12, migration012}} {
		var applied int
		if err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied != 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)",
			migration.version, s.now().Format(timestampFormat)); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
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
	if params.MaxWallSeconds == 0 {
		params.MaxWallSeconds = 14_400
	}
	if params.MaxTokens == 0 {
		params.MaxTokens = 262_144
	}
	if params.MaxWallSeconds < 60 || params.MaxWallSeconds > 604_800 || params.MaxTokens < 1 || params.MaxTokens > 10_000_000 {
		return jobs.Job{}, storage.ErrInvalid
	}
	now := s.now()
	job := jobs.Job{
		ID: params.ID, ProjectID: params.ProjectID, Repository: params.Repository,
		Task: params.Task, IssueNumber: params.IssueNumber, State: jobs.StateQueued,
		AcceptanceCriteria: json.RawMessage(`[]`), Version: 1, MaxWallSeconds: params.MaxWallSeconds,
		DeadlineAt: now.Add(time.Duration(params.MaxWallSeconds) * time.Second), MaxTokens: params.MaxTokens,
		CreatedAt: now, UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("begin create job: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs(id, project_id, repository, task, issue_number, state,
			acceptance_criteria, version, max_wall_seconds, deadline_at, max_tokens, reserved_tokens, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		job.ID, job.ProjectID, job.Repository, job.Task, job.IssueNumber,
		job.State, string(job.AcceptanceCriteria), job.Version, job.MaxWallSeconds, job.DeadlineAt.Format(timestampFormat), job.MaxTokens,
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
	review_cycle, version, max_wall_seconds, deadline_at, max_tokens, reserved_tokens, created_at, updated_at FROM jobs`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (jobs.Job, error) {
	var job jobs.Job
	var issue sql.NullInt64
	var state, criteria, deadline, created, updated string
	err := row.Scan(
		&job.ID, &job.ProjectID, &job.Repository, &job.Task, &issue, &state,
		&job.BaseSHA, &job.ResultSHA, &criteria, &job.AcceptanceCriteriaHash,
		&job.ReviewCycle, &job.Version, &job.MaxWallSeconds, &deadline, &job.MaxTokens, &job.ReservedTokens, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, storage.ErrNotFound
	}
	if err != nil {
		return jobs.Job{}, fmt.Errorf("scan job: %w", err)
	}
	job.State = jobs.State(state)
	job.AcceptanceCriteria = json.RawMessage(criteria)
	if deadline != "" {
		job.DeadlineAt, err = time.Parse(timestampFormat, deadline)
		if err != nil {
			return jobs.Job{}, fmt.Errorf("parse job deadline_at: %w", err)
		}
	}
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

func (s *Store) GetConfigRevision(ctx context.Context, id string) (appconfig.Revision, error) {
	return scanRevision(s.db.QueryRowContext(ctx, revisionSelect+" WHERE id = ?", id))
}

const revisionSelect = `SELECT id, sequence, actor_id, schema_version, before_document,
	after_document, document_diff, validation_result, rollback_of, reason, created_at FROM config_revisions`

func scanRevision(row scanner) (appconfig.Revision, error) {
	var revision appconfig.Revision
	var before, after, diff, validation, created string
	err := row.Scan(&revision.ID, &revision.Sequence, &revision.ActorID,
		&revision.SchemaVersion, &before, &after, &diff, &validation,
		&revision.RollbackOf, &revision.Reason, &created)
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
			after_document, document_diff, validation_result, rollback_of, reason, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, revision.ID,
		required(revision.ActorID, "system"), revision.SchemaVersion,
		string(normalizeJSON(revision.Before)), string(revision.After),
		string(normalizeJSON(revision.Diff)), string(normalizeJSON(revision.ValidationResult)),
		revision.RollbackOf, revision.Reason, revision.CreatedAt.Format(timestampFormat))
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
		Details: json.RawMessage(fmt.Sprintf(`{"schema_version":%d,"rollback_of":%q,"reason":%q}`,
			revision.SchemaVersion, revision.RollbackOf, revision.Reason)),
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

func (s *Store) AcquireJobLease(ctx context.Context, ownerID string, ttl time.Duration) (jobs.Job, storage.JobLease, error) {
	if strings.TrimSpace(ownerID) == "" || ttl < time.Second || ttl > time.Hour {
		return jobs.Job{}, storage.JobLease{}, storage.ErrInvalid
	}
	resumable := make([]jobs.State, 0)
	for _, state := range jobs.AllStates() {
		if state.Resumable() {
			resumable = append(resumable, state)
		}
	}
	placeholders := make([]string, len(resumable))
	arguments := make([]any, 0, len(resumable)+2)
	for index, state := range resumable {
		placeholders[index] = "?"
		arguments = append(arguments, state)
	}
	now := s.now()
	arguments = append(arguments, now.Format(timestampFormat))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("begin lease acquisition: %w", err)
	}
	defer tx.Rollback()
	query := jobSelect + ` AS j LEFT JOIN job_leases AS l ON l.job_id = j.id
		WHERE j.state IN (` + strings.Join(placeholders, ",") + `)
		AND (l.job_id IS NULL OR l.expires_at <= ?)
		ORDER BY j.created_at ASC LIMIT 1`
	job, err := scanJob(tx.QueryRowContext(ctx, query, arguments...))
	if errors.Is(err, storage.ErrNotFound) {
		return jobs.Job{}, storage.JobLease{}, storage.ErrNoLeaseAvailable
	}
	if err != nil {
		return jobs.Job{}, storage.JobLease{}, err
	}
	lease := storage.JobLease{
		JobID: job.ID, OwnerID: ownerID, AcquiredAt: now,
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
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, storage.JobLease{}, fmt.Errorf("commit lease acquisition: %w", err)
	}
	return job, lease, nil
}

func (s *Store) RenewJobLease(ctx context.Context, jobID, ownerID string, ttl time.Duration) (storage.JobLease, error) {
	if jobID == "" || ownerID == "" || ttl < time.Second || ttl > time.Hour {
		return storage.JobLease{}, storage.ErrInvalid
	}
	now := s.now()
	expires := now.Add(ttl)
	result, err := s.db.ExecContext(ctx, `UPDATE job_leases
		SET heartbeat_at = ?, expires_at = ?
		WHERE job_id = ? AND owner_id = ? AND expires_at > ?`,
		now.Format(timestampFormat), expires.Format(timestampFormat), jobID, ownerID, now.Format(timestampFormat))
	if err != nil {
		return storage.JobLease{}, fmt.Errorf("renew job lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return storage.JobLease{}, fmt.Errorf("inspect lease renewal: %w", err)
	}
	if changed != 1 {
		return storage.JobLease{}, storage.ErrLeaseLost
	}
	return s.getJobLease(ctx, jobID)
}

func (s *Store) ReleaseJobLease(ctx context.Context, jobID, ownerID string) error {
	if jobID == "" || ownerID == "" {
		return storage.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM job_leases WHERE job_id = ? AND owner_id = ?", jobID, ownerID)
	if err != nil {
		return fmt.Errorf("release job lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect lease release: %w", err)
	}
	if changed == 1 {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM job_leases WHERE job_id = ?", jobID).Scan(&count); err != nil {
		return fmt.Errorf("check released lease: %w", err)
	}
	if count == 0 {
		return nil
	}
	return storage.ErrLeaseLost
}

func (s *Store) getJobLease(ctx context.Context, jobID string) (storage.JobLease, error) {
	var lease storage.JobLease
	var acquired, heartbeat, expires string
	err := s.db.QueryRowContext(ctx, `SELECT job_id, owner_id, acquired_at, heartbeat_at, expires_at
		FROM job_leases WHERE job_id = ?`, jobID).Scan(
		&lease.JobID, &lease.OwnerID, &acquired, &heartbeat, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.JobLease{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.JobLease{}, fmt.Errorf("read job lease: %w", err)
	}
	for _, item := range []struct {
		value       string
		destination *time.Time
	}{{acquired, &lease.AcquiredAt}, {heartbeat, &lease.HeartbeatAt}, {expires, &lease.ExpiresAt}} {
		parsed, err := time.Parse(timestampFormat, item.value)
		if err != nil {
			return storage.JobLease{}, fmt.Errorf("parse job lease timestamp: %w", err)
		}
		*item.destination = parsed
	}
	return lease, nil
}

func (s *Store) IndexArtifact(ctx context.Context, record storage.ArtifactRecord) (storage.ArtifactRecord, error) {
	_, digestErr := hex.DecodeString(record.ObjectSHA256)
	expectedPath := filepath.ToSlash(filepath.Join("objects", firstTwo(record.ObjectSHA256), record.ObjectSHA256))
	if record.ID == "" || record.JobID == "" || record.ProjectID == "" || len(record.ObjectSHA256) != 64 ||
		digestErr != nil || record.Bytes < 0 || record.RelativePath != expectedPath ||
		record.Kind == "" || record.MediaType == "" || record.Producer == "" ||
		!json.Valid(record.Metadata) {
		return storage.ArtifactRecord{}, storage.ErrInvalid
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = s.now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("begin artifact index: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_objects(sha256, bytes, relative_path, created_at)
		VALUES(?, ?, ?, ?) ON CONFLICT(sha256) DO NOTHING`, record.ObjectSHA256, record.Bytes,
		record.RelativePath, record.CreatedAt.Format(timestampFormat)); err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("index artifact object: %w", err)
	}
	var existingBytes int64
	var existingPath string
	if err := tx.QueryRowContext(ctx, "SELECT bytes, relative_path FROM artifact_objects WHERE sha256 = ?", record.ObjectSHA256).
		Scan(&existingBytes, &existingPath); err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("verify artifact object: %w", err)
	}
	if existingBytes != record.Bytes || existingPath != record.RelativePath {
		return storage.ArtifactRecord{}, storage.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_artifacts(
		id, job_id, project_id, object_sha256, kind, media_type, producer, metadata, idempotency_key, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.JobID, record.ProjectID,
		record.ObjectSHA256, record.Kind, record.MediaType, record.Producer,
		string(record.Metadata), record.IdempotencyKey, record.CreatedAt.Format(timestampFormat)); err != nil {
		if record.IdempotencyKey == "" || !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return storage.ArtifactRecord{}, fmt.Errorf("index job artifact: %w", err)
		}
		existing, lookupErr := scanArtifact(tx.QueryRowContext(ctx,
			artifactSelect+" WHERE a.job_id = ? AND a.idempotency_key = ?", record.JobID, record.IdempotencyKey))
		if lookupErr != nil {
			return storage.ArtifactRecord{}, lookupErr
		}
		if existing.ProjectID != record.ProjectID || existing.ObjectSHA256 != record.ObjectSHA256 ||
			existing.Kind != record.Kind || existing.MediaType != record.MediaType || existing.Producer != record.Producer ||
			!bytes.Equal(existing.Metadata, record.Metadata) {
			return storage.ArtifactRecord{}, storage.ErrIdempotencyKey
		}
		return existing, nil
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: record.Producer, ActorRole: "service", Action: "artifact.index",
		TargetType: "artifact", TargetID: record.ID,
		Details: json.RawMessage(fmt.Sprintf(`{"job_id":%q,"sha256":%q,"bytes":%d,"kind":%q}`,
			record.JobID, record.ObjectSHA256, record.Bytes, record.Kind)),
	}); err != nil {
		return storage.ArtifactRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("commit artifact index: %w", err)
	}
	return record, nil
}

func firstTwo(value string) string {
	if len(value) < 2 {
		return ""
	}
	return value[:2]
}

const artifactSelect = `SELECT a.id, a.job_id, a.project_id, a.object_sha256, o.bytes,
	o.relative_path, a.kind, a.media_type, a.producer, a.metadata, a.idempotency_key, a.created_at
	FROM job_artifacts AS a JOIN artifact_objects AS o ON o.sha256 = a.object_sha256`

func (s *Store) GetArtifact(ctx context.Context, jobID, artifactID string) (storage.ArtifactRecord, error) {
	return scanArtifact(s.db.QueryRowContext(ctx, artifactSelect+" WHERE a.job_id = ? AND a.id = ?", jobID, artifactID))
}

func (s *Store) ListJobArtifacts(ctx context.Context, jobID string, limit int) ([]storage.ArtifactRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, artifactSelect+" WHERE a.job_id = ? ORDER BY a.created_at, a.id LIMIT ?", jobID, limit)
	if err != nil {
		return nil, fmt.Errorf("list job artifacts: %w", err)
	}
	defer rows.Close()
	items := make([]storage.ArtifactRecord, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanArtifact(row scanner) (storage.ArtifactRecord, error) {
	var record storage.ArtifactRecord
	var metadata, created string
	if err := row.Scan(&record.ID, &record.JobID, &record.ProjectID, &record.ObjectSHA256,
		&record.Bytes, &record.RelativePath, &record.Kind, &record.MediaType,
		&record.Producer, &metadata, &record.IdempotencyKey, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ArtifactRecord{}, storage.ErrNotFound
		}
		return storage.ArtifactRecord{}, fmt.Errorf("scan artifact: %w", err)
	}
	record.Metadata = json.RawMessage(metadata)
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("parse artifact timestamp: %w", err)
	}
	record.CreatedAt = parsed
	return record, nil
}
