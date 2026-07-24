package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type securityAdversarialCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID             string                           `json:"id"`
		Title          string                           `json:"title"`
		Policy         string                           `json:"policy"`
		Implementation []string                         `json:"implementation"`
		Tests          []string                         `json:"tests"`
		UI             []string                         `json:"ui"`
		Docs           []string                         `json:"docs"`
		Proof          []securityAdversarialProofMarker `json:"proof"`
	} `json:"surfaces"`
}

type securityAdversarialProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestSecurityAdversarialCoverageMatrixCoversRequiredBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "security-adversarial-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage securityAdversarialCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode security/adversarial coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"authorization-reauth-stale-edit":           false,
		"untrusted-input-command-injection":         false,
		"worker-container-isolation":                false,
		"provider-ssrf-egress-fallback-cost":        false,
		"forge-webhook-auth-replay":                 false,
		"cross-project-memory-evaluation-isolation": false,
		"archive-restore-path-safety":               false,
		"unsafe-preview-no-mutation":                false,
		"redaction-secret-suppression":              false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("security/adversarial surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected security/adversarial surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate security/adversarial surface %q", surface.ID)
		}
		required[surface.ID] = true
		if strings.TrimSpace(surface.Title) == "" || strings.TrimSpace(surface.Policy) == "" {
			t.Fatalf("%s lacks title or policy text", surface.ID)
		}
		requireSecurityCoveragePaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireSecurityCoveragePaths(t, root, surface.ID, "tests", surface.Tests)
		requireSecurityCoverageOptionalPaths(t, root, surface.ID, "ui", surface.UI)
		requireSecurityCoveragePaths(t, root, surface.ID, "docs", surface.Docs)
		if len(surface.Proof) == 0 {
			t.Fatalf("%s has no proof snippets", surface.ID)
		}
		for _, proof := range surface.Proof {
			path := securityCoveragePath(proof.Path)
			if strings.TrimSpace(path) == "" {
				t.Fatalf("%s has blank proof path", surface.ID)
			}
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatalf("%s references missing proof path %q: %v", surface.ID, proof.Path, err)
			}
			requireSecurityCoverageNonEmpty(t, surface.ID, "proof.contains", proof.Contains)
			for _, expected := range proof.Contains {
				if !strings.Contains(string(content), expected) {
					t.Fatalf("%s proof %s does not contain %q", surface.ID, proof.Path, expected)
				}
			}
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("security/adversarial coverage matrix omits %s", id)
		}
	}
}

func requireSecurityCoveragePaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireSecurityCoverageNonEmpty(t, id, field, values)
	requireSecurityCoverageOptionalPaths(t, root, id, field, values)
}

func requireSecurityCoverageOptionalPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	for _, value := range values {
		path := securityCoveragePath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, value, err)
		}
	}
}

func requireSecurityCoverageNonEmpty(t *testing.T, id, field string, values []string) {
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

func securityCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
