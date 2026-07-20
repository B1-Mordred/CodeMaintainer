package runnerd

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/local-code-maintainer/appliance/internal/runners"
)

var ErrRunNotFound = errors.New("runner execution not found")

type Executor interface {
	Start(context.Context, WorkerSpec) error
	Inspect(context.Context, runners.RunID) (runners.Status, error)
	Logs(context.Context, runners.RunID, int64, int) (runners.LogChunk, error)
	Stop(context.Context, runners.RunID) error
	Artifacts(context.Context, runners.RunID) ([]runners.Artifact, error)
}

// FakeExecutor is used only by explicit mock profiles and policy/API tests. It
// records the fully resolved server-side specification so tests can prove that
// untrusted request fields never influence worker containment.
type FakeExecutor struct {
	mu      sync.Mutex
	runs    map[runners.RunID]runners.Status
	specs   map[runners.RunID]WorkerSpec
	logs    map[runners.RunID]string
	outputs map[runners.RunID][]runners.Artifact
}

func NewFakeExecutor() *FakeExecutor {
	return &FakeExecutor{
		runs: make(map[runners.RunID]runners.Status), specs: make(map[runners.RunID]WorkerSpec),
		logs: make(map[runners.RunID]string), outputs: make(map[runners.RunID][]runners.Artifact),
	}
}

func (f *FakeExecutor) Start(_ context.Context, spec WorkerSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, exists := f.specs[spec.RunID]; exists {
		if !sameWorkerIdentity(existing, spec) {
			return errors.New("run identifier is already bound to a different worker request")
		}
		return nil
	}
	f.specs[spec.RunID] = spec
	f.runs[spec.RunID] = runners.Status{
		ID: spec.RunID, JobID: spec.JobID, Kind: spec.Kind, State: "running", StartedAt: time.Now().UTC(),
	}
	f.logs[spec.RunID] = "deterministic runnerd fake executor\n"
	f.outputs[spec.RunID] = []runners.Artifact{}
	return nil
}

func sameWorkerIdentity(left, right WorkerSpec) bool {
	return left.RunID == right.RunID && left.JobID == right.JobID && left.ProjectID == right.ProjectID &&
		left.Kind == right.Kind && left.Image == right.Image && left.User == right.User
}

func (f *FakeExecutor) Inspect(_ context.Context, runID runners.RunID) (runners.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, exists := f.runs[runID]
	if !exists {
		return runners.Status{}, ErrRunNotFound
	}
	return status, nil
}

func (f *FakeExecutor) Logs(_ context.Context, runID runners.RunID, cursor int64, limit int) (runners.LogChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, exists := f.logs[runID]
	if !exists {
		return runners.LogChunk{}, ErrRunNotFound
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= int64(len(data)) {
		return runners.LogChunk{NextCursor: int64(len(data))}, nil
	}
	if limit <= 0 || limit > 64<<10 {
		limit = 64 << 10
	}
	end := int(cursor) + limit
	if end > len(data) {
		end = len(data)
	}
	return runners.LogChunk{NextCursor: int64(end), Data: data[cursor:end], Truncated: end < len(data)}, nil
}

func (f *FakeExecutor) Stop(_ context.Context, runID runners.RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, exists := f.runs[runID]
	if !exists {
		return ErrRunNotFound
	}
	status.State = "stopped"
	f.runs[runID] = status
	return nil
}

func (f *FakeExecutor) Artifacts(_ context.Context, runID runners.RunID) ([]runners.Artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	artifacts, exists := f.outputs[runID]
	if !exists {
		return nil, ErrRunNotFound
	}
	return append([]runners.Artifact(nil), artifacts...), nil
}

func (f *FakeExecutor) Spec(runID runners.RunID) (WorkerSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, exists := f.specs[runID]
	return spec, exists
}
