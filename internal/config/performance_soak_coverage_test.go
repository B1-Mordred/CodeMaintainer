package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type performanceSoakCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID             string                       `json:"id"`
		Status         string                       `json:"status"`
		Title          string                       `json:"title"`
		Implementation []string                     `json:"implementation"`
		Tests          []string                     `json:"tests"`
		UI             []string                     `json:"ui"`
		Docs           []string                     `json:"docs"`
		Proof          []performanceSoakProofMarker `json:"proof"`
		RemainingWork  []string                     `json:"remaining_work"`
	} `json:"surfaces"`
}

type performanceSoakProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestPerformanceSoakCoverageMatrixTracksRequiredCriteria(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "performance-soak-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage performanceSoakCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode performance/soak coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"scheduler-resource-safety-and-fairness":         false,
		"runtime-benchmark-quality-gates":                false,
		"historical-evaluation-performance-metrics":      false,
		"observability-bottleneck-and-resource-timeline": false,
		"deterministic-local-acceptance-smoke":           false,
		"bounded-soak-thresholds-and-reports":            false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("performance/soak surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected performance/soak surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate performance/soak surface %q", surface.ID)
		}
		required[surface.ID] = true
		if surface.Status != "covered" && surface.Status != "in_progress" && surface.Status != "remaining" {
			t.Fatalf("%s has invalid status %q", surface.ID, surface.Status)
		}
		if strings.TrimSpace(surface.Title) == "" {
			t.Fatalf("%s lacks title", surface.ID)
		}
		requirePerformanceSoakPaths(t, root, surface.ID, "implementation", surface.Implementation)
		requirePerformanceSoakPaths(t, root, surface.ID, "tests", surface.Tests)
		requirePerformanceSoakPaths(t, root, surface.ID, "ui", surface.UI)
		requirePerformanceSoakPaths(t, root, surface.ID, "docs", surface.Docs)
		requirePerformanceSoakProof(t, root, surface.ID, surface.Proof)
		if surface.Status != "covered" {
			requirePerformanceSoakNonEmpty(t, surface.ID, "remaining_work", surface.RemainingWork)
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("performance/soak coverage matrix omits %s", id)
		}
	}
}

func requirePerformanceSoakPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requirePerformanceSoakNonEmpty(t, id, field, values)
	for _, value := range values {
		path := performanceSoakCoveragePath(value)
		if path == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}

func requirePerformanceSoakProof(t *testing.T, root, id string, proof []performanceSoakProofMarker) {
	t.Helper()
	if len(proof) == 0 {
		t.Fatalf("%s has no proof entries", id)
	}
	for _, entry := range proof {
		path := performanceSoakCoveragePath(entry.Path)
		if path == "" {
			t.Fatalf("%s has blank proof path", id)
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s references missing proof path %q: %v", id, path, err)
		}
		requirePerformanceSoakNonEmpty(t, id, "proof.contains", entry.Contains)
		for _, expected := range entry.Contains {
			if !strings.Contains(string(payload), expected) {
				t.Fatalf("%s proof %s does not contain %q", id, path, expected)
			}
		}
	}
}

func requirePerformanceSoakNonEmpty(t *testing.T, id, field string, values []string) {
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

func performanceSoakCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	if strings.HasPrefix(path, "go test") || strings.HasPrefix(path, "go vet") ||
		strings.HasPrefix(path, "web npm") || strings.HasPrefix(path, "docker compose") {
		return "docs/performance-soak.md"
	}
	return strings.TrimSpace(path)
}
