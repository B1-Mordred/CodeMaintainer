package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestJobBudgetsAreDurableIdempotentAndAppendOnly(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	clock := time.Date(2026, 7, 20, 21, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	job, err := store.CreateJob(context.Background(), storage.CreateJobParams{
		ProjectID: "project", Repository: "owner/repo", Task: "bounded task", ActorID: "operator",
		MaxWallSeconds: 600, MaxTokens: 20_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.MaxWallSeconds != 600 || !job.DeadlineAt.Equal(clock.Add(10*time.Minute)) || job.MaxTokens != 20_000 || job.ReservedTokens != 0 {
		t.Fatalf("job budget = %+v", job)
	}
	job, err = store.ReserveJobTokens(context.Background(), job.ID, job.Version, job.State, 16_384)
	if err != nil || job.ReservedTokens != 16_384 {
		t.Fatalf("first reservation = %+v, %v", job, err)
	}
	job, err = store.ReserveJobTokens(context.Background(), job.ID, job.Version, job.State, 16_384)
	if err != nil || job.ReservedTokens != 16_384 {
		t.Fatalf("idempotent reservation = %+v, %v", job, err)
	}
	job, err = store.TransitionJob(context.Background(), job.ID, jobs.TransitionRequest{To: jobs.StateSyncing, ActorID: "test", Reason: "next phase", ExpectedVersion: job.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveJobTokens(context.Background(), job.ID, job.Version, job.State, 16_384); !errors.Is(err, storage.ErrBudgetExceeded) {
		t.Fatalf("over-budget reservation returned %v", err)
	}
	if _, err := store.db.ExecContext(context.Background(), "DELETE FROM job_token_reservations WHERE job_id = ?", job.ID); err == nil {
		t.Fatal("token reservation history was deleted")
	}
}
