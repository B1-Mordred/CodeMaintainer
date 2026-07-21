package githubsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

const maxPublicationArtifact = int64(1 << 20)

type Store interface {
	ListJobs(context.Context, int, int) ([]jobs.Job, error)
	GetProject(context.Context, string) (projects.Project, error)
	ListJobArtifacts(context.Context, string, int) ([]storage.ArtifactRecord, error)
	ApplyGitHubPullRequestEvent(context.Context, gitbridge.PullRequestEvent) (storage.GitHubDeliveryResult, error)
}

type ArtifactReader interface {
	Open(context.Context, string, string) (storage.ArtifactRecord, io.ReadCloser, error)
}

type Bridge interface {
	Register(context.Context, gitbridge.Registration) error
	PullRequestEvent(context.Context, string, int) (gitbridge.PullRequestEvent, error)
}

type Reconciler struct {
	store     Store
	artifacts ArtifactReader
	bridge    Bridge
	interval  time.Duration
	logger    *slog.Logger
}

func New(store Store, artifacts ArtifactReader, bridge Bridge, interval time.Duration, logger *slog.Logger) (*Reconciler, error) {
	if store == nil || artifacts == nil || bridge == nil || interval < time.Second || logger == nil {
		return nil, storage.ErrInvalid
	}
	return &Reconciler{store: store, artifacts: artifacts, bridge: bridge, interval: interval, logger: logger}, nil
}

func (r *Reconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if _, err := r.Step(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.WarnContext(ctx, "GitHub polling reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Reconciler) Step(ctx context.Context) (int, error) {
	jobList, err := r.store.ListJobs(ctx, 500, 0)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, job := range jobList {
		if job.State != jobs.StateCompleted && job.State != jobs.StateDraftPRCreated {
			continue
		}
		project, err := r.store.GetProject(ctx, job.ProjectID)
		if err != nil {
			return applied, err
		}
		if !project.Enabled || project.Provider != "github" || project.Repository != job.Repository {
			continue
		}
		publication, found, err := r.publication(ctx, job)
		if err != nil {
			return applied, err
		}
		if !found {
			continue
		}
		if err := r.bridge.Register(ctx, gitbridge.Registration{
			ProjectID: project.ID, Provider: project.Provider, Repository: project.Repository,
			DefaultBranch: project.DefaultBranch,
		}); err != nil {
			return applied, err
		}
		event, err := r.bridge.PullRequestEvent(ctx, project.ID, publication.Number)
		if err != nil {
			return applied, err
		}
		if event.Action != "closed" || (event.Outcome != "merged" && event.Outcome != "rejected") {
			continue
		}
		if _, err := r.store.ApplyGitHubPullRequestEvent(ctx, event); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func (r *Reconciler) publication(ctx context.Context, job jobs.Job) (gitbridge.Publication, bool, error) {
	items, err := r.store.ListJobArtifacts(ctx, job.ID, 100)
	if err != nil {
		return gitbridge.Publication{}, false, err
	}
	for _, item := range items {
		if item.Kind != "publication" {
			continue
		}
		_, reader, err := r.artifacts.Open(ctx, job.ID, item.ID)
		if err != nil {
			return gitbridge.Publication{}, false, err
		}
		payload, readErr := io.ReadAll(io.LimitReader(reader, maxPublicationArtifact+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(payload)) > maxPublicationArtifact {
			return gitbridge.Publication{}, false, storage.ErrInvalid
		}
		var publication gitbridge.Publication
		if json.Unmarshal(payload, &publication) != nil || publication.Provider != "github" || publication.Number <= 0 ||
			publication.ResultSHA != job.ResultSHA || !publication.Draft {
			return gitbridge.Publication{}, false, storage.ErrInvalid
		}
		return publication, true, nil
	}
	return gitbridge.Publication{}, false, nil
}
