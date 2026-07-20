package automation

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Dispatcher interface {
	DispatchDueSchedule(context.Context, string) (ScheduleRun, error)
}

type Scheduler struct {
	store  Dispatcher
	poll   time.Duration
	logger *slog.Logger
}

func NewScheduler(store Dispatcher, poll time.Duration, logger *slog.Logger) (*Scheduler, error) {
	if store == nil || poll < 100*time.Millisecond || poll > time.Minute {
		return nil, ErrInvalid
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{store: store, poll: poll, logger: logger}, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		for {
			run, err := s.store.DispatchDueSchedule(ctx, "controller-scheduler")
			if errors.Is(err, ErrNoDueSchedule) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				s.logger.ErrorContext(ctx, "schedule dispatch failed", "error", err)
				break
			}
			s.logger.InfoContext(ctx, "schedule dispatched", "schedule_id", run.ScheduleID, "run_id", run.ID, "job_id", run.JobID, "status", run.Status)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
