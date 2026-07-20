package memory

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type SyncStore interface {
	GetMemory(context.Context, ProjectScope, string) (Record, error)
	IndexQueue
}

type Synchronizer struct {
	store  SyncStore
	index  Index
	owner  string
	poll   time.Duration
	logger *slog.Logger
}

func NewSynchronizer(store SyncStore, index Index, owner string, poll time.Duration, logger *slog.Logger) (*Synchronizer, error) {
	if store == nil || index == nil || owner == "" || poll < 10*time.Millisecond || poll > time.Minute {
		return nil, ErrInvalid
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Synchronizer{store: store, index: index, owner: owner, poll: poll, logger: logger}, nil
}

func (s *Synchronizer) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		for {
			err := s.ProcessOne(ctx)
			if errors.Is(err, ErrNoIndexOperation) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				s.logger.ErrorContext(ctx, "memory index synchronization failed", "error", err)
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Synchronizer) ProcessOne(ctx context.Context) error {
	operation, err := s.store.ClaimMemoryIndexOperation(ctx, s.owner, 30*time.Second)
	if err != nil {
		return err
	}
	operationErr := s.apply(ctx, operation)
	if operationErr == nil {
		return s.store.CompleteMemoryIndexOperation(ctx, operation.ID, s.owner)
	}
	retry := time.Duration(1<<min(operation.Attempts, 6)) * time.Second
	if err := s.store.FailMemoryIndexOperation(ctx, operation.ID, s.owner, operationErr.Error(), retry); err != nil {
		return errors.Join(operationErr, err)
	}
	return operationErr
}

func (s *Synchronizer) apply(ctx context.Context, operation IndexOperation) error {
	switch operation.Action {
	case "upsert":
		record, err := s.store.GetMemory(ctx, operation.Scope, operation.RecordID)
		if err != nil {
			return err
		}
		if record.Version != operation.RecordVersion || record.Status != StatusCanonical {
			return nil
		}
		return s.index.Upsert(ctx, record)
	case "forget":
		return s.index.Forget(ctx, operation.Scope, operation.RecordID)
	default:
		return ErrInvalid
	}
}
