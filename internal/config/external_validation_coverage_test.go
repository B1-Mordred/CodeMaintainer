package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type externalValidationCoverage struct {
	SchemaVersion int `json:"schema_version"`
	Items         []struct {
		ID                    string   `json:"id"`
		Title                 string   `json:"title"`
		OperatorPrerequisites []string `json:"operator_prerequisites"`
		SafeFakeEvidence      []string `json:"safe_fake_evidence"`
		OperatorProcedure     []string `json:"operator_procedure"`
		NotClaimedByCI        string   `json:"not_claimed_by_ci"`
	} `json:"items"`
}

func TestExternalValidationCoverageHasSafeFakesAndFiniteProcedures(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "external-validation-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage externalValidationCoverage
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode external validation coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	required := map[string]bool{
		"external-rootless-worker-topology":         false,
		"external-licensed-model-weights":           false,
		"external-github-app-installation":          false,
		"external-openviking-embeddings":            false,
		"external-hermes-profile":                   false,
		"external-remote-auth-tls":                  false,
		"external-separate-host-encrypted-restore":  false,
		"external-oci-vulnerability-signature-scan": false,
	}
	if len(coverage.Items) != len(required) {
		t.Fatalf("external validation item count = %d, want %d", len(coverage.Items), len(required))
	}
	for _, item := range coverage.Items {
		if _, ok := required[item.ID]; !ok {
			t.Fatalf("unexpected external validation id %q", item.ID)
		}
		if required[item.ID] {
			t.Fatalf("duplicate external validation id %q", item.ID)
		}
		required[item.ID] = true
		if strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.NotClaimedByCI) == "" {
			t.Fatalf("%s is missing title or CI boundary", item.ID)
		}
		requireNonEmptyStrings(t, item.ID, "operator_prerequisites", item.OperatorPrerequisites)
		requireExistingCoveragePaths(t, root, item.ID, "safe_fake_evidence", item.SafeFakeEvidence)
		requireExistingCoveragePaths(t, root, item.ID, "operator_procedure", item.OperatorProcedure)
	}
}

func requireNonEmptyStrings(t *testing.T, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("%s has no %s", id, field)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s has a blank %s entry", id, field)
		}
	}
}

func requireExistingCoveragePaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireNonEmptyStrings(t, id, field, values)
	for _, value := range values {
		path := strings.SplitN(value, "#", 2)[0]
		path = strings.SplitN(path, ":", 2)[0]
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}
