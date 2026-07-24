package golden

import (
	"strings"
	"testing"
	"time"

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

func TestApprovalResolutionUsesLatestAppendOnlyDecision(t *testing.T) {
	report, err := ChangedReportForTest("job_golden", "project", strings.Repeat("a", 64), "medium", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	report.ID = "report-golden"
	comparisonID := report.Comparisons[0].ID
	rejected := Approval{ID: "approval-1", ReportID: report.ID, ComparisonID: comparisonID, Approved: false, CreatedAt: time.Unix(1, 0)}
	if resolution := ResolveApprovals(report, []Approval{rejected}); resolution.Pending != 1 || resolution.Rejected != 1 {
		t.Fatalf("rejected update should remain unresolved: %#v", resolution)
	}
	approved := Approval{ID: "approval-2", ReportID: report.ID, ComparisonID: comparisonID, Approved: true, CreatedAt: time.Unix(2, 0)}
	if resolution := ResolveApprovals(report, []Approval{rejected, approved}); resolution.Pending != 0 || resolution.Rejected != 0 {
		t.Fatalf("latest approved update should resolve: %#v", resolution)
	}
}
