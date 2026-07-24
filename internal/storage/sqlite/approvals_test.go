package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestPublicationApprovalRequiresReviewerReauthenticationAndBindsExactSHA(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_approval", ProjectID: "project", Repository: "owner/repo", Task: "task", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := []jobs.State{
		jobs.StateSyncing, jobs.StateCreatingWorktree, jobs.StatePreparingDependencies,
		jobs.StateLockingAcceptanceCriteria, jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel, jobs.StateReproducing,
		jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
		jobs.StateLoadingQCModel, jobs.StateQCReview, jobs.StateAwaitingOperator,
	}
	for _, state := range path {
		job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
			To: state, ActorID: "engine", Reason: "seed", ExpectedVersion: job.Version,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	resultSHA := "0123456789abcdef0123456789abcdef01234567"
	if _, err := store.db.ExecContext(ctx, "UPDATE jobs SET result_sha=? WHERE id=?", resultSHA, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApprovePublication(ctx, job.ID, storage.PublicationApprovalRequest{
		ActorID: "reviewer", ActorRole: "reviewer", Rationale: "reviewed", ExpectedVersion: job.Version,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("approval without reauthentication returned %v", err)
	}
	approved, approval, err := store.ApprovePublication(ctx, job.ID, storage.PublicationApprovalRequest{
		ActorID: "reviewer", ActorRole: "reviewer", Rationale: "reviewed exact report",
		Reauthenticated: true, ExpectedVersion: job.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != jobs.StatePublishingBranch || approval.SubjectSHA != resultSHA || !approval.Reauthenticated {
		t.Fatalf("approval = %#v, job = %#v", approval, approved)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE approvals SET rationale='tampered' WHERE id=?", approval.ID); err == nil {
		t.Fatal("approval was mutable")
	}
}

func TestRemoteEgressApprovalRequiresExactManifestAndResumesWorkerPhase(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := providers.NewService(store).EnsureDefaults(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_remote_egress", ProjectID: "project", Repository: "owner/repo", Task: "network auth task", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := []jobs.State{
		jobs.StateSyncing, jobs.StateCreatingWorktree, jobs.StatePreparingDependencies,
		jobs.StateLockingAcceptanceCriteria, jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel, jobs.StateReproducing,
		jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
		jobs.StateLoadingDocumentationModel, jobs.StateDocumentationReview, jobs.StateAwaitingRemoteEgressApproval,
	}
	for _, state := range path {
		job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
			To: state, ActorID: "engine", Reason: "seed", ExpectedVersion: job.Version,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest := providers.EgressManifest{
		JobID: job.ID, ProjectID: job.ProjectID, RouteID: "remote-documentation-ci-preview",
		ProviderID: "fake-openai-responses", EndpointID: "fake-openai-responses-endpoint",
		ModelProfileID: "fake-remote-json", Purpose: "documentation worker task packet",
		DataClasses: []string{"documentation_public_source", "context_packet"}, Redactions: []string{"secret_scan"},
		EstimatedBytes: 1024, EstimatedTokens: 256, Retention: "operator_reviewed",
		PolicyDecision: providers.DecisionAllowed, DecisionReason: "test retained manifest",
	}
	manifest.ManifestSHA256, err = providers.HashManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = store.RecordEgressManifest(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApproveRemoteEgress(ctx, job.ID, storage.RemoteEgressApprovalRequest{
		ActorID: "reviewer", ActorRole: "reviewer", Rationale: "reviewed manifest", ExpectedVersion: job.Version,
		SubjectSHA: manifest.ManifestSHA256, ResumeState: jobs.StateDocumentationReview,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("approval without reauthentication returned %v", err)
	}
	if _, _, err := store.ApproveRemoteEgress(ctx, job.ID, storage.RemoteEgressApprovalRequest{
		ActorID: "reviewer", ActorRole: "reviewer", Rationale: "reviewed manifest", Reauthenticated: true,
		ExpectedVersion: job.Version, SubjectSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ResumeState: jobs.StateDocumentationReview,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("approval for unknown manifest returned %v", err)
	}
	approved, approval, err := store.ApproveRemoteEgress(ctx, job.ID, storage.RemoteEgressApprovalRequest{
		ActorID: "reviewer", ActorRole: "reviewer", Rationale: "reviewed exact remote egress manifest",
		Reauthenticated: true, ExpectedVersion: job.Version, SubjectSHA: manifest.ManifestSHA256,
		ResumeState: jobs.StateDocumentationReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != jobs.StateDocumentationReview || approval.Kind != storage.ApprovalKindRemoteEgress ||
		approval.SubjectSHA != manifest.ManifestSHA256 || !approval.Reauthenticated {
		t.Fatalf("remote egress approval = %#v, job = %#v", approval, approved)
	}
}
