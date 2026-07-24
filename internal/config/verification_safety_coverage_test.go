package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type verificationSafetyCoverageFile struct {
	SchemaVersion int    `json:"schema_version"`
	RequirementID string `json:"requirement_id"`
	Surfaces      []struct {
		ID             string                    `json:"id"`
		Claim          string                    `json:"claim"`
		Implementation []string                  `json:"implementation"`
		Tests          []string                  `json:"tests"`
		UI             []string                  `json:"ui"`
		Docs           []string                  `json:"docs"`
		Proof          []verificationSafetyProof `json:"proof"`
	} `json:"surfaces"`
}

type verificationSafetyProof struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestVerificationSafetyCoverageCoversDOD13Evidence(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "verification-safety-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage verificationSafetyCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode verification safety coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	if coverage.RequirementID != "I2-DOD-13" {
		t.Fatalf("requirement id = %q, want I2-DOD-13", coverage.RequirementID)
	}
	required := map[string]bool{
		"new-failure-non-waiver":          false,
		"final-full-suite-non-waiver":     false,
		"golden-update-explicit-approval": false,
		"operator-surface-parity":         false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("verification safety coverage surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected verification safety coverage surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate verification safety coverage surface %q", surface.ID)
		}
		required[surface.ID] = true
		if strings.TrimSpace(surface.Claim) == "" {
			t.Fatalf("%s lacks a safety claim", surface.ID)
		}
		requireVerificationSafetyPaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireVerificationSafetyPaths(t, root, surface.ID, "tests", surface.Tests)
		requireVerificationSafetyPaths(t, root, surface.ID, "ui", surface.UI)
		requireVerificationSafetyPaths(t, root, surface.ID, "docs", surface.Docs)
		if len(surface.Proof) == 0 {
			t.Fatalf("%s has no proof markers", surface.ID)
		}
		for _, proof := range surface.Proof {
			path := verificationSafetyPath(proof.Path)
			if strings.TrimSpace(path) == "" {
				t.Fatalf("%s has blank proof path", surface.ID)
			}
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatalf("%s references missing proof path %q: %v", surface.ID, proof.Path, err)
			}
			requireVerificationSafetyNonEmpty(t, surface.ID, "proof.contains", proof.Contains)
			for _, expected := range proof.Contains {
				if !strings.Contains(string(content), expected) {
					t.Fatalf("%s proof %s does not contain %q", surface.ID, proof.Path, expected)
				}
			}
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("verification safety coverage matrix omits %s", id)
		}
	}
}

func requireVerificationSafetyPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireVerificationSafetyNonEmpty(t, id, field, values)
	for _, value := range values {
		path := verificationSafetyPath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, value, err)
		}
	}
}

func requireVerificationSafetyNonEmpty(t *testing.T, id, field string, values []string) {
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

func verificationSafetyPath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
