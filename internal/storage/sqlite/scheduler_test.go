package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/scheduler"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestSchedulerDecisionsAreAppendOnlyAndListedNewestFirst(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 24, 3, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	decision, err := scheduler.Simulate(scheduler.SimulationRequest{
		Mode: scheduler.ModeQualityLatency, Topology: scheduler.DefaultTopology(),
		Profiles: scheduler.DefaultProfiles(), MaintenanceWindow: true,
		Queued: []scheduler.QueueItem{{
			JobID: "job_scheduler", ProjectID: "project_scheduler", State: "queued", Priority: 100,
			ProfileID: "verification_offline", CreatedAt: now.Add(-time.Minute),
		}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.RecordSchedulerDecision(ctx, decision)
	if err != nil || saved.ID == "" || saved.CreatedAt != now {
		t.Fatalf("saved decision = %#v, %v", saved, err)
	}
	items, err := store.ListSchedulerDecisions(ctx, 10)
	if err != nil || len(items) != 1 || items[0].SelectedJobID != "job_scheduler" {
		t.Fatalf("listed decisions = %#v, %v", items, err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE scheduler_decisions SET status='rejected' WHERE id=?", saved.ID); err == nil {
		t.Fatal("scheduler decision update unexpectedly succeeded")
	}
	if _, err := store.RecordSchedulerDecision(ctx, scheduler.Decision{Mode: "bad"}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("invalid decision error = %v", err)
	}
}

func TestSchedulerDefersSameProjectLeaseAndRecordsDecision(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 24, 3, 30, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	first, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_active_project", ProjectID: "project_one", Repository: "owner/repo", Task: "first", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AcquireJobLease(ctx, "worker-one", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_same_project", ProjectID: first.ProjectID, Repository: "owner/repo", Task: "same project", ActorID: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AcquireJobLease(ctx, "worker-two", 30*time.Second); !errors.Is(err, storage.ErrNoLeaseAvailable) {
		t.Fatalf("same-project scheduler lease returned %v", err)
	}
	decisions, err := store.ListSchedulerDecisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var deferred *scheduler.Decision
	for index := range decisions {
		if len(decisions[index].DeferredJobIDs) == 1 && decisions[index].DeferredJobIDs[0] == "job_same_project" {
			deferred = &decisions[index]
			break
		}
	}
	if deferred == nil || deferred.Status != scheduler.DecisionDeferred || !deferred.FairnessApplied {
		t.Fatalf("same-project decision not retained: %#v", decisions)
	}
}

func TestSchedulerDefersLeaseThatWouldExceedMemory(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 24, 3, 45, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_impl_active", ProjectID: "project_impl", Repository: "owner/impl", Task: "impl", ActorID: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_qc_active", ProjectID: "project_qc", Repository: "owner/qc", Task: "qc", ActorID: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_docs_candidate", ProjectID: "project_docs", Repository: "owner/docs", Task: "docs", ActorID: "test"}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id    string
		state string
		owner string
	}{
		{"job_impl_active", "implementing", "worker-impl"},
		{"job_qc_active", "qc_review", "worker-qc"},
		{"job_docs_candidate", "documentation_review", ""},
	} {
		if _, err := store.db.ExecContext(ctx, "UPDATE jobs SET state = ?, updated_at = ? WHERE id = ?", item.state, formatTime(now), item.id); err != nil {
			t.Fatal(err)
		}
		if item.owner != "" {
			if _, err := store.db.ExecContext(ctx, `INSERT INTO job_leases(job_id, owner_id, acquired_at, heartbeat_at, expires_at)
				VALUES(?, ?, ?, ?, ?)`, item.id, item.owner, formatTime(now), formatTime(now), formatTime(now.Add(30*time.Second))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, _, err := store.AcquireJobLease(ctx, "worker-docs", 30*time.Second); !errors.Is(err, storage.ErrNoLeaseAvailable) {
		t.Fatalf("memory-pressure scheduler lease returned %v", err)
	}
	decisions, err := store.ListSchedulerDecisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Status != scheduler.DecisionDeferred ||
		len(decisions[0].DeferredJobIDs) != 1 || decisions[0].DeferredJobIDs[0] != "job_docs_candidate" {
		t.Fatalf("memory-pressure decision not retained: %#v", decisions)
	}
}
