package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/golden"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestGoldenReportsAndApprovalsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job_golden", ProjectID: "project", Repository: "owner/repo", Task: "golden update", ActorID: "operator"}); err != nil {
		t.Fatal(err)
	}
	report, err := golden.ChangedReportForTest("job_golden", "project", strings.Repeat("a", 64), "medium", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveGoldenReport(ctx, report)
	if err != nil {
		t.Fatal(err)
	}
	found, err := store.GetGoldenReport(ctx, saved.ID)
	if err != nil || found.ID != saved.ID || found.JobID != "job_golden" {
		t.Fatalf("get report = %#v, %v", found, err)
	}
	listed, err := store.ListGoldenReports(ctx, "job_golden", 10)
	if err != nil || len(listed) != 1 || listed[0].ID != saved.ID || listed[0].Status != "approval_required" {
		t.Fatalf("reports = %#v, %v", listed, err)
	}
	if _, err := store.ApproveGoldenUpdate(ctx, golden.ApprovalRequest{
		ReportID: saved.ID, ComparisonID: saved.Comparisons[0].ID, ActorID: "reviewer",
		ActorRole: "administrator", Reason: "approve reviewed fixture update", Approved: true,
		Reauthenticated: false,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("approval without reauth err = %v", err)
	}
	approval, err := store.ApproveGoldenUpdate(ctx, golden.ApprovalRequest{
		ReportID: saved.ID, ComparisonID: saved.Comparisons[0].ID, ActorID: "reviewer",
		ActorRole: "administrator", Reason: "approve reviewed fixture update", Approved: true,
		Reauthenticated: true,
	})
	if err != nil || approval.CandidateArtifactSHA256 != saved.Comparisons[0].CandidateArtifactSHA256 {
		t.Fatalf("approval = %#v, %v", approval, err)
	}
	if resolution := golden.ResolveApprovals(saved, []golden.Approval{approval}); resolution.Pending != 0 || resolution.Rejected != 0 {
		t.Fatalf("approved golden did not resolve pending update: %#v", resolution)
	}
	approvals, err := store.ListGoldenApprovals(ctx, "job_golden", 10)
	if err != nil || len(approvals) != 1 || approvals[0].ID != approval.ID {
		t.Fatalf("approvals = %#v, %v", approvals, err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE golden_rehearsal_reports SET status='passed'"); err == nil {
		t.Fatal("golden reports are mutable")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM golden_update_approvals"); err == nil {
		t.Fatal("golden approvals are deletable")
	}
}
