package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/memory"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestAuthenticatedGitHubMergePromotesExactJobCandidateOnce(t *testing.T) {
	ctx := context.Background()
	store, job, record := seedForgeEventFixture(t, ctx, "github")
	defer store.Close()
	event := gitbridge.PullRequestEvent{
		DeliveryID: "delivery-merge", Repository: job.Repository, Number: 23, Action: "closed", Outcome: "merged",
		Branch: "maintainer/" + job.ID, BaseBranch: "main", HeadSHA: job.ResultSHA,
		MergedCommit: "abcdef0123456789abcdef0123456789abcdef01", PayloadSHA256: strings.Repeat("a", 64),
	}
	result, err := store.ApplyGitHubPullRequestEvent(ctx, event)
	if err != nil || result.AffectedMemory != 1 || result.Replay {
		t.Fatalf("merge result = %#v, %v", result, err)
	}
	promoted, err := store.GetMemory(ctx, record.Scope, record.ID)
	if err != nil || promoted.Status != memory.StatusCanonical || promoted.MergedCommit != event.MergedCommit {
		t.Fatalf("promoted memory = %#v, %v", promoted, err)
	}
	replay, err := store.ApplyGitHubPullRequestEvent(ctx, event)
	if err != nil || !replay.Replay || replay.AffectedMemory != 1 {
		t.Fatalf("replayed merge = %#v, %v", replay, err)
	}
}

func TestAuthenticatedGitHubRejectionStalesCandidateAndExactSHAIsRequired(t *testing.T) {
	ctx := context.Background()
	store, job, record := seedForgeEventFixture(t, ctx, "github")
	defer store.Close()
	event := gitbridge.PullRequestEvent{
		DeliveryID: "delivery-reject", Repository: job.Repository, Number: 23, Action: "closed", Outcome: "rejected",
		Branch: "maintainer/" + job.ID, BaseBranch: "main", HeadSHA: job.ResultSHA,
		PayloadSHA256: strings.Repeat("b", 64),
	}
	event.Number = 24
	if _, err := store.ApplyGitHubPullRequestEvent(ctx, event); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("mismatched publication number returned %v", err)
	}
	event.Number = 23
	event.HeadSHA = strings.Repeat("f", 40)
	if _, err := store.ApplyGitHubPullRequestEvent(ctx, event); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("mismatched result SHA returned %v", err)
	}
	event.HeadSHA = job.ResultSHA
	result, err := store.ApplyGitHubPullRequestEvent(ctx, event)
	if err != nil || result.AffectedMemory != 1 {
		t.Fatalf("rejection result = %#v, %v", result, err)
	}
	stale, err := store.GetMemory(ctx, record.Scope, record.ID)
	if err != nil || stale.Status != memory.StatusStale {
		t.Fatalf("stale memory = %#v, %v", stale, err)
	}
}

func TestAuthenticatedGitLabMergePromotesExactJobCandidateOnce(t *testing.T) {
	ctx := context.Background()
	store, job, record := seedForgeEventFixture(t, ctx, "gitlab")
	defer store.Close()
	event := gitbridge.PullRequestEvent{
		Provider: "gitlab", DeliveryID: "gitlab-delivery-merge", Repository: job.Repository, Number: 23, Action: "closed", Outcome: "merged",
		Branch: "maintainer/" + job.ID, BaseBranch: "main", HeadSHA: job.ResultSHA,
		MergedCommit: "abcdef0123456789abcdef0123456789abcdef01", PayloadSHA256: strings.Repeat("e", 64),
	}
	result, err := store.ApplyGitHubPullRequestEvent(ctx, event)
	if err != nil || result.AffectedMemory != 1 || result.Replay {
		t.Fatalf("merge result = %#v, %v", result, err)
	}
	promoted, err := store.GetMemory(ctx, record.Scope, record.ID)
	if err != nil || promoted.Status != memory.StatusCanonical || promoted.MergedCommit != event.MergedCommit {
		t.Fatalf("promoted memory = %#v, %v", promoted, err)
	}
	replay, err := store.ApplyGitHubPullRequestEvent(ctx, event)
	if err != nil || !replay.Replay {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
}

func seedForgeEventFixture(t *testing.T, ctx context.Context, provider string) (*Store, jobs.Job, memory.Record) {
	t.Helper()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{
		ID: provider + "-fixture", Provider: provider, Repository: "owner/repo", DefaultBranch: "main",
	}, "administrator"); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_" + provider, ProjectID: provider + "-fixture", Repository: "owner/repo", Task: "repair", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	const resultSHA = "0123456789abcdef0123456789abcdef01234567"
	if _, err := store.db.ExecContext(ctx, "UPDATE jobs SET result_sha = ? WHERE id = ?", resultSHA, job.ID); err != nil {
		t.Fatal(err)
	}
	for _, state := range []jobs.State{
		jobs.StateSyncing, jobs.StateCreatingWorktree, jobs.StatePreparingDependencies, jobs.StateLockingAcceptanceCriteria,
		jobs.StateLoadingImplementationModel, jobs.StateReproducing, jobs.StateImplementing, jobs.StateVerifyingTargeted,
		jobs.StateVerifyingFull, jobs.StateLoadingQCModel, jobs.StateQCReview, jobs.StateAwaitingOperator,
		jobs.StatePublishingBranch, jobs.StateDraftPRCreated, jobs.StateCompleted,
	} {
		job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
			To: state, ActorID: "fixture", Reason: "seed authenticated webhook state", ExpectedVersion: job.Version,
		})
		if err != nil {
			t.Fatalf("transition to %s: %v", state, err)
		}
	}
	job.ResultSHA = resultSHA
	artifactID := "artifact_0123456789abcdef0123456789abcdef"
	digest := strings.Repeat("c", 64)
	if _, err := store.IndexArtifact(ctx, storage.ArtifactRecord{
		ID: artifactID, JobID: job.ID, ProjectID: job.ProjectID, ObjectSHA256: digest, Bytes: 2,
		RelativePath: "objects/cc/" + digest, Kind: "final_report", MediaType: "application/json",
		Producer: "workflow-controller", IdempotencyKey: "final", Metadata: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	publicationDigest := strings.Repeat("d", 64)
	if _, err := store.IndexArtifact(ctx, storage.ArtifactRecord{
		ID: "artifact_abcdef0123456789abcdef0123456789", JobID: job.ID, ProjectID: job.ProjectID,
		ObjectSHA256: publicationDigest, Bytes: 2, RelativePath: "objects/dd/" + publicationDigest,
		Kind: "publication", MediaType: "application/json", Producer: "workflow-controller", IdempotencyKey: "publication",
		Metadata: []byte(`{"provider":"` + provider + `","number":23,"branch":"maintainer/job_` + provider + `","result_sha":"0123456789abcdef0123456789abcdef01234567"}`),
	}); err != nil {
		t.Fatal(err)
	}
	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	record, err := store.PutCandidate(ctx, scope, memory.Record{
		ID: "memory_0123456789abcdef0123456789abcdef", Content: "verified exact maintenance case",
		Kind: "verified_case", SourceURI: "artifact://" + artifactID + "/final-report", BaseCommit: resultSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, job, record
}
