package evaluation

import (
	"context"
	"time"
)

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) CreateDataset(ctx context.Context, request CreateDatasetRequest, actor string) (Dataset, error) {
	dataset := NewDataset(request, actor, s.now())
	return s.store.CreateEvaluationDataset(ctx, dataset)
}

func (s *Service) LaunchRun(ctx context.Context, request LaunchRunRequest, actor string) (Run, error) {
	dataset, err := s.store.GetEvaluationDataset(ctx, request.DatasetID)
	if err != nil {
		return Run{}, err
	}
	run, err := SimulateRun(dataset, request, actor, "", s.now())
	if err != nil {
		return Run{}, err
	}
	return s.store.RecordEvaluationRun(ctx, run)
}
