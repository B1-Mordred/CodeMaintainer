package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type composeAcceptanceCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID             string                         `json:"id"`
		Status         string                         `json:"status"`
		Title          string                         `json:"title"`
		Implementation []string                       `json:"implementation"`
		Tests          []string                       `json:"tests"`
		UI             []string                       `json:"ui"`
		Docs           []string                       `json:"docs"`
		Proof          []composeAcceptanceProofMarker `json:"proof"`
		RemainingWork  []string                       `json:"remaining_work"`
	} `json:"surfaces"`
}

type composeAcceptanceProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestComposeAcceptanceCoverageMatrixTracksRunnableStackEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "compose-acceptance-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage composeAcceptanceCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode compose/acceptance coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"ci-containerized-core-gate":   false,
		"local-acceptance-script":      false,
		"documented-compose-views":     false,
		"mock-runtime-runnable-stack":  false,
		"operator-maintenance-scripts": false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("compose/acceptance surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected compose/acceptance surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate compose/acceptance surface %q", surface.ID)
		}
		required[surface.ID] = true
		if surface.Status != "covered" && surface.Status != "in_progress" && surface.Status != "remaining" {
			t.Fatalf("%s has invalid status %q", surface.ID, surface.Status)
		}
		if strings.TrimSpace(surface.Title) == "" {
			t.Fatalf("%s lacks title", surface.ID)
		}
		requireComposeAcceptancePaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireComposeAcceptancePaths(t, root, surface.ID, "tests", surface.Tests)
		requireComposeAcceptancePaths(t, root, surface.ID, "ui", surface.UI)
		requireComposeAcceptancePaths(t, root, surface.ID, "docs", surface.Docs)
		requireComposeAcceptanceProof(t, root, surface.ID, surface.Proof)
		if surface.Status != "covered" {
			requireComposeAcceptanceNonEmpty(t, surface.ID, "remaining_work", surface.RemainingWork)
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("compose/acceptance coverage matrix omits %s", id)
		}
	}
}

func requireComposeAcceptancePaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireComposeAcceptanceNonEmpty(t, id, field, values)
	for _, value := range values {
		path := composeAcceptanceCoveragePath(value)
		if path == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}

func requireComposeAcceptanceProof(t *testing.T, root, id string, proof []composeAcceptanceProofMarker) {
	t.Helper()
	if len(proof) == 0 {
		t.Fatalf("%s has no proof entries", id)
	}
	for _, entry := range proof {
		path := composeAcceptanceCoveragePath(entry.Path)
		if path == "" {
			t.Fatalf("%s has blank proof path", id)
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s references missing proof path %q: %v", id, path, err)
		}
		requireComposeAcceptanceNonEmpty(t, id, "proof.contains", entry.Contains)
		for _, expected := range entry.Contains {
			if !strings.Contains(string(payload), expected) {
				t.Fatalf("%s proof %s does not contain %q", id, path, expected)
			}
		}
	}
}

func requireComposeAcceptanceNonEmpty(t *testing.T, id, field string, values []string) {
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

func composeAcceptanceCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
