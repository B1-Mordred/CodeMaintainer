package githubsync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

type fixtureStore struct {
	job      jobs.Job
	project  projects.Project
	artifact storage.ArtifactRecord
	events   []gitbridge.PullRequestEvent
}

func (f *fixtureStore) ListJobs(context.Context, int, int) ([]jobs.Job, error) {
	return []jobs.Job{f.job}, nil
}
func (f *fixtureStore) GetProject(context.Context, string) (projects.Project, error) {
	return f.project, nil
}
func (f *fixtureStore) ListJobArtifacts(context.Context, string, int) ([]storage.ArtifactRecord, error) {
	return []storage.ArtifactRecord{f.artifact}, nil
}
func (f *fixtureStore) ApplyGitHubPullRequestEvent(_ context.Context, event gitbridge.PullRequestEvent) (storage.GitHubDeliveryResult, error) {
	f.events = append(f.events, event)
	return storage.GitHubDeliveryResult{DeliveryID: event.DeliveryID, Outcome: event.Outcome}, nil
}

type fixtureArtifacts struct{ payload []byte }

func (f fixtureArtifacts) Open(context.Context, string, string) (storage.ArtifactRecord, io.ReadCloser, error) {
	return storage.ArtifactRecord{}, io.NopCloser(bytes.NewReader(f.payload)), nil
}

type fixtureBridge struct {
	registration gitbridge.Registration
	event        gitbridge.PullRequestEvent
}

func (f *fixtureBridge) Register(_ context.Context, registration gitbridge.Registration) error {
	f.registration = registration
	return nil
}
func (f *fixtureBridge) PullRequestEvent(context.Context, string, int) (gitbridge.PullRequestEvent, error) {
	return f.event, nil
}

func TestPollingFallbackAppliesOnlyClosedGitHubPullRequests(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	publication, _ := json.Marshal(gitbridge.Publication{Provider: "github", Number: 23, ResultSHA: sha, Draft: true})
	store := &fixtureStore{
		job:      jobs.Job{ID: "job_poll", ProjectID: "p", Repository: "owner/repo", ResultSHA: sha, State: jobs.StateCompleted},
		project:  projects.Project{ID: "p", Provider: "github", Repository: "owner/repo", DefaultBranch: "main", Enabled: true},
		artifact: storage.ArtifactRecord{ID: "artifact_poll", Kind: "publication"},
	}
	bridge := &fixtureBridge{event: gitbridge.PullRequestEvent{Action: "open", Outcome: "pending"}}
	reconciler, err := New(store, fixtureArtifacts{publication}, bridge, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := reconciler.Step(context.Background()); err != nil || applied != 0 || len(store.events) != 0 {
		t.Fatalf("open poll applied=%d events=%d err=%v", applied, len(store.events), err)
	}
	bridge.event = gitbridge.PullRequestEvent{DeliveryID: "poll-closed", Action: "closed", Outcome: "merged", HeadSHA: sha}
	if applied, err := reconciler.Step(context.Background()); err != nil || applied != 1 || len(store.events) != 1 {
		t.Fatalf("closed poll applied=%d events=%d err=%v", applied, len(store.events), err)
	}
	if bridge.registration.Repository != "owner/repo" {
		t.Fatalf("registration = %#v", bridge.registration)
	}
}
