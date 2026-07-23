package testdesigner

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeReportRequiresExactContractRiskAndResultBinding(t *testing.T) {
	report := Report{
		JobID: "job_one", SchemaVersion: 1, ContractSHA256: strings.Repeat("a", 64),
		RiskLevel: "medium", ResultSHA: strings.Repeat("b", 40), SourceContext: "independent_test_designer_context_v1",
		Status: "proposed", DispositionsRequired: true,
		Proposals: []Proposal{{
			ID: "TD-1", Category: "boundary_case", Claim: "negative values need a regression test",
			Rationale: "candidate diff changes arithmetic behavior", EvidenceIDs: []string{"baseline-1"},
			SuggestedTests: []string{"TestAddNegative"}, Disposition: "pending",
		}},
	}
	payload, _ := json.Marshal(report)
	if _, err := DecodeReport(payload, report.JobID, report.ContractSHA256, report.RiskLevel, report.ResultSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReport(payload, report.JobID, report.ContractSHA256, "high", report.ResultSHA); err == nil {
		t.Fatal("risk-mismatched report was accepted")
	}
	var body map[string]any
	_ = json.Unmarshal(payload, &body)
	body["hidden_reasoning"] = "not allowed"
	payload, _ = json.Marshal(body)
	if _, err := DecodeReport(payload, report.JobID, report.ContractSHA256, report.RiskLevel, report.ResultSHA); err == nil {
		t.Fatal("unknown report field was accepted")
	}
}

func TestSkippedReportIsOnlyValidForLowRisk(t *testing.T) {
	if _, err := NewSkipped("job_one", strings.Repeat("a", 64), "low", strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSkipped("job_one", strings.Repeat("a", 64), "medium", strings.Repeat("b", 40)); err == nil {
		t.Fatal("medium-risk Test Designer was skipped")
	}
}
