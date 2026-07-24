package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type documentationPolicyCoverageFile struct {
	SchemaVersion      int      `json:"schema_version"`
	RequirementID      string   `json:"requirement_id"`
	KnownRemainingWork []string `json:"known_remaining_work"`
	Surfaces           []struct {
		ID             string                     `json:"id"`
		Claim          string                     `json:"claim"`
		Implementation []string                   `json:"implementation"`
		Tests          []string                   `json:"tests"`
		UI             []string                   `json:"ui"`
		Docs           []string                   `json:"docs"`
		Proof          []documentationPolicyProof `json:"proof"`
	} `json:"surfaces"`
}

type documentationPolicyProof struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestDocumentationPolicyCoverageCoversDOD14Evidence(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "documentation-policy-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage documentationPolicyCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode documentation policy coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	if coverage.RequirementID != "I2-DOD-14" {
		t.Fatalf("requirement id = %q, want I2-DOD-14", coverage.RequirementID)
	}
	required := map[string]bool{
		"policy-impact-selection":             false,
		"source-controlled-generation":        false,
		"render-validate-review-blockers":     false,
		"fresh-verification-and-traceability": false,
		"operator-surface-parity":             false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("documentation policy coverage surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected documentation policy coverage surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate documentation policy coverage surface %q", surface.ID)
		}
		required[surface.ID] = true
		if strings.TrimSpace(surface.Claim) == "" {
			t.Fatalf("%s lacks a documentation policy claim", surface.ID)
		}
		requireDocumentationPolicyPaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireDocumentationPolicyPaths(t, root, surface.ID, "tests", surface.Tests)
		requireDocumentationPolicyPaths(t, root, surface.ID, "ui", surface.UI)
		requireDocumentationPolicyPaths(t, root, surface.ID, "docs", surface.Docs)
		if len(surface.Proof) == 0 {
			t.Fatalf("%s has no proof markers", surface.ID)
		}
		for _, proof := range surface.Proof {
			path := documentationPolicyPath(proof.Path)
			if strings.TrimSpace(path) == "" {
				t.Fatalf("%s has blank proof path", surface.ID)
			}
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatalf("%s references missing proof path %q: %v", surface.ID, proof.Path, err)
			}
			requireDocumentationPolicyNonEmpty(t, surface.ID, "proof.contains", proof.Contains)
			for _, expected := range proof.Contains {
				if !strings.Contains(string(content), expected) {
					t.Fatalf("%s proof %s does not contain %q", surface.ID, proof.Path, expected)
				}
			}
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("documentation policy coverage matrix omits %s", id)
		}
	}
	if len(coverage.KnownRemainingWork) == 0 {
		t.Fatal("documentation policy coverage must explicitly retain known remaining work while DOD-14 is in progress")
	}
}

func requireDocumentationPolicyPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireDocumentationPolicyNonEmpty(t, id, field, values)
	for _, value := range values {
		path := documentationPolicyPath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, value, err)
		}
	}
}

func requireDocumentationPolicyNonEmpty(t *testing.T, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("%s has no %s entries", id, field)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s has blank %s entry", id, field)
		}
	}
}

func documentationPolicyPath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
