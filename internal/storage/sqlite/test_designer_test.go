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
	time.Sleep(time.Millisecond)
	skipped, err := testdesigner.NewSkipped(job.ID, strings.Repeat("a", 64), "low", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTestDesignerReport(ctx, skipped); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != "skipped" || items[1].Proposals[0].Disposition != "pending" {
		t.Fatalf("unexpected Test Designer reports: %#v", items)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE test_designer_reports SET status='skipped'"); err == nil {
		t.Fatal("test designer reports accepted update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM test_designer_reports"); err == nil {
		t.Fatal("test designer reports accepted delete")
	}
}
