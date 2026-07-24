package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type configurationParityFile struct {
	SchemaVersion int `json:"schema_version"`
	Operations    []struct {
		ID    string   `json:"id"`
		Title string   `json:"title"`
		REST  []string `json:"rest"`
		CLI   []string `json:"cli"`
		UI    []string `json:"ui"`
		Tests []string `json:"tests"`
		Docs  []string `json:"docs"`
	} `json:"operations"`
}

func TestConfigurationParityMatrixCoversRESTCLIUIAndEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "configuration-parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var matrix configurationParityFile
	if err := json.Unmarshal(payload, &matrix); err != nil {
		t.Fatalf("decode configuration parity matrix: %v", err)
	}
	if matrix.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", matrix.SchemaVersion)
	}
	required := map[string]bool{
		"config-descriptors":     false,
		"config-values":          false,
		"config-effective":       false,
		"config-prerequisites":   false,
		"config-export":          false,
		"config-import-preview":  false,
		"config-import":          false,
		"config-draft-list":      false,
		"config-draft-create":    false,
		"config-draft-read":      false,
		"config-draft-update":    false,
		"config-draft-checks":    false,
		"config-draft-validate":  false,
		"config-draft-dry-run":   false,
		"config-draft-review":    false,
		"config-draft-apply":     false,
		"config-draft-discard":   false,
		"config-history":         false,
		"config-rollback":        false,
		"legacy-config-export":   false,
		"legacy-config-validate": false,
		"legacy-config-apply":    false,
		"legacy-config-rollback": false,
	}
	openAPI := readRequiredText(t, filepath.Join(root, "internal", "api", "openapi.yaml"))
	maintainctl := readRequiredText(t, filepath.Join(root, "cmd", "maintainctl", "main.go"))
	for _, operation := range matrix.Operations {
		if _, ok := required[operation.ID]; !ok {
			t.Fatalf("unexpected configuration parity operation %q", operation.ID)
		}
		if required[operation.ID] {
			t.Fatalf("duplicate configuration parity operation %q", operation.ID)
		}
		required[operation.ID] = true
		if strings.TrimSpace(operation.Title) == "" {
			t.Fatalf("%s has no title", operation.ID)
		}
		for _, route := range operation.REST {
			method, path, ok := strings.Cut(route, " ")
			if !ok || strings.TrimSpace(method) == "" || !strings.HasPrefix(path, "/api/v1/") {
				t.Fatalf("%s has invalid REST route %q", operation.ID, route)
			}
			openAPIPath := strings.TrimPrefix(path, "/api/v1")
			if !strings.Contains(openAPI, openAPIPath+":") {
				t.Fatalf("%s references REST route missing from OpenAPI: %s", operation.ID, route)
			}
		}
		for _, command := range operation.CLI {
			for _, token := range strings.Fields(command) {
				if !strings.Contains(maintainctl, token) {
					t.Fatalf("%s references CLI command token not present in maintainctl: %q from %q", operation.ID, token, command)
				}
			}
		}
		requireCoverageFiles(t, root, operation.ID, "ui", operation.UI)
		requireCoverageFiles(t, root, operation.ID, "tests", operation.Tests)
		requireCoverageFiles(t, root, operation.ID, "docs", operation.Docs)
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("configuration parity matrix omits %s", id)
		}
	}
}

func readRequiredText(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func requireCoverageFiles(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("%s has no %s evidence", id, field)
	}
	for _, value := range values {
		path := strings.SplitN(value, "#", 2)[0]
		path = strings.SplitN(path, ":", 2)[0]
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has blank %s evidence", id, field)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s evidence %q: %v", id, field, value, err)
		}
	}
}
