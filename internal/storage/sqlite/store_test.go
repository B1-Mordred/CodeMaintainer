package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
