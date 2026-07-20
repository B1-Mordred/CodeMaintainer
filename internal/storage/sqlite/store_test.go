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

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
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
	if revision.ID != "config_v1" || revision.Reason != "" {
		t.Fatalf("unexpected migrated revision: %#v", revision)
	}
	var migrations int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 10 {
		t.Fatalf("applied migration count = %d, want 10", migrations)
	}
	var leaseTable string
	if err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='job_leases'").Scan(&leaseTable); err != nil {
		t.Fatalf("job_leases table missing: %v", err)
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
