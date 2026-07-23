package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/risk"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
)

func TestTaskContractApprovalGatesImplementation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_contract", ProjectID: "project", Repository: "owner/repo", Task: "repair protected API", ActorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []jobs.State{jobs.StateSyncing, jobs.StateCreatingWorktree, jobs.StatePreparingDependencies, jobs.StateLockingAcceptanceCriteria} {
		job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{To: state, ActorID: "engine", Reason: "seed", ExpectedVersion: job.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	draftRequest, err := taskcontract.DraftFromTask(job.ID, job.Task, nil)
	if err != nil {
		t.Fatal(err)
	}
	draftRequest.ActorID = "clarifier"
	draftRequest.ActorRole = "system"
	draftRequest.Reason = "fixture draft"
	contract, err := store.EnsureTaskContract(ctx, draftRequest)
	if err != nil || contract.Status != "draft" || contract.ContractSHA256 == "" {
		t.Fatalf("draft = %#v, %v", contract, err)
	}
	job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{To: jobs.StateAwaitingTaskApproval, ActorID: "engine", Reason: "await contract approval", ExpectedVersion: job.Version})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.ApproveTaskContract(ctx, taskcontract.ApprovalRequest{
		JobID: job.ID, ExpectedVersion: contract.Version, ActorID: "operator", ActorRole: "operator", Reason: "approved exact scope",
	})
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approval = %#v, %v", approved, err)
	}
	job, err = store.GetJob(ctx, job.ID)
	if err != nil || job.State != jobs.StateLoadingImplementationModel || job.AcceptanceCriteriaHash == "" {
		t.Fatalf("approved job = %#v, %v", job, err)
	}
}

func TestRiskWaiverRequiresReauthenticationExpiryAndLowerLevel(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_risk", ProjectID: "project", Repository: "owner/repo", Task: "change authentication", ActorID: "operator"}); err != nil {
		t.Fatal(err)
	}
	contract, err := taskcontract.Normalize(taskcontract.UpsertRequest{
		JobID: "job_risk", SourceKind: "free_form", RequestedBehavior: "change authentication and database migration",
		AcceptanceCriteria:  []taskcontract.Criterion{{ID: "AC-1", Statement: "works", VerificationMethod: "full"}},
		CompletionChecklist: []taskcontract.Criterion{{ID: "DONE-1", Statement: "done", VerificationMethod: "audit"}},
		Questions:           []taskcontract.Question{{ID: "Q-1", Question: "scope?"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Assess(contract, []string{"migrations/001.sql"}, "")
	if err != nil {
		t.Fatal(err)
	}
	assessment, err = store.SaveRiskAssessment(ctx, assessment)
	if err != nil || assessment.Level != risk.LevelHigh {
		t.Fatalf("assessment = %#v, %v", assessment, err)
	}
	_, _, err = store.CreateRiskWaiver(ctx, risk.WaiverRequest{
		JobID: "job_risk", AssessmentID: assessment.ID, ToLevel: risk.LevelMedium, Reason: "fixture", ActorID: "reviewer", ActorRole: "reviewer",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("unauthenticated waiver error = %v", err)
	}
	waiver, lowered, err := store.CreateRiskWaiver(ctx, risk.WaiverRequest{
		JobID: "job_risk", AssessmentID: assessment.ID, ToLevel: risk.LevelMedium, Reason: "documented test fixture lowering", ActorID: "reviewer", ActorRole: "reviewer",
		Reauthenticated: true, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil || waiver.ID == "" || lowered.Level != risk.LevelMedium {
		t.Fatalf("waiver = %#v lowered=%#v err=%v", waiver, lowered, err)
	}
}
