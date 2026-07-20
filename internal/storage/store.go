package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/jobs"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrInvalid        = errors.New("invalid")
	ErrIdempotencyKey = errors.New("idempotency key reused with different input")
)

type CreateJobParams struct {
	ID          string
	ProjectID   string
	Repository  string
	Task        string
	IssueNumber *int64
	ActorID     string
	Details     json.RawMessage
}

type JobStore interface {
	CreateJob(context.Context, CreateJobParams) (jobs.Job, error)
	GetJob(context.Context, string) (jobs.Job, error)
	ListJobs(context.Context, int, int) ([]jobs.Job, error)
	TransitionJob(context.Context, string, jobs.TransitionRequest) (jobs.Job, error)
	ListTransitions(context.Context, string, int64, int) ([]jobs.Transition, error)
}

type AuditStore interface {
	AppendAudit(context.Context, audit.AppendRequest) (audit.Event, error)
	ListAudit(context.Context, int64, int) ([]audit.Event, error)
}

type ConfigStore interface {
	CurrentConfig(context.Context) (config.Revision, error)
	CreateConfigRevision(context.Context, config.Revision) (config.Revision, error)
	ListConfigRevisions(context.Context, int) ([]config.Revision, error)
}

type Store interface {
	JobStore
	AuditStore
	ConfigStore
	Close() error
}
