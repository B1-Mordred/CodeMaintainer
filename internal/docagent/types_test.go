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
