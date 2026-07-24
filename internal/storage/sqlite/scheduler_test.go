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
