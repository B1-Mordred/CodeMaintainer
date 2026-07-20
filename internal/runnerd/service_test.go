package runnerd

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/runners"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestServiceAuthenticatesAndRejectsCallerControlledContainerFields(t *testing.T) {
	server, _ := testService(t)
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/runs", strings.NewReader(`{"job_id":"job","project_id":"project","kind":"implementation"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request returned %d", response.StatusCode)
	}

	for _, field := range []string{"image", "command", "mounts", "network", "capabilities", "environment"} {
		payload := `{"job_id":"job","project_id":"project","kind":"implementation","` + field + `":"attacker-controlled"}`
		response = runnerRequest(t, server.URL+"/v1/runs", http.MethodPost, payload)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("caller-controlled %s returned %d", field, response.StatusCode)
		}
	}
}

func TestServiceResolvesPolicyBeforeStartingExecutorAndBoundsLogs(t *testing.T) {
	server, executor := testService(t)
	defer server.Close()
	response := runnerRequest(t, server.URL+"/v1/runs", http.MethodPost,
		`{"job_id":"job_123","project_id":"owner-repo","kind":"qc","input_artifact_ids":["artifact_1"]}`)
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("start returned %d: %s", response.StatusCode, payload)
	}
	var started struct {
		RunID runners.RunID `json:"run_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	spec, exists := executor.Spec(started.RunID)
	if !exists || spec.Image == "" || spec.Network != NetworkInferenceOnly || !spec.Mounts[0].ReadOnly {
		t.Fatalf("executor received unsafe specification: %#v", spec)
	}
	duplicate := runnerRequest(t, server.URL+"/v1/runs", http.MethodPost,
		`{"job_id":"job_123","project_id":"owner-repo","kind":"qc","input_artifact_ids":["artifact_1"]}`)
	if duplicate.StatusCode != http.StatusCreated {
		duplicate.Body.Close()
		t.Fatalf("idempotent start returned %d", duplicate.StatusCode)
	}
	var repeated struct {
		RunID runners.RunID `json:"run_id"`
	}
	if err := json.NewDecoder(duplicate.Body).Decode(&repeated); err != nil {
		t.Fatal(err)
	}
	duplicate.Body.Close()
	if repeated.RunID != started.RunID {
		t.Fatalf("idempotent start returned %s, want %s", repeated.RunID, started.RunID)
	}

	response = runnerRequest(t, server.URL+"/v1/runs/"+string(started.RunID)+"/logs?limit=4", http.MethodGet, "")
	var chunk runners.LogChunk
	if err := json.NewDecoder(response.Body).Decode(&chunk); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(chunk.Data) > 4 || !chunk.Truncated {
		t.Fatalf("logs were not bounded: status=%d chunk=%#v", response.StatusCode, chunk)
	}

	response = runnerRequest(t, server.URL+"/v1/runs/"+string(started.RunID)+"/stop", http.MethodPost, "")
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stop returned %d", response.StatusCode)
	}
}

func TestServiceRejectsPolicyTraversalAndOversizedRequests(t *testing.T) {
	server, _ := testService(t)
	defer server.Close()
	response := runnerRequest(t, server.URL+"/v1/runs", http.MethodPost,
		`{"job_id":"../escape","project_id":"project","kind":"implementation"}`)
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("traversal returned %d", response.StatusCode)
	}
	oversized := `{"job_id":"job","project_id":"project","kind":"implementation","padding":"` + strings.Repeat("x", maxRequestBytes) + `"}`
	response = runnerRequest(t, server.URL+"/v1/runs", http.MethodPost, oversized)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized request returned %d", response.StatusCode)
	}
}

func testService(t *testing.T) (*httptest.Server, *FakeExecutor) {
	t.Helper()
	executor := NewFakeExecutor()
	service, err := NewService(testPolicy(t), executor, []byte(testToken), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(service), executor
}

func runnerRequest(t *testing.T, target, method, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
