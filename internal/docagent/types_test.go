package docagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestRequiresExactJobBindingAndBlocksUnsupportedClaims(t *testing.T) {
	manifest := Manifest{
		JobID: "job-doc", ProjectID: "project-doc", SchemaVersion: 1,
		ContractSHA256: strings.Repeat("a", 64), RiskLevel: "medium", ResultSHA: strings.Repeat("b", 40),
		SourceContext: "documentation_agent_context_v1", PolicyVersion: "documentation-policy-v1",
		Requirements: []Requirement{{
			ID: "DOC-README", Document: "docs/quality-workflow.md", Reason: "workflow state changed",
			Source: "candidate_diff", RenderTargets: []string{"markdown"}, Required: true, Status: "satisfied",
		}},
		Changes: []Change{{
			Path: "docs/quality-workflow.md", Action: "updated", PolicyRule: "workflow-documentation",
			SourceOfTruth: "LOCAL_CODE_MAINTAINER_INCREMENT_2_GOAL.md", LinkedEvidenceIDs: []string{"candidate_diff"},
		}},
		Checks: []Check{{
			ID: "DOC-CHECK-MARKDOWN", Kind: "render", Target: "docs/quality-workflow.md",
			Status: "passed", Summary: "markdown render target validated with pinned policy placeholder",
		}},
		UnsupportedClaims: []UnsupportedClaim{}, Edits: []Edit{}, Status: "changes_applied",
		PolicySummary: "documentation policy required and validated source-controlled workflow documentation",
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeManifest(payload, manifest.JobID, manifest.ProjectID, manifest.ContractSHA256, manifest.RiskLevel, manifest.ResultSHA)
	if err != nil || decoded.Status != "changes_applied" {
		t.Fatalf("decode manifest = %#v, %v", decoded, err)
	}
	if _, err := DecodeManifest(payload, "other-job", manifest.ProjectID, manifest.ContractSHA256, manifest.RiskLevel, manifest.ResultSHA); err == nil {
		t.Fatal("manifest with mismatched job binding was accepted")
	}
	manifest.UnsupportedClaims = []UnsupportedClaim{{Claim: "rendered PDF is accessible", Location: "docs/quality-workflow.md", Reason: "no PDF renderer evidence was retained"}}
	payload, _ = json.Marshal(manifest)
	if _, err := DecodeManifest(payload, manifest.JobID, manifest.ProjectID, manifest.ContractSHA256, manifest.RiskLevel, manifest.ResultSHA); err == nil {
		t.Fatal("unsupported documentation claim did not block manifest")
	}
}

func TestNoDocumentationRequiredIsBoundedEvidence(t *testing.T) {
	manifest, err := NoDocumentationRequired("job-doc", "project-doc", strings.Repeat("a", 64), "low", strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "no_documentation_required" || len(manifest.Requirements) != 0 || len(manifest.Checks) != 1 {
		t.Fatalf("unexpected no-doc manifest: %#v", manifest)
	}
}

func TestBuiltinDocumentationPolicySimulatesRequiredDocsChecksAndRenderTargets(t *testing.T) {
	profile := BuiltinPolicyProfile()
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
	result, err := SimulatePolicy(PolicySimulationInput{
		Paths:         []string{"internal/api/openapi.yaml", "cmd/maintainctl/main.go"},
		ChangeClasses: []string{"public_api", "cli"},
		RiskLevel:     "medium",
		Languages:     []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NoDocumentationOK || result.PublicationReady || len(result.MatchedRules) < 3 {
		t.Fatalf("simulation did not gate docs: %#v", result)
	}
	documents := map[string]bool{}
	for _, requirement := range result.Requirements {
		documents[requirement.Document] = true
		if requirement.Status != "pending" || !requirement.Required {
			t.Fatalf("requirement not pending required evidence: %#v", requirement)
		}
	}
	for _, document := range []string{"docs/api.md", "docs/cli.md", "docs/quality-workflow.md"} {
		if !documents[document] {
			t.Fatalf("required document %s missing from %#v", document, result.Requirements)
		}
	}
	if len(result.Checks) == 0 || len(result.RenderTargets) == 0 || len(result.SourceMappings) == 0 {
		t.Fatalf("simulation omitted checks/render targets/source mappings: %#v", result)
	}
}

func TestDocumentationPolicyRejectsUnboundedSimulationInput(t *testing.T) {
	if _, err := SimulatePolicy(PolicySimulationInput{Paths: []string{"../secret"}}); err == nil {
		t.Fatal("unsafe simulation path was accepted")
	}
	if _, err := SimulatePolicy(PolicySimulationInput{}); err == nil {
		t.Fatal("empty simulation input was accepted")
	}
}
