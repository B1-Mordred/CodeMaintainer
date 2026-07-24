package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	"github.com/B1-Mordred/CodeMaintainer/internal/findings"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestFindingsRemainStableAndWaiversRequireRecentReviewerAuthorization(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project", Repository: "owner/repo", Task: "fix", ActorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	finding := agents.Finding{
		ID: "QC-TEST-001", Severity: "must_fix", Category: "correctness", Claim: "result is wrong",
		Location: agents.Location{Path: "answer.go", Line: 3}, Evidence: "TestAnswer fails", RequiredResolution: "fix the result",
		VerificationMethod: "run full_tests",
	}
	records, err := store.ObserveFindings(ctx, job.ID, 0, []agents.Finding{finding})
	if err != nil || len(records) != 1 || records[0].Status != findings.StatusOpen {
		t.Fatalf("observed records %#v, %v", records, err)
	}
	request := findings.TransitionRequest{
		To: findings.StatusHumanWaived, ActorID: "operator", ActorRole: "reviewer", Rationale: "accepted risk",
		ExpectedVersion: records[0].Version,
	}
	if _, err := store.TransitionFinding(ctx, job.ID, finding.ID, request); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("waiver without reauthentication returned %v", err)
	}
	request.Reauthenticated = true
	waived, err := store.TransitionFinding(ctx, job.ID, finding.ID, request)
	if err != nil || waived.Status != findings.StatusHumanWaived {
		t.Fatalf("authorized waiver returned %#v, %v", waived, err)
	}
	closed, err := store.TransitionFinding(ctx, job.ID, finding.ID, findings.TransitionRequest{
		To: findings.StatusClosed, ActorID: "workflow", ActorRole: "system", Rationale: "waiver gate complete", ExpectedVersion: waived.Version,
	})
	if err != nil || closed.Status != findings.StatusClosed {
		t.Fatalf("waived close returned %#v, %v", closed, err)
	}
}

func TestLaterReviewCannotChurnStableFindingOrInventOrdinaryBlocker(t *testing.T) {
	ctx := context.Background()
	store, _ := Open(ctx, ":memory:")
	defer store.Close()
	job, _ := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project", Repository: "owner/repo", Task: "fix", ActorID: "operator"})
	base := agents.Finding{
		ID: "QC-STABLE-001", Severity: "must_fix", Category: "correctness", Claim: "wrong",
		Location: agents.Location{Path: "answer.go", Line: 3}, Evidence: "test fails", RequiredResolution: "fix", VerificationMethod: "full_tests",
	}
	if _, err := store.ObserveFindings(ctx, job.ID, 0, []agents.Finding{base}); err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Category = "security"
	if _, err := store.ObserveFindings(ctx, job.ID, 1, []agents.Finding{changed}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stable finding meaning change returned %v", err)
	}
	newFinding := base
	newFinding.ID = "QC-CHURN-002"
	if _, err := store.ObserveFindings(ctx, job.ID, 1, []agents.Finding{newFinding}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("ordinary post-repair blocker returned %v", err)
	}
	newFinding.Category = "newly_observed_evidence"
	if _, err := store.ObserveFindings(ctx, job.ID, 1, []agents.Finding{newFinding}); err != nil {
		t.Fatalf("allowed new-evidence finding returned %v", err)
	}
}

func TestDocumentationPolicyFindingsCanAppearAfterRepairReview(t *testing.T) {
	ctx := context.Background()
	store, _ := Open(ctx, ":memory:")
	defer store.Close()
	job, _ := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project", Repository: "owner/repo", Task: "fix docs", ActorID: "operator"})
	for _, finding := range []agents.Finding{
		{
			ID: "QC-DOCREQ-001", Severity: "must_fix", Category: "documentation_policy",
			Claim: "required docs are stale", Location: agents.Location{Path: "docs/api.md"},
			Evidence: "documentation policy requires an API update", RequiredResolution: "update docs/api.md",
			VerificationMethod: "documentation_policy_check",
		},
		{
			ID: "QC-DOCCLAIM-001", Severity: "blocker", Category: "documentation_unsupported_claim",
			Claim: "unsupported documentation claim", Location: agents.Location{Path: "docs/api.md"},
			Evidence: "claim is not linked to source evidence", RequiredResolution: "remove or cite the claim",
			VerificationMethod: "documentation_claim_qc",
		},
	} {
		if _, err := store.ObserveFindings(ctx, job.ID, 1, []agents.Finding{finding}); err != nil {
			t.Fatalf("documentation finding after repair returned %v", err)
		}
	}
}
