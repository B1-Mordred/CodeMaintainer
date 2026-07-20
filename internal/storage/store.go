package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/local-code-maintainer/appliance/internal/agents"
	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/auth"
	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/projects"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalid          = errors.New("invalid")
	ErrIdempotencyKey   = errors.New("idempotency key reused with different input")
	ErrNoLeaseAvailable = errors.New("no resumable job lease is available")
	ErrLeaseLost        = errors.New("job lease is expired or owned by another worker")
	ErrBudgetExceeded   = errors.New("job budget exceeded")
)

type CreateJobParams struct {
	ID             string
	ProjectID      string
	Repository     string
	Task           string
	IssueNumber    *int64
	ActorID        string
	Details        json.RawMessage
	MaxWallSeconds int
	MaxTokens      int
}

type JobStore interface {
	CreateJob(context.Context, CreateJobParams) (jobs.Job, error)
	GetJob(context.Context, string) (jobs.Job, error)
	ListJobs(context.Context, int, int) ([]jobs.Job, error)
	TransitionJob(context.Context, string, jobs.TransitionRequest) (jobs.Job, error)
	ListTransitions(context.Context, string, int64, int) ([]jobs.Transition, error)
}

type JobMetadataPatch struct {
	BaseSHA                *string
	ResultSHA              *string
	AcceptanceCriteria     *json.RawMessage
	AcceptanceCriteriaHash *string
	ReviewCycle            *int
}

type PhaseCompletion struct {
	JobID           string
	PhaseState      jobs.State
	ExpectedVersion int64
	To              jobs.State
	ActorID         string
	Reason          string
	Details         json.RawMessage
	Outcome         json.RawMessage
	Metadata        JobMetadataPatch
}

type PhaseRecord struct {
	JobID        string          `json:"job_id"`
	PhaseState   jobs.State      `json:"phase_state"`
	PhaseVersion int64           `json:"phase_version"`
	Outcome      json.RawMessage `json:"outcome"`
	CreatedAt    time.Time       `json:"created_at"`
}

type WorkflowStore interface {
	JobStore
	ReserveJobTokens(context.Context, string, int64, jobs.State, int) (jobs.Job, error)
	PutCandidate(context.Context, memory.ProjectScope, memory.Record) (memory.Record, error)
	GetMemory(context.Context, memory.ProjectScope, string) (memory.Record, error)
	CompletePhase(context.Context, PhaseCompletion) (jobs.Job, error)
	ListPhaseRecords(context.Context, string, int) ([]PhaseRecord, error)
}

type ProjectStore interface {
	UpsertProject(context.Context, projects.UpsertRequest, string) (projects.Project, error)
	DisableProject(context.Context, string, string) (projects.Project, error)
	GetProject(context.Context, string) (projects.Project, error)
	ListProjects(context.Context, int) ([]projects.Project, error)
}

type Approval struct {
	ID              string    `json:"id"`
	JobID           string    `json:"job_id"`
	Kind            string    `json:"kind"`
	SubjectSHA      string    `json:"subject_sha"`
	ActorID         string    `json:"actor_id"`
	ActorRole       string    `json:"actor_role"`
	Rationale       string    `json:"rationale"`
	Reauthenticated bool      `json:"reauthenticated"`
	CreatedAt       time.Time `json:"created_at"`
}

type PublicationApprovalRequest struct {
	ActorID         string
	ActorRole       string
	Rationale       string
	Reauthenticated bool
	ExpectedVersion int64
}

type ApprovalStore interface {
	ApprovePublication(context.Context, string, PublicationApprovalRequest) (jobs.Job, Approval, error)
	ListApprovals(context.Context, string) ([]Approval, error)
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
	GetConfigScope(context.Context, config.ScopeRef) (config.ScopeState, error)
	ApplyConfigScope(context.Context, config.ApplyScopeRequest) (config.RegistryRevision, config.ScopeState, error)
	GetConfigRegistryRevision(context.Context, string) (config.RegistryRevision, error)
	ListConfigRegistryRevisions(context.Context, config.ScopeRef, int) ([]config.RegistryRevision, error)
	GetConfigScopeAtRevision(context.Context, string) (config.ScopeState, error)
	CreateConfigDraft(context.Context, config.CreateDraftRequest) (config.Draft, error)
	GetConfigDraft(context.Context, string) (config.Draft, error)
	ListConfigDrafts(context.Context, config.ScopeRef, int) ([]config.Draft, error)
	UpdateConfigDraft(context.Context, config.UpdateDraftRequest) (config.Draft, error)
	TransitionConfigDraft(context.Context, config.TransitionDraftRequest) (config.Draft, error)
	SaveConfigCheck(context.Context, config.CheckResult) (config.CheckResult, error)
	ListConfigChecks(context.Context, string, int) ([]config.CheckResult, error)
	SaveJobConfigSnapshot(context.Context, config.JobSnapshot) (config.JobSnapshot, error)
	GetJobConfigSnapshot(context.Context, string) (config.JobSnapshot, error)
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
	ID             string          `json:"id"`
	JobID          string          `json:"job_id"`
	ProjectID      string          `json:"project_id"`
	ObjectSHA256   string          `json:"sha256"`
	Bytes          int64           `json:"bytes"`
	RelativePath   string          `json:"-"`
	Kind           string          `json:"kind"`
	MediaType      string          `json:"media_type"`
	Producer       string          `json:"producer"`
	IdempotencyKey string          `json:"-"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
}

type ArtifactStore interface {
	IndexArtifact(context.Context, ArtifactRecord) (ArtifactRecord, error)
	GetArtifact(context.Context, string, string) (ArtifactRecord, error)
	ListJobArtifacts(context.Context, string, int) ([]ArtifactRecord, error)
}

type FindingStore interface {
	ObserveFindings(context.Context, string, int, []agents.Finding) ([]findings.Record, error)
	TransitionFinding(context.Context, string, string, findings.TransitionRequest) (findings.Record, error)
	ListFindings(context.Context, string) ([]findings.Record, error)
}

type Store interface {
	automation.Store
	auth.Store
	memory.DurableStore
	memory.IndexQueue
	WorkflowStore
	AuditStore
	ConfigStore
	LeaseStore
	ArtifactStore
	FindingStore
	ProjectStore
	ApprovalStore
	GitHubEventStore
	intelligence.Store
	Close() error
}

type GitHubDeliveryResult struct {
	DeliveryID     string `json:"delivery_id"`
	Outcome        string `json:"outcome"`
	AffectedMemory int    `json:"affected_memory"`
	Replay         bool   `json:"replay"`
}

type GitHubEventStore interface {
	ApplyGitHubPullRequestEvent(context.Context, gitbridge.PullRequestEvent) (GitHubDeliveryResult, error)
}
