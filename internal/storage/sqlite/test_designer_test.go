package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

func TestTestDesignerReportsAreAppendOnlyAndBounded(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "project-one", Repository: "owner/repo", Task: "repair auth path", ActorID: "operator-one"})
	if err != nil {
		t.Fatal(err)
	}
	report := testdesigner.Report{
		JobID: job.ID, SchemaVersion: 1, ContractSHA256: strings.Repeat("a", 64),
		RiskLevel: "medium", ResultSHA: strings.Repeat("b", 40), SourceContext: "independent_test_designer_context_v1",
		Status: "proposed", DispositionsRequired: true,
		Proposals: []testdesigner.Proposal{{
			ID: "TD-1", Category: "boundary_case", Claim: "missing negative regression",
			Rationale: "candidate diff changes a boundary", EvidenceIDs: []string{"diff-1"},
			SuggestedTests: []string{"TestNegative"}, GoldenRehearsals: []string{}, Disposition: "pending",
		}},
	}
	if _, err := store.SaveTestDesignerReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	reportID := report.ID
	items, err := store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("initial reports = %#v, %v", items, err)
	}
	reportID = items[0].ID
	disposition, err := store.SaveTestDesignerDisposition(ctx, testdesigner.Disposition{
		ReportID: reportID, ProposalID: "TD-1", Disposition: "accepted",
		Reason: "implemented the required negative regression", ActorID: "reviewer", ActorRole: "reviewer",
	})
	if err != nil {
		t.Fatalf("save disposition: %v", err)
	}
	if disposition.JobID != job.ID || disposition.ID == "" {
		t.Fatalf("saved disposition = %#v", disposition)
	}
	items, err = store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil || items[0].Proposals[0].Disposition != "accepted" || items[0].Proposals[0].DispositionReason == "" {
		t.Fatalf("effective disposition not applied: %#v, %v", items, err)
	}
	dispositions, err := store.ListTestDesignerDispositions(ctx, job.ID, "", 10)
	if err != nil || len(dispositions) != 1 || dispositions[0].ProposalID != "TD-1" {
		t.Fatalf("dispositions = %#v, %v", dispositions, err)
	}
	if _, err := store.SaveTestDesignerDisposition(ctx, testdesigner.Disposition{
		ReportID: reportID, ProposalID: "TD-1", Disposition: "rejected",
		Reason: "later reviewer correction with evidence", ActorID: "reviewer", ActorRole: "reviewer",
	}); err != nil {
		t.Fatalf("save correcting disposition: %v", err)
	}
	items, err = store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil || items[0].Proposals[0].Disposition != "rejected" {
		t.Fatalf("latest disposition not effective: %#v, %v", items, err)
	}
	time.Sleep(time.Millisecond)
	skipped, err := testdesigner.NewSkipped(job.ID, strings.Repeat("a", 64), "low", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTestDesignerReport(ctx, skipped); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != "skipped" || items[1].Proposals[0].Disposition != "rejected" {
		t.Fatalf("unexpected Test Designer reports: %#v", items)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE test_designer_reports SET status='skipped'"); err == nil {
		t.Fatal("test designer reports accepted update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM test_designer_reports"); err == nil {
		t.Fatal("test designer reports accepted delete")
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE test_designer_dispositions SET disposition='accepted'"); err == nil {
		t.Fatal("test designer dispositions accepted update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM test_designer_dispositions"); err == nil {
		t.Fatal("test designer dispositions accepted delete")
	}
}
