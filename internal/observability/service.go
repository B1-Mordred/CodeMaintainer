package observability

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

func (s *Service) Status(ctx context.Context) (Status, error) {
	events, err := s.store.ListObservabilityEvents(ctx, "", 20)
	if err != nil {
		return Status{}, err
	}
	bundles, err := s.store.ListSupportBundles(ctx, 10)
	if err != nil {
		return Status{}, err
	}
	return Status{
		SchemaVersion:         SchemaVersion,
		LocalCollector:        "controller-sqlite",
		RetentionDays:         30,
		SamplingRatio:         1,
		ExternalOTLPEnabled:   false,
		ExternalOTLPAllowlist: []string{},
		RedactionPolicy:       RedactionPolicy(),
		RecentEvents:          events,
		RecentSupportBundles:  bundles,
	}, nil
}

func (s *Service) RecordEvent(ctx context.Context, request RecordEventRequest, actor string) (Event, error) {
	event, err := NewEvent(request, actor, s.now())
	if err != nil {
		return Event{}, err
	}
	return s.store.RecordObservabilityEvent(ctx, event)
}

func (s *Service) CreateSupportBundle(ctx context.Context, request CreateSupportBundleRequest, actor string) (SupportBundle, error) {
	events, err := s.store.ListObservabilityEvents(ctx, "", 100)
	if err != nil {
		return SupportBundle{}, err
	}
	bundle, err := NewSupportBundle(events, request, actor, s.now())
	if err != nil {
		return SupportBundle{}, err
	}
	return s.store.RecordSupportBundle(ctx, bundle)
}
