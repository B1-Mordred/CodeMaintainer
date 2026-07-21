package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

func TestWorkerReleasesLeaseWhenProcessorCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_worker", ProjectID: "p", Repository: "o/r", Task: "task", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	processed := make(chan jobs.Job, 1)
	processor := ProcessorFunc(func(_ context.Context, value jobs.Job) error {
		processed <- value
		cancel()
		return nil
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := NewWorker(store, processor, "worker-one", 3*time.Second, time.Millisecond, logger)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case got := <-processed:
		if got.ID != job.ID {
			t.Fatalf("processed job %s, want %s", got.ID, job.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not process queued job")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	claimContext := context.Background()
	reclaimed, lease, err := store.AcquireJobLease(claimContext, "worker-two", 3*time.Second)
	if err != nil {
		t.Fatalf("completed worker retained lease: %v", err)
	}
	if reclaimed.ID != job.ID || lease.OwnerID != "worker-two" {
		t.Fatalf("unexpected reclaimed lease: %#v %#v", reclaimed, lease)
	}
}

func TestWorkerRejectsInvalidLeaseConfiguration(t *testing.T) {
	worker := NewWorker(nil, nil, "", 0, 0, nil)
	if err := worker.Run(context.Background()); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("invalid worker returned %v", err)
	}
}
