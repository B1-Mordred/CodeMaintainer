package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePacket(mode string) TaskPacket {
	packet := TaskPacket{
		SchemaVersion: 1, Mode: mode, JobID: "job_fixture", OriginalTask: "fix the arithmetic defect",
		AcceptanceCriteria: []Criterion{{ID: "AC-1", Statement: "addition returns the sum", VerificationMethod: "full_tests"}},
		BaseSHA:            strings.Repeat("a", 40), RelevantFiles: []FileContext{{Path: "answer.go", Content: "package answer"}},
	}
	if mode == "qc" {
		packet.ResultSHA = strings.Repeat("b", 40)
		packet.Diff = "diff --git a/answer.go b/answer.go"
	}
	return packet
}

func TestImplementationAppliesOnlyHashBoundRelativeFileEdits(t *testing.T) {
	worktree := t.TempDir()
	original := []byte("package answer\n\nfunc Add(a, b int) int { return a - b }\n")
	path := filepath.Join(worktree, "answer.go")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := ImplementationResult{
		SchemaVersion: 1,
		Summary:       ActionSummary{Reproduction: "test failed", Plan: "fix operator", Changes: []string{"fixed Add"}, RegressionTests: []string{"TestAdd"}, Checks: []string{"full_tests"}, Evidence: []string{"fixture"}},
		Edits:         []Edit{{Path: "answer.go", Content: "package answer\n\nfunc Add(a, b int) int { return a + b }\n", ExpectedSHA256: HashContent(original)}},
	}
	response, _ := json.Marshal(result)
	server := modelFixtureServer(t, response)
	defer server.Close()
	client, _ := NewModelClient(server.URL + "/v1")
	packet, _ := json.Marshal(fixturePacket("implementation"))
	if _, err := RunImplementation(context.Background(), client, packet, worktree); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if !strings.Contains(string(updated), "a + b") {
		t.Fatalf("edit was not applied: %s", updated)
	}
	result.Edits[0].Path = "../escape"
	response, _ = json.Marshal(result)
	if _, err := DecodeImplementationResult(response); err == nil {
		t.Fatal("prompt-injected path escape was accepted")
	}
}

func TestQCRequiresTaskBindingEvidenceAndConsistentVerdict(t *testing.T) {
	packet := fixturePacket("qc")
	report := QCReport{
		SchemaVersion: 1, JobID: packet.JobID, BaseSHA: packet.BaseSHA, ResultSHA: packet.ResultSHA, Verdict: "blocking_findings",
		Findings: []Finding{{
			ID: "QC-ADD-001", Severity: "must_fix", Category: "correctness", Claim: "negative values regress",
			Location: Location{Path: "answer.go", Line: 3}, Evidence: "TestNegative fails", RequiredResolution: "handle negative values",
			VerificationMethod: "run full_tests including TestNegative",
		}}, VerificationRequests: []VerificationRequest{},
	}
	payload, _ := json.Marshal(report)
	if _, err := DecodeQCReport(payload, packet); err != nil {
		t.Fatal(err)
	}
	report.Findings[0].Evidence = ""
	payload, _ = json.Marshal(report)
	if _, err := DecodeQCReport(payload, packet); err == nil {
		t.Fatal("blocking finding without evidence was accepted")
	}
	report.Findings = nil
	payload, _ = json.Marshal(report)
	if _, err := DecodeQCReport(payload, packet); err == nil {
		t.Fatal("blocking verdict without findings was accepted")
	}
}

func TestPromptsTreatIssueSourceOutputAndMemoryAsUntrustedData(t *testing.T) {
	prompt, _ := promptFiles.ReadFile("prompts/implementation.txt")
	for _, required := range []string{"untrusted", "comments", "test output", "memory", "Do not weaken tests", "Do not reveal hidden reasoning"} {
		if !strings.Contains(string(prompt), required) {
			t.Errorf("implementation prompt lacks %q", required)
		}
	}
}

func modelFixtureServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected model path %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
	}))
}
