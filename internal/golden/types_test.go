package golden

import (
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

func TestNewReportUsesRegisteredCapabilityRehearsalsAndTestDesignerProvenance(t *testing.T) {
	catalog, err := capabilities.BuiltInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(
		"job_golden", "project", strings.Repeat("a", 64), "medium", strings.Repeat("b", 40),
		[]capabilities.Assignment{{ProjectID: "project", PackID: "r-statistical-validation", PackVersion: "1.0.0", Enabled: true}},
		catalog,
		[]testdesigner.Report{{Proposals: []testdesigner.Proposal{{ID: "td-1", GoldenRehearsals: []string{"r-reference-results"}}}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || len(report.Comparisons) != 1 {
		t.Fatalf("report = %#v", report)
	}
	comparison := report.Comparisons[0]
	if comparison.RehearsalID != "r-reference-results" || comparison.Source.Source != "capability_pack_and_test_designer" || comparison.Source.ProposalID != "td-1" {
		t.Fatalf("comparison provenance = %#v", comparison)
	}
}

func TestChangedGoldenRequiresApprovalStatus(t *testing.T) {
	report, err := ChangedReportForTest("job_golden", "project", strings.Repeat("a", 64), "high", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "approval_required" || !report.Comparisons[0].ApprovalRequired {
		t.Fatalf("changed report = %#v", report)
	}
	report.Status = "passed"
	if err := report.Validate(); err == nil {
		t.Fatal("changed golden validated without approval_required status")
	}
}
