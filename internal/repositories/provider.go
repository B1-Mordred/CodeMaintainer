package repositories

import (
	"context"
	"errors"
	"sync"
)

type Registration struct {
	ProjectID     string `json:"project_id"`
	Repository    string `json:"repository"`
	DefaultBranch string `json:"default_branch"`
}

type SyncResult struct {
	ProjectID string `json:"project_id"`
	BaseSHA   string `json:"base_sha"`
}

type Worktree struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
	Branch    string `json:"branch"`
	HandleID  string `json:"handle_id"`
	Path      string `json:"-"`
}

type Provider interface {
	Register(context.Context, Registration) error
	Sync(context.Context, string) (SyncResult, error)
	CreateWorktree(context.Context, string, string, string) (Worktree, error)
}

type Fake struct {
	mu       sync.Mutex
	projects map[string]Registration
	jobs     map[string]Worktree
}

func NewFake() *Fake {
	return &Fake{projects: make(map[string]Registration), jobs: make(map[string]Worktree)}
}

func (f *Fake) Register(_ context.Context, registration Registration) error {
	if registration.ProjectID == "" || registration.Repository == "" || registration.DefaultBranch == "" {
		return errors.New("invalid repository registration")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[registration.ProjectID] = registration
	return nil
}

func (f *Fake) Sync(_ context.Context, projectID string) (SyncResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok {
		return SyncResult{}, errors.New("repository is not registered")
	}
	return SyncResult{ProjectID: projectID, BaseSHA: "0000000000000000000000000000000000000001"}, nil
}

func (f *Fake) CreateWorktree(_ context.Context, projectID, jobID, baseSHA string) (Worktree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok || jobID == "" || baseSHA == "" {
		return Worktree{}, errors.New("invalid worktree request")
	}
	if _, exists := f.jobs[jobID]; exists {
		return Worktree{}, errors.New("worktree already exists for job")
	}
	worktree := Worktree{ProjectID: projectID, JobID: jobID, BaseSHA: baseSHA, Branch: "maintainer/" + jobID, HandleID: "worktree-" + jobID}
	f.jobs[jobID] = worktree
	return worktree, nil
}
