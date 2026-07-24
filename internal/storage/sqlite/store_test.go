package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestJobLifecyclePersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ProjectID: "project-one", Repository: "owner/repo", Task: "repair defect", ActorID: "operator-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: jobs.StateSyncing, ActorID: "engine", Reason: "start synchronization", ExpectedVersion: job.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != jobs.StateSyncing || job.Version != 2 {
		t.Fatalf("unexpected transitioned job: %#v", job)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.State != jobs.StateSyncing || restored.Version != 2 {
		t.Fatalf("restart lost durable state: %#v", restored)
	}
	transitions, err := reopened.ListTransitions(ctx, job.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 || transitions[0].From != nil || transitions[1].To != jobs.StateSyncing {
		t.Fatalf("unexpected transition history: %#v", transitions)
	}
	auditEvents, err := reopened.ListAudit(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(auditEvents) != 2 {
		t.Fatalf("expected create and transition audit events, got %d", len(auditEvents))
	}
}

func TestInvalidAndStaleTransitionsAreAtomic(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ProjectID: "project-one", Repository: "owner/repo", Task: "repair defect", ActorID: "operator-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: jobs.StateCompleted, ActorID: "attacker", Reason: "skip gates", ExpectedVersion: job.Version,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
	if _, err := store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: jobs.StateSyncing, ActorID: "engine", Reason: "stale request", ExpectedVersion: job.Version + 10,
	}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	unchanged, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.State != jobs.StateQueued || unchanged.Version != 1 {
		t.Fatalf("failed transition changed job: %#v", unchanged)
	}
	transitions, err := store.ListTransitions(ctx, job.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("failed transition leaked history, got %d rows", len(transitions))
	}
}

func TestAppendOnlyTablesRejectMutation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ProjectID: "project-one", Repository: "owner/repo", Task: "repair defect", ActorID: "operator-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"audit update":      "UPDATE audit_events SET action='changed'",
		"audit delete":      "DELETE FROM audit_events",
		"transition update": "UPDATE job_transitions SET reason='changed'",
		"transition delete": "DELETE FROM job_transitions WHERE job_id='" + job.ID + "'",
	} {
		if _, err := store.db.ExecContext(ctx, statement); err == nil {
			t.Errorf("%s unexpectedly succeeded", name)
		}
	}
}

func TestConfigurationRevisionsAreDurableAndOrdered(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := 0; index < 2; index++ {
		document := appconfig.Default(".data")
		document.Workflow.MaxReviewCycles += index
		after, _ := json.Marshal(document)
		created, err := store.CreateConfigRevision(ctx, appconfig.Revision{
			ActorID: "administrator", SchemaVersion: appconfig.SchemaVersion,
			Before: json.RawMessage(`{}`), After: after, Diff: json.RawMessage(`[]`),
			ValidationResult: json.RawMessage(`{"valid":true}`),
			Reason:           "test revision",
		})
		if err != nil {
			t.Fatal(err)
		}
		if created.Sequence != int64(index+1) {
			t.Fatalf("unexpected sequence %d", created.Sequence)
		}
		time.Sleep(time.Millisecond)
	}
	current, err := store.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.Sequence != 2 {
		t.Fatalf("current revision is %d, want 2", current.Sequence)
	}
	items, err := store.ListConfigRevisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Sequence != 2 || items[1].Sequence != 1 {
		t.Fatalf("revisions are not newest-first: %#v", items)
	}
}

func TestMigrationFromVersionOneAddsEveryRetainedSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, migration001); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)", time.Now().UTC().Format(timestampFormat)); err != nil {
		t.Fatal(err)
	}
	document, _ := json.Marshal(appconfig.Default(".data"))
	var historicalDocument map[string]any
	if err := json.Unmarshal(document, &historicalDocument); err != nil {
		t.Fatal(err)
	}
	delete(historicalDocument, "notifications")
	document, _ = json.Marshal(historicalDocument)
	if _, err := db.ExecContext(ctx, `INSERT INTO config_revisions(
		id, actor_id, schema_version, before_document, after_document,
		document_diff, validation_result, rollback_of, created_at)
		VALUES('config_v1', 'system', 1, '{}', ?, '[]', '{"valid":true}', '', ?)`,
		string(document), time.Now().UTC().Format(timestampFormat)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	revision, err := store.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if revision.ID != "config_notifications_v15" || !strings.Contains(string(revision.After), `"local_inbox_enabled":true`) {
		t.Fatalf("unexpected migrated revision: %#v", revision)
	}
	var migrations int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 34 {
		t.Fatalf("applied migration count = %d, want 34", migrations)
	}
	var leaseTable string
	if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='job_leases'").Scan(&leaseTable); err != nil {
		t.Fatalf("job_leases table missing: %v", err)
	}
	var registryValue string
	if err := store.db.QueryRowContext(ctx, `SELECT value_json FROM config_scope_values
		WHERE setting_key = 'workflow.max_review_cycles' AND scope_kind = 'system' AND scope_id = ''`).Scan(&registryValue); err != nil {
		t.Fatalf("active Increment 1 configuration was not imported into the registry: %v", err)
	}
	if registryValue != "2" {
		t.Fatalf("migrated review-cycle value = %q, want 2", registryValue)
	}
	for _, table := range []string{"baseline_supersessions", "differential_corrections", "test_impact_overrides"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 20 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"capability_installations", "capability_lifecycle_events", "capability_assignments", "repo_doctor_scans", "repo_doctor_proposals"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 21 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"forge_profiles", "forge_sync_runs", "forge_objects"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 22 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"windows_worker_profiles", "windows_worker_runs"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 23 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"golden_rehearsal_reports", "golden_update_approvals"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 28 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"documentation_manifests"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 29 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"policy_bundles", "policy_activations", "policy_simulations", "policy_decisions"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 30 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"policy_test_runs"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 31 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"test_designer_dispositions"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 32 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"golden_comparison_profiles"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 33 table %s missing: %v", table, err)
		}
	}
	for _, table := range []string{"scheduler_decisions"} {
		var name string
		if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("migration 34 table %s missing: %v", table, err)
		}
	}
}

func TestMigration17PreservesVersion16DraftsAndAddsImportStorage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "version-16.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`WITH RECURSIVE versions(value) AS (SELECT 1 UNION ALL SELECT value + 1 FROM versions WHERE value < 16)
		 INSERT INTO schema_migrations SELECT value, '2026-07-20T00:00:00Z' FROM versions`,
		`CREATE TABLE config_drafts (
		 id TEXT PRIMARY KEY, scope_kind TEXT NOT NULL, scope_id TEXT NOT NULL,
		 state TEXT NOT NULL, base_scope_version INTEGER NOT NULL, version INTEGER NOT NULL,
		 author_id TEXT NOT NULL, reviewer_id TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL,
		 applied_revision_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		) STRICT`,
		`CREATE TABLE jobs(id TEXT PRIMARY KEY)`,
		migration013,
		`INSERT INTO config_drafts(id, scope_kind, scope_id, state, base_scope_version, version,
		 author_id, reason, created_at, updated_at) VALUES(
		 'configdraft_existing', 'system', '', 'draft', 1, 2, 'operator', 'retained',
		 '2026-07-20T00:00:00Z', '2026-07-20T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var operation string
	if err := store.db.QueryRowContext(ctx,
		`SELECT operation FROM config_drafts WHERE id = 'configdraft_existing'`).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	if operation != "apply" {
		t.Fatalf("retained draft operation = %q, want apply", operation)
	}
	var table string
	if err := store.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'config_import_unknown_entries'`).Scan(&table); err != nil {
		t.Fatalf("import preservation table missing: %v", err)
	}
}

func TestMigration24PreservesAppendOnlyGitHubDeliveryAndAllowsGitLabEvents(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE jobs(id TEXT PRIMARY KEY); INSERT INTO jobs(id) VALUES('job_fixture');`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, migration013); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO github_deliveries(delivery_id,event,action,outcome,repository,pr_number,job_id,head_sha,merged_commit,payload_sha256,affected_records,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`
	if _, err := db.ExecContext(ctx, insert, "github-existing", "pull_request", "closed", "merged", "owner/repo", 1, "job_fixture", strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64), 1, "2026-07-22T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, migration024); err != nil {
		t.Fatal(err)
	}
	var event string
	if err := db.QueryRowContext(ctx, `SELECT event FROM github_deliveries WHERE delivery_id='github-existing'`).Scan(&event); err != nil || event != "pull_request" {
		t.Fatalf("retained event %q error %v", event, err)
	}
	if _, err := db.ExecContext(ctx, insert, "gitlab-new", "merge_request", "closed", "rejected", "owner/repo", 2, "job_fixture", strings.Repeat("d", 40), "", strings.Repeat("e", 64), 0, "2026-07-22T00:01:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE github_deliveries SET outcome='merged' WHERE delivery_id='gitlab-new'`); err == nil {
		t.Fatal("migrated forge delivery was mutable")
	}
}

func TestMigration14PreservesHistoricalMemoryIndexQueueWithoutSequence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "historical.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`WITH RECURSIVE versions(value) AS (SELECT 1 UNION ALL SELECT value + 1 FROM versions WHERE value < 13)
		 INSERT INTO schema_migrations SELECT value, '2026-07-20T00:00:00Z' FROM versions`,
		`CREATE TABLE config_revisions(sequence INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
		 actor_id TEXT NOT NULL, schema_version INTEGER NOT NULL, before_document TEXT NOT NULL, after_document TEXT NOT NULL,
		 document_diff TEXT NOT NULL, validation_result TEXT NOT NULL, rollback_of TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`INSERT INTO config_revisions(id, actor_id, schema_version, before_document, after_document, document_diff,
		 validation_result, created_at) VALUES('config_fixture', 'system', 1, '{}', '{"schema_version":1}', '[]', '{}', '2026-07-20T00:00:00Z')`,
		`CREATE TABLE memory_records(id TEXT PRIMARY KEY)`,
		`CREATE TABLE jobs(id TEXT PRIMARY KEY, project_id TEXT NOT NULL)`,
		migration013,
		`CREATE TABLE job_transitions(sequence INTEGER PRIMARY KEY, job_id TEXT NOT NULL, to_state TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE schedule_runs(sequence INTEGER PRIMARY KEY, schedule_id TEXT NOT NULL, job_id TEXT, status TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE automation_requests(sequence INTEGER PRIMARY KEY, kind TEXT NOT NULL, job_id TEXT NOT NULL, requested_by TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE memory_index_operations (
		 id TEXT PRIMARY KEY, record_id TEXT NOT NULL REFERENCES memory_records(id), owner TEXT NOT NULL,
		 repository TEXT NOT NULL, action TEXT NOT NULL, record_version INTEGER NOT NULL, state TEXT NOT NULL,
		 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', next_attempt_at TEXT NOT NULL,
		 lease_owner TEXT NOT NULL DEFAULT '', lease_expires_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		 UNIQUE(record_id, record_version, action))`,
		`INSERT INTO memory_records(id) VALUES('memory_0123456789abcdef0123456789abcdef')`,
		`INSERT INTO memory_index_operations(id, record_id, owner, repository, action, record_version, state,
		 next_attempt_at, created_at, updated_at) VALUES('operation', 'memory_0123456789abcdef0123456789abcdef',
		 'owner', 'repo', 'upsert', 2, 'pending', '2026-07-20T00:00:00Z', '2026-07-20T00:00:00Z', '2026-07-20T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var sequence int64
	var id string
	if err := store.db.QueryRowContext(ctx, "SELECT sequence, id FROM memory_index_operations").Scan(&sequence, &id); err != nil {
		t.Fatal(err)
	}
	if sequence != 1 || id != "operation" {
		t.Fatalf("preserved operation = sequence %d id %q", sequence, id)
	}
}

func TestArtifactIndexIsImmutableJobScopedAndAudited(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_artifacts", ProjectID: "project-one", Repository: "owner/repo", Task: "verify", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	record := storage.ArtifactRecord{
		ID: "artifact_one", JobID: job.ID, ProjectID: job.ProjectID,
		ObjectSHA256: strings.Repeat("a", 64), Bytes: 12,
		RelativePath: "objects/aa/" + strings.Repeat("a", 64), Kind: "command_result",
		MediaType: "application/json", Producer: "verifier", Metadata: json.RawMessage(`{"class":"full_tests"}`),
	}
	created, err := store.IndexArtifact(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetArtifact(ctx, job.ID, created.ID)
	if err != nil || loaded.ObjectSHA256 != record.ObjectSHA256 || loaded.RelativePath != record.RelativePath {
		t.Fatalf("loaded artifact %#v, %v", loaded, err)
	}
	items, err := store.ListJobArtifacts(ctx, job.ID, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("listed artifacts %#v, %v", items, err)
	}
	if _, err := store.GetArtifact(ctx, "another-job", created.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("cross-job lookup returned %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE job_artifacts SET kind='tampered' WHERE id=?", created.ID); err == nil {
		t.Fatal("artifact association was mutable")
	}
	auditEvents, err := store.ListAudit(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if auditEvents[0].Action != "artifact.index" {
		t.Fatalf("artifact indexing was not audited: %#v", auditEvents)
	}
}

func TestJobLeasesAreExclusiveRenewableAndRecoverAfterExpiry(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	first, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_first", ProjectID: "p", Repository: "o/r", Task: "first", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	second, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_second", ProjectID: "p", Repository: "o/r", Task: "second", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}

	claimedFirst, firstLease, err := store.AcquireJobLease(ctx, "worker-one", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimedFirst.ID != first.ID || firstLease.OwnerID != "worker-one" {
		t.Fatalf("unexpected first lease: %#v %#v", claimedFirst, firstLease)
	}
	claimedSecond, _, err := store.AcquireJobLease(ctx, "worker-two", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimedSecond.ID != second.ID {
		t.Fatalf("second worker claimed %s, want %s", claimedSecond.ID, second.ID)
	}
	if _, _, err := store.AcquireJobLease(ctx, "worker-three", 30*time.Second); !errors.Is(err, storage.ErrNoLeaseAvailable) {
		t.Fatalf("expected no lease, got %v", err)
	}
	if _, err := store.RenewJobLease(ctx, first.ID, "wrong-worker", 30*time.Second); !errors.Is(err, storage.ErrLeaseLost) {
		t.Fatalf("wrong owner renewed lease: %v", err)
	}
	clock = firstLease.ExpiresAt.Add(time.Second)
	reclaimed, lease, err := store.AcquireJobLease(ctx, "worker-three", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.ID != first.ID || lease.OwnerID != "worker-three" {
		t.Fatalf("expired lease was not reclaimed: %#v %#v", reclaimed, lease)
	}
	if err := store.ReleaseJobLease(ctx, first.ID, "worker-one"); !errors.Is(err, storage.ErrLeaseLost) {
		t.Fatalf("old owner released reclaimed lease: %v", err)
	}
	if err := store.ReleaseJobLease(ctx, first.ID, "worker-three"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseJobLease(ctx, first.ID, "worker-three"); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}
}
