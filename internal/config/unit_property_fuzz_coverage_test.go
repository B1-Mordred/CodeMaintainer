package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type unitPropertyFuzzCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID             string                        `json:"id"`
		Status         string                        `json:"status"`
		Title          string                        `json:"title"`
		Implementation []string                      `json:"implementation"`
		Tests          []string                      `json:"tests"`
		Docs           []string                      `json:"docs"`
		Proof          []unitPropertyFuzzProofMarker `json:"proof"`
		RemainingWork  []string                      `json:"remaining_work"`
	} `json:"surfaces"`
}

type unitPropertyFuzzProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestUnitPropertyFuzzCoverageMatrixTracksRequiredCategories(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "unit-property-fuzz-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage unitPropertyFuzzCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode unit/property/fuzz coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"unit-schema-precedence-migrations":                   false,
		"unit-cache-index-context-budget":                     false,
		"unit-risk-policy-differential-impact-evidence":       false,
		"unit-pack-redaction-worker-forge":                    false,
		"unit-provider-route-capability-cost-egress-fallback": false,
		"fuzz-imports-manifests-webhooks-archives":            false,
		"fuzz-paths-toolcalls-markdown":                       false,
		"fuzz-streamed-provider-events":                       false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("unit/property/fuzz surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected unit/property/fuzz surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate unit/property/fuzz surface %q", surface.ID)
		}
		required[surface.ID] = true
		if surface.Status != "covered" && surface.Status != "remaining" {
			t.Fatalf("%s has invalid status %q", surface.ID, surface.Status)
		}
		if strings.TrimSpace(surface.Title) == "" {
			t.Fatalf("%s lacks title", surface.ID)
		}
		requireUnitPropertyFuzzPaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireUnitPropertyFuzzPaths(t, root, surface.ID, "tests", surface.Tests)
		requireUnitPropertyFuzzPaths(t, root, surface.ID, "docs", surface.Docs)
		requireUnitPropertyFuzzProof(t, root, surface.ID, surface.Proof)
		if surface.Status == "remaining" {
			requireUnitPropertyFuzzNonEmpty(t, surface.ID, "remaining_work", surface.RemainingWork)
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("unit/property/fuzz coverage matrix omits %s", id)
		}
	}
}

func requireUnitPropertyFuzzPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireUnitPropertyFuzzNonEmpty(t, id, field, values)
	for _, value := range values {
		path := unitPropertyFuzzCoveragePath(value)
		if path == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}

func requireUnitPropertyFuzzProof(t *testing.T, root, id string, proof []unitPropertyFuzzProofMarker) {
	t.Helper()
	if len(proof) == 0 {
		t.Fatalf("%s has no proof entries", id)
	}
	for _, entry := range proof {
		path := unitPropertyFuzzCoveragePath(entry.Path)
		if path == "" {
			t.Fatalf("%s has blank proof path", id)
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s references missing proof path %q: %v", id, path, err)
		}
		requireUnitPropertyFuzzNonEmpty(t, id, "proof.contains", entry.Contains)
		for _, expected := range entry.Contains {
			if !strings.Contains(string(payload), expected) {
				t.Fatalf("%s proof %s does not contain %q", id, path, expected)
			}
		}
	}
}

func requireUnitPropertyFuzzNonEmpty(t *testing.T, id, field string, values []string) {
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

func unitPropertyFuzzCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
