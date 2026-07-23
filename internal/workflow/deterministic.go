package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
	"github.com/B1-Mordred/CodeMaintainer/internal/verification"
)

// DeterministicBackend is used only by the explicit mock profile. It applies
// the seeded fixture repair without a model or compiler so CI and first-run can
// exercise the complete durable workflow without weights or a worker daemon.
type DeterministicBackend struct {
	worktreesRoot string
}

func NewDeterministicBackend(worktreesRoot string) (*DeterministicBackend, error) {
	if !filepath.IsAbs(worktreesRoot) {
		return nil, errors.New("mock worktree root must be absolute")
	}
	return &DeterministicBackend{worktreesRoot: filepath.Clean(worktreesRoot)}, nil
}

func (*DeterministicBackend) PrepareDependencies(context.Context, jobs.Job) error { return nil }

func (b *DeterministicBackend) Implement(_ context.Context, job jobs.Job, packet agents.TaskPacket) (agents.ImplementationResult, error) {
	result := agents.ImplementationResult{
		SchemaVersion: 1,
		Summary: agents.ActionSummary{
			Reproduction: "The seeded mock regression was reproduced by the locked targeted gate.",
			Plan:         "Apply the smallest fixture-only correction.", Changes: []string{"Applied one bounded fixture edit."},
			RegressionTests: []string{"Retained or added the negative-input regression assertion."},
			Checks:          []string{"The controller will run all exact-commit gates."}, Evidence: []string{"Deterministic mock adapter."},
		},
	}
	for _, file := range packet.RelevantFiles {
		updated := file.Content
		if packet.Mode == "implementation" {
			updated = strings.Replace(updated, "return a - b", "return a + b", 1)
		} else if packet.Mode == "repair" && strings.HasSuffix(file.Path, "_test.go") &&
			!strings.Contains(updated, "TestAddNegative") && strings.Contains(updated, `import "testing"`) {
			updated += "\nfunc TestAddNegative(t *testing.T) {\n\tif Add(-2, -3) != -5 {\n\t\tt.Fatal(Add(-2, -3))\n\t}\n}\n"
		}
		if updated != file.Content {
			result.Edits = []agents.Edit{{Path: file.Path, Content: updated, ExpectedSHA256: agents.HashContent([]byte(file.Content))}}
			break
		}
	}
	if len(result.Edits) != 1 {
		return agents.ImplementationResult{}, errors.New("mock repository does not contain the expected seeded fixture target")
	}
	if err := agents.ApplyEdits(filepath.Join(b.worktreesRoot, job.ID), result.Edits); err != nil {
		return agents.ImplementationResult{}, err
	}
	return result, nil
}

func (b *DeterministicBackend) Verify(_ context.Context, job jobs.Job, _ verification.Language,
	classes []verification.Class, purpose string) (VerificationResult, error) {
	answer, err := os.ReadFile(filepath.Join(b.worktreesRoot, job.ID, "answer.go"))
	if err != nil {
		return VerificationResult{}, err
	}
	passed := strings.Contains(string(answer), "return a + b")
	if purpose == "reproduction" {
		passed = !strings.Contains(string(answer), "return a - b")
	} else if purpose == "final" {
		tests, readErr := os.ReadFile(filepath.Join(b.worktreesRoot, job.ID, "answer_test.go"))
		if readErr != nil {
			return VerificationResult{}, readErr
		}
		passed = passed && strings.Contains(string(tests), "TestAddNegative")
	}
	head := job.ResultSHA
	if head == "" {
		head = job.BaseSHA
	}
	results := make([]verification.ExecutionResult, len(classes))
	for index, class := range classes {
		results[index] = verification.ExecutionResult{Class: class}
		if !passed {
			results[index].ExitCode = 1
		}
	}
	return VerificationResult{
		SchemaVersion: 1, Passed: passed,
		Scan: &verification.ScanResult{HeadSHA: head, PatchSHA256: agents.HashContent(answer)}, Results: results,
	}, nil
}

func (*DeterministicBackend) DesignTests(_ context.Context, job jobs.Job, packet agents.TaskPacket) (testdesigner.Report, error) {
	report := testdesigner.Report{
		JobID: job.ID, SchemaVersion: 1, ContractSHA256: packet.ContractSHA256,
		RiskLevel: packet.RiskLevel, ResultSHA: packet.ResultSHA, SourceContext: "independent_test_designer_context_v1",
		Status: "proposed", DispositionsRequired: true,
		Proposals: []testdesigner.Proposal{{
			ID: "TD-FIXTURE-001", Category: "regression_coverage",
			Claim:       "Add or retain a boundary regression for the candidate change.",
			Rationale:   "The independent Test Designer receives only the approved contract, bounded context, baseline evidence, and candidate diff.",
			EvidenceIDs: []string{"candidate_diff", "baseline_evidence"}, SuggestedTests: []string{"boundary regression test"},
			GoldenRehearsals: []string{}, Disposition: "pending",
		}},
	}
	return report, report.Validate()
}

func (b *DeterministicBackend) Document(_ context.Context, job jobs.Job, packet agents.TaskPacket) (documentation.Manifest, error) {
	if packet.RiskLevel == "low" {
		return documentation.NoDocumentationRequired(job.ID, job.ProjectID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
	}
	path := "docs/mock-maintenance-" + job.ID + ".md"
	action := "created"
	content := "# Mock maintenance documentation\n\n" +
		"- Job: `" + job.ID + "`\n" +
		"- Contract: `" + packet.ContractSHA256 + "`\n" +
		"- Result: `" + packet.ResultSHA + "`\n" +
		"- Policy: documentation-policy-v1\n"
	if err := os.MkdirAll(filepath.Join(b.worktreesRoot, job.ID, "docs"), 0o700); err != nil {
		return documentation.Manifest{}, err
	}
	target := filepath.Join(b.worktreesRoot, job.ID, path)
	if existing, err := os.ReadFile(target); err == nil && string(existing) == content {
		action = "not_changed"
	} else {
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			return documentation.Manifest{}, err
		}
	}
	status := "changes_applied"
	if action == "not_changed" {
		status = "passed"
	}
	manifest := documentation.Manifest{
		JobID: job.ID, ProjectID: job.ProjectID, SchemaVersion: 1, ContractSHA256: packet.ContractSHA256,
		RiskLevel: packet.RiskLevel, ResultSHA: packet.ResultSHA, SourceContext: "documentation_agent_context_v1",
		PolicyVersion: "documentation-policy-v1",
		Requirements: []documentation.Requirement{{
			ID: "DOC-MOCK-MAINTENANCE", Document: path, Reason: "medium/high risk work requires operator-visible documentation evidence",
			Source: "risk_router", RenderTargets: []string{"markdown"}, Required: true, Status: "satisfied",
		}},
		Changes: []documentation.Change{{
			Path: path, Action: action, PolicyRule: "risk-documentation",
			SourceOfTruth: "approved_task_contract", LinkedEvidenceIDs: []string{"task_contract", "candidate_diff"},
		}},
		Checks: []documentation.Check{{
			ID: "DOC-CHECK-MOCK", Kind: "markdown", Target: path, Status: "passed",
			Summary: "deterministic mock documentation was written from controller-bound evidence",
		}},
		UnsupportedClaims: []documentation.UnsupportedClaim{}, Edits: []documentation.Edit{}, Status: status,
		PolicySummary: "medium/high risk documentation policy required source-controlled maintenance documentation evidence",
	}
	return manifest, manifest.Validate()
}

func (*DeterministicBackend) Review(_ context.Context, job jobs.Job, packet agents.TaskPacket) (agents.QCReport, error) {
	report := agents.QCReport{
		SchemaVersion: 1, JobID: job.ID, BaseSHA: job.BaseSHA, ResultSHA: job.ResultSHA,
		Verdict: "pass", Findings: []agents.Finding{}, VerificationRequests: []agents.VerificationRequest{},
	}
	if job.ReviewCycle == 0 {
		report.Verdict = "blocking_findings"
		report.Findings = []agents.Finding{{
			ID: "QC-FIXTURE-001", Severity: "must_fix", Category: "regression_coverage",
			Claim:    "The seeded fixture requires one explicit negative-input regression assertion.",
			Location: agents.Location{Path: "answer_test.go", Line: 1}, Evidence: "The negative-input assertion is absent from the first reviewed exact commit.",
			RequiredResolution: "Add the explicit negative-input regression assertion.", VerificationMethod: "full_tests",
		}}
	}
	payload, _ := json.Marshal(report)
	return agents.DecodeQCReport(payload, packet)
}
