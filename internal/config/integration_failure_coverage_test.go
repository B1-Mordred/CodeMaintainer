package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type integrationFailureCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID             string                          `json:"id"`
		Status         string                          `json:"status"`
		Title          string                          `json:"title"`
		Fixtures       []string                        `json:"fixtures"`
		Implementation []string                        `json:"implementation"`
		Tests          []string                        `json:"tests"`
		Docs           []string                        `json:"docs"`
		Proof          []integrationFailureProofMarker `json:"proof"`
		RemainingWork  []string                        `json:"remaining_work"`
	} `json:"surfaces"`
}

type integrationFailureProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestIntegrationFailureCoverageMatrixTracksRequiredFixturesAndFailures(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "integration-failure-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage integrationFailureCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode integration/failure coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"fixtures-php83-composer-mysql-redis-vite-tailwind": false,
		"fixtures-dotnet-r-malformed":                       false,
		"fixtures-forges-providers-localgit":                false,
		"failure-baseline-index-config-restart":             false,
		"failure-pack-policy-cache-memory-docs":             false,
		"failure-provider-egress-telemetry-security":        false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("integration/failure surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected integration/failure surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate integration/failure surface %q", surface.ID)
		}
		required[surface.ID] = true
		if surface.Status != "covered" && surface.Status != "in_progress" && surface.Status != "remaining" {
			t.Fatalf("%s has invalid status %q", surface.ID, surface.Status)
		}
		if strings.TrimSpace(surface.Title) == "" {
			t.Fatalf("%s lacks title", surface.ID)
		}
		requireIntegrationFailureOptionalPaths(t, root, surface.ID, "fixtures", surface.Fixtures)
		requireIntegrationFailurePaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireIntegrationFailurePaths(t, root, surface.ID, "tests", surface.Tests)
		requireIntegrationFailurePaths(t, root, surface.ID, "docs", surface.Docs)
		requireIntegrationFailureProof(t, root, surface.ID, surface.Proof)
		if surface.Status != "covered" {
			requireIntegrationFailureNonEmpty(t, surface.ID, "remaining_work", surface.RemainingWork)
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("integration/failure coverage matrix omits %s", id)
		}
	}
}

func requireIntegrationFailurePaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireIntegrationFailureNonEmpty(t, id, field, values)
	requireIntegrationFailureOptionalPaths(t, root, id, field, values)
}

func requireIntegrationFailureOptionalPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	for _, value := range values {
		path := integrationFailureCoveragePath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}

func requireIntegrationFailureProof(t *testing.T, root, id string, proof []integrationFailureProofMarker) {
	t.Helper()
	if len(proof) == 0 {
		t.Fatalf("%s has no proof entries", id)
	}
	for _, entry := range proof {
		path := integrationFailureCoveragePath(entry.Path)
		if path == "" {
			t.Fatalf("%s has blank proof path", id)
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s references missing proof path %q: %v", id, path, err)
		}
		requireIntegrationFailureNonEmpty(t, id, "proof.contains", entry.Contains)
		for _, expected := range entry.Contains {
			if !strings.Contains(string(payload), expected) {
				t.Fatalf("%s proof %s does not contain %q", id, path, expected)
			}
		}
	}
}

func requireIntegrationFailureNonEmpty(t *testing.T, id, field string, values []string) {
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

func integrationFailureCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
