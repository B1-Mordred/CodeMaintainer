package runners

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Kind is a server-recognized execution profile. It is intentionally not an
// image name or command string.
type Kind string

const (
	KindDependencies   Kind = "dependencies"
	KindImplementation Kind = "implementation"
	KindVerification   Kind = "verification"
	KindQC             Kind = "qc"
)

type JobRequest struct {
	JobID            string   `json:"job_id"`
	ProjectID        string   `json:"project_id"`
	Kind             Kind     `json:"kind"`
	InputArtifactIDs []string `json:"input_artifact_ids"`
}

type RunID string

type Status struct {
	ID        RunID     `json:"id"`
	JobID     string    `json:"job_id"`
	Kind      Kind      `json:"kind"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
}

type LogChunk struct {
	NextCursor int64  `json:"next_cursor"`
	Data       string `json:"data"`
	Truncated  bool   `json:"truncated"`
}

type Artifact struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Runner interface {
	Start(context.Context, JobRequest) (RunID, error)
	Inspect(context.Context, RunID) (Status, error)
	Logs(context.Context, RunID, int64, int) (LogChunk, error)
	Stop(context.Context, RunID) error
	Artifacts(context.Context, RunID) ([]Artifact, error)
}

var ErrInvalidRequest = errors.New("invalid runner request")

// Fake is a deterministic in-process executor for tests and explicit mock
// deployments. Production always uses runnerd over its authenticated socket.
type Fake struct {
	mu      sync.Mutex
	counter int64
	runs    map[RunID]Status
}

func NewFake() *Fake { return &Fake{runs: make(map[RunID]Status)} }

func (f *Fake) Start(_ context.Context, request JobRequest) (RunID, error) {
	if request.JobID == "" || request.ProjectID == "" || !request.Kind.Valid() {
		return "", ErrInvalidRequest
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	id := RunID(request.JobID + "-fake-run-" + time.Unix(f.counter, 0).UTC().Format("150405"))
	f.runs[id] = Status{ID: id, JobID: request.JobID, Kind: request.Kind, State: "running", StartedAt: time.Now().UTC()}
	return id, nil
}

func (f *Fake) Inspect(_ context.Context, id RunID) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, ok := f.runs[id]
	if !ok {
		return Status{}, errors.New("run not found")
	}
	return status, nil
}

func (f *Fake) Logs(_ context.Context, id RunID, cursor int64, limit int) (LogChunk, error) {
	if _, err := f.Inspect(context.Background(), id); err != nil {
		return LogChunk{}, err
	}
	if limit <= 0 || limit > 64<<10 {
		limit = 64 << 10
	}
	value := "deterministic fake runner\n"
	if cursor >= int64(len(value)) {
		return LogChunk{NextCursor: int64(len(value))}, nil
	}
	end := int(cursor) + limit
	if end > len(value) {
		end = len(value)
	}
	return LogChunk{NextCursor: int64(end), Data: value[cursor:end], Truncated: end < len(value)}, nil
}

func (f *Fake) Stop(_ context.Context, id RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, ok := f.runs[id]
	if !ok {
		return errors.New("run not found")
	}
	status.State = "stopped"
	f.runs[id] = status
	return nil
}

func (f *Fake) Artifacts(context.Context, RunID) ([]Artifact, error) { return []Artifact{}, nil }

func (k Kind) Valid() bool {
	return k == KindDependencies || k == KindImplementation || k == KindVerification || k == KindQC
}
