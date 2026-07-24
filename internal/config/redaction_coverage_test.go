package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type redactionCoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Surfaces      []struct {
		ID                string           `json:"id"`
		Title             string           `json:"title"`
		Policy            string           `json:"policy"`
		Implementation    []string         `json:"implementation"`
		Tests             []string         `json:"tests"`
		UI                []string         `json:"ui"`
		Docs              []string         `json:"docs"`
		ForbiddenExamples []string         `json:"forbidden_examples"`
		Proof             []redactionProof `json:"proof"`
	} `json:"surfaces"`
}

type redactionProof struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestRedactionCoverageMatrixCoversSecretBearingOutputs(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "redaction-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage redactionCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode redaction coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"configuration-secret-state":          false,
		"configuration-import-export-secrets": false,
		"provider-egress-manifests":           false,
		"observability-events":                false,
		"support-bundle-manifests":            false,
		"project-memory":                      false,
		"auth-session-credentials":            false,
		"forge-credentials":                   false,
		"windows-worker-credentials":          false,
		"policy-decision-inputs":              false,
	}
	if len(coverage.Surfaces) != len(required) {
		t.Fatalf("redaction surface count = %d, want %d", len(coverage.Surfaces), len(required))
	}
	for _, surface := range coverage.Surfaces {
		if _, ok := required[surface.ID]; !ok {
			t.Fatalf("unexpected redaction surface %q", surface.ID)
		}
		if required[surface.ID] {
			t.Fatalf("duplicate redaction surface %q", surface.ID)
		}
		required[surface.ID] = true
		if strings.TrimSpace(surface.Title) == "" || strings.TrimSpace(surface.Policy) == "" {
			t.Fatalf("%s lacks title or policy text", surface.ID)
		}
		requireRedactionPaths(t, root, surface.ID, "implementation", surface.Implementation)
		requireRedactionPaths(t, root, surface.ID, "tests", surface.Tests)
		requireRedactionPaths(t, root, surface.ID, "ui", surface.UI)
		requireRedactionPaths(t, root, surface.ID, "docs", surface.Docs)
		requireRedactionNonEmpty(t, surface.ID, "forbidden_examples", surface.ForbiddenExamples)
		if len(surface.Proof) == 0 {
			t.Fatalf("%s has no proof snippets", surface.ID)
		}
		for _, proof := range surface.Proof {
			path := redactionCoveragePath(proof.Path)
			if strings.TrimSpace(path) == "" {
				t.Fatalf("%s has blank proof path", surface.ID)
			}
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatalf("%s references missing proof path %q: %v", surface.ID, proof.Path, err)
			}
			requireRedactionNonEmpty(t, surface.ID, "proof.contains", proof.Contains)
			for _, expected := range proof.Contains {
				if !strings.Contains(string(content), expected) {
					t.Fatalf("%s proof %s does not contain %q", surface.ID, proof.Path, expected)
				}
			}
		}
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("redaction coverage matrix omits %s", id)
		}
	}
}

func requireRedactionPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireRedactionNonEmpty(t, id, field, values)
	for _, value := range values {
		path := redactionCoveragePath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, value, err)
		}
	}
}

func requireRedactionNonEmpty(t *testing.T, id, field string, values []string) {
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

func redactionCoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	return strings.TrimSpace(path)
}
