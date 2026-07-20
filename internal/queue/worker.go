package queue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

type Processor interface {
	Process(context.Context, jobs.Job) error
}

type ProcessorFunc func(context.Context, jobs.Job) error

func (f ProcessorFunc) Process(ctx context.Context, job jobs.Job) error { return f(ctx, job) }

// Worker claims one resumable job at a time through a durable expiring lease.
// A lost heartbeat cancels the processor, preventing two controllers from
// continuing the same phase after ownership changes.
type Worker struct {
	store        storage.LeaseStore
	processor    Processor
	ownerID      string
	leaseTTL     time.Duration
	pollInterval time.Duration
	logger       *slog.Logger
}

func NewWorker(store storage.LeaseStore, processor Processor, ownerID string, leaseTTL, pollInterval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store: store, processor: processor, ownerID: ownerID,
		leaseTTL: leaseTTL, pollInterval: pollInterval, logger: logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.store == nil || w.processor == nil || w.ownerID == "" || w.leaseTTL < time.Second || w.pollInterval <= 0 {
		return storage.ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		job, lease, err := w.store.AcquireJobLease(ctx, w.ownerID, w.leaseTTL)
		if errors.Is(err, storage.ErrNoLeaseAvailable) {
			if !wait(ctx, w.pollInterval) {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := w.processLeased(ctx, job, lease); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.ErrorContext(ctx, "leased job processing failed", "job_id", job.ID, "error", err)
			if !wait(ctx, w.pollInterval) {
				return nil
			}
		}
	}
}

func (w *Worker) processLeased(parent context.Context, job jobs.Job, lease storage.JobLease) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	heartbeatErrors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		interval := w.leaseTTL / 3
		if interval < time.Second {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.store.RenewJobLease(ctx, lease.JobID, lease.OwnerID, w.leaseTTL); err != nil {
					heartbeatErrors <- err
					cancel()
					return
				}
			}
		}
	}()
	processErr := w.processor.Process(ctx, job)
	cancel()
	<-done
	releaseErr := w.store.ReleaseJobLease(context.WithoutCancel(parent), lease.JobID, lease.OwnerID)
	select {
	case heartbeatErr := <-heartbeatErrors:
		return heartbeatErr
	default:
	}
	if processErr != nil {
		return processErr
	}
	return releaseErr
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
