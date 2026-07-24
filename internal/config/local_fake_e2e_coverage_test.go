package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type localFakeE2ECoverageFile struct {
	SchemaVersion int `json:"schema_version"`
	Steps         []struct {
		Step           int                       `json:"step"`
		ID             string                    `json:"id"`
		Status         string                    `json:"status"`
		Title          string                    `json:"title"`
		Fixtures       []string                  `json:"fixtures"`
		Implementation []string                  `json:"implementation"`
		Tests          []string                  `json:"tests"`
		UI             []string                  `json:"ui"`
		Docs           []string                  `json:"docs"`
		Proof          []localFakeE2EProofMarker `json:"proof"`
		RemainingWork  []string                  `json:"remaining_work"`
	} `json:"steps"`
}

type localFakeE2EProofMarker struct {
	Path     string   `json:"path"`
	Contains []string `json:"contains"`
}

func TestLocalFakeE2ECoverageMatrixTracksAllFourteenContractSteps(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "local-fake-e2e-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage localFakeE2ECoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode local-fake E2E coverage: %v", err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", coverage.SchemaVersion)
	}
	if len(coverage.Steps) != 14 {
		t.Fatalf("local-fake E2E step count = %d, want 14", len(coverage.Steps))
	}
	seen := map[int]bool{}
	for _, step := range coverage.Steps {
		if step.Step < 1 || step.Step > 14 {
			t.Fatalf("invalid local-fake E2E step number %d", step.Step)
		}
		if seen[step.Step] {
			t.Fatalf("duplicate local-fake E2E step %d", step.Step)
		}
		seen[step.Step] = true
		if step.ID == "" || strings.TrimSpace(step.Title) == "" {
			t.Fatalf("step %d lacks id or title", step.Step)
		}
		if step.Status != "covered" && step.Status != "in_progress" && step.Status != "remaining" {
			t.Fatalf("step %d has invalid status %q", step.Step, step.Status)
		}
		requireLocalFakeE2EOptionalPaths(t, root, step.ID, "fixtures", step.Fixtures)
		requireLocalFakeE2EPaths(t, root, step.ID, "implementation", step.Implementation)
		requireLocalFakeE2EPaths(t, root, step.ID, "tests", step.Tests)
		requireLocalFakeE2EPaths(t, root, step.ID, "ui", step.UI)
		requireLocalFakeE2EPaths(t, root, step.ID, "docs", step.Docs)
		requireLocalFakeE2EProof(t, root, step.ID, step.Proof)
		if step.Status != "covered" {
			requireLocalFakeE2ENonEmpty(t, step.ID, "remaining_work", step.RemainingWork)
		}
	}
	for index := 1; index <= 14; index++ {
		if !seen[index] {
			t.Fatalf("local-fake E2E coverage omits contract step %d", index)
		}
	}
}

func requireLocalFakeE2EPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	requireLocalFakeE2ENonEmpty(t, id, field, values)
	requireLocalFakeE2EOptionalPaths(t, root, id, field, values)
}

func requireLocalFakeE2EOptionalPaths(t *testing.T, root, id, field string, values []string) {
	t.Helper()
	for _, value := range values {
		path := localFakeE2ECoveragePath(value)
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s has invalid %s path %q", id, field, value)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s references missing %s path %q: %v", id, field, path, err)
		}
	}
}

func requireLocalFakeE2EProof(t *testing.T, root, id string, proof []localFakeE2EProofMarker) {
	t.Helper()
	if len(proof) == 0 {
		t.Fatalf("%s has no proof entries", id)
	}
	for _, entry := range proof {
		path := localFakeE2ECoveragePath(entry.Path)
		if path == "" {
			t.Fatalf("%s has blank proof path", id)
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s references missing proof path %q: %v", id, path, err)
		}
		requireLocalFakeE2ENonEmpty(t, id, "proof.contains", entry.Contains)
		for _, expected := range entry.Contains {
			if !strings.Contains(string(payload), expected) {
				t.Fatalf("%s proof %s does not contain %q", id, path, expected)
			}
		}
	}
}

func requireLocalFakeE2ENonEmpty(t *testing.T, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("%s has no %s entries", id, field)
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s has blank %s entry %d", id, field, index)
		}
	}
}

func localFakeE2ECoveragePath(value string) string {
	path := strings.SplitN(value, "#", 2)[0]
	path = strings.SplitN(path, ":", 2)[0]
	if strings.HasPrefix(path, "step") {
		return ""
	}
	if _, err := fmt.Sscanf(path, "%d", new(int)); err == nil {
		return ""
	}
	return strings.TrimSpace(path)
}
