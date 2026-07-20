package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/jobs"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalid          = errors.New("invalid")
	ErrIdempotencyKey   = errors.New("idempotency key reused with different input")
	ErrNoLeaseAvailable = errors.New("no resumable job lease is available")
	ErrLeaseLost        = errors.New("job lease is expired or owned by another worker")
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
	GetConfigRevision(context.Context, string) (config.Revision, error)
	CreateConfigRevision(context.Context, config.Revision) (config.Revision, error)
	ListConfigRevisions(context.Context, int) ([]config.Revision, error)
}

type JobLease struct {
	JobID       string    `json:"job_id"`
	OwnerID     string    `json:"owner_id"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type LeaseStore interface {
	AcquireJobLease(context.Context, string, time.Duration) (jobs.Job, JobLease, error)
	RenewJobLease(context.Context, string, string, time.Duration) (JobLease, error)
	ReleaseJobLease(context.Context, string, string) error
}

type ArtifactRecord struct {
	ID           string          `json:"id"`
	JobID        string          `json:"job_id"`
	ProjectID    string          `json:"project_id"`
	ObjectSHA256 string          `json:"sha256"`
	Bytes        int64           `json:"bytes"`
	RelativePath string          `json:"-"`
	Kind         string          `json:"kind"`
	MediaType    string          `json:"media_type"`
	Producer     string          `json:"producer"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

type ArtifactStore interface {
	IndexArtifact(context.Context, ArtifactRecord) (ArtifactRecord, error)
	GetArtifact(context.Context, string, string) (ArtifactRecord, error)
	ListJobArtifacts(context.Context, string, int) ([]ArtifactRecord, error)
}

type Store interface {
	JobStore
	AuditStore
	ConfigStore
	LeaseStore
	ArtifactStore
	Close() error
}
