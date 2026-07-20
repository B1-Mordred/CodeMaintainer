package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func testServer(t *testing.T) (*httptest.Server, *storesqlite.Store) {
	t.Helper()
	store, err := storesqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	document := appconfig.Default(".data")
	after, _ := json.Marshal(document)
	_, err = store.CreateConfigRevision(context.Background(), appconfig.Revision{
		ActorID: "system", SchemaVersion: 1, Before: json.RawMessage(`{}`), After: after,
		Diff: json.RawMessage(`[]`), ValidationResult: json.RawMessage(`{"valid":true}`),
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewServer(store, logger, "mock"))
	t.Cleanup(server.Close)
	t.Cleanup(func() { store.Close() })
	return server, store
}

func TestHealthStaticShellAndSecurityHeaders(t *testing.T) {
	server, _ := testServer(t)
	for _, path := range []string{"/healthz", "/"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", path, response.StatusCode, payload)
		}
		if response.Header.Get("X-Frame-Options") != "DENY" || response.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s missing security headers", path)
		}
	}
}

func TestCreateInspectCancelRetryJob(t *testing.T) {
	server, _ := testServer(t)
	payload := []byte(`{"project_id":"owner-repo","repository":"owner/repo","task":"repair the seeded defect"}`)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Maintainer-Actor", "test-operator")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("create returned %d: %s", response.StatusCode, body)
	}
	var created struct {
		ID      string `json:"id"`
		State   string `json:"state"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.State != "queued" || created.Version != 1 || created.ID == "" {
		t.Fatalf("unexpected created job: %#v", created)
	}
	for _, action := range []string{"cancel", "retry"} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs/"+created.ID+"/actions/"+action, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s returned %d: %s", action, response.StatusCode, body)
		}
	}
	response, err = http.Get(server.URL + "/api/v1/jobs/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var inspected struct {
		Job struct {
			State   string `json:"state"`
			Version int64  `json:"version"`
		} `json:"job"`
		Transitions []any `json:"transitions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Job.State != "queued" || inspected.Job.Version != 3 || len(inspected.Transitions) != 3 {
		t.Fatalf("unexpected inspected job: %#v", inspected)
	}
}

func TestRequestBoundaryRejectsUnknownFieldsAndBadRepository(t *testing.T) {
	server, _ := testServer(t)
	for _, payload := range []string{
		`{"project_id":"p","repository":"owner/repo","task":"x","command":"rm"}`,
		`{"project_id":"p","repository":"../../host","task":"x"}`,
	} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 || response.StatusCode >= 500 {
			t.Fatalf("unsafe payload returned %d: %s", response.StatusCode, payload)
		}
	}
}

func TestEventStreamReplaysDurableInitialTransition(t *testing.T) {
	server, _ := testServer(t)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(`{"project_id":"p","repository":"owner/repo","task":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	streamRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/jobs/"+created.ID+"/events", nil)
	streamResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	reader := bufio.NewReader(streamResponse.Body)
	var event strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		event.WriteString(line)
		if line == "\n" {
			break
		}
	}
	cancel()
	streamResponse.Body.Close()
	if !strings.Contains(event.String(), "event: transition") || !strings.Contains(event.String(), `"to":"queued"`) {
		t.Fatalf("unexpected event: %s", event.String())
	}
}

func TestConfigurationApplyValidationAndRollbackAreVersioned(t *testing.T) {
	server, store := testServer(t)
	ctx := context.Background()
	initial, err := store.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document := appconfig.Default(".data")
	document.Workflow.MaxReviewCycles = 3

	response := postJSON(t, server.URL+"/api/v1/config/revisions", map[string]any{
		"document": document, "reason": "allow one additional bounded review cycle",
	})
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("configuration apply returned %d: %s", response.StatusCode, payload)
	}
	var applied appconfig.Revision
	if err := json.NewDecoder(response.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if applied.Sequence != initial.Sequence+1 || applied.Reason == "" || string(applied.Diff) == "[]" {
		t.Fatalf("configuration revision lacks evidence: %#v", applied)
	}
	response = postJSON(t, server.URL+"/api/v1/config/validate", map[string]any{"document": document})
	var safeValidation struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&safeValidation); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !safeValidation.Valid || safeValidation.Errors == nil || len(safeValidation.Errors) != 0 {
		t.Fatalf("safe validation must return an empty errors array: %#v", safeValidation)
	}

	unsafe := document
	unsafe.Deployment.DataRoot = "/browser-controlled-host-path"
	response = postJSON(t, server.URL+"/api/v1/config/validate", map[string]any{"document": unsafe})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("validation returned %d", response.StatusCode)
	}
	var validation struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if validation.Valid || len(validation.Errors) == 0 {
		t.Fatalf("unsafe data root passed validation: %#v", validation)
	}
	response = postJSON(t, server.URL+"/api/v1/config/revisions", map[string]any{
		"document": unsafe, "reason": "attempt unsafe path",
	})
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unsafe apply returned %d", response.StatusCode)
	}

	response = postJSON(t, server.URL+"/api/v1/config/revisions/"+initial.ID+"/rollback", map[string]any{
		"reason": "restore the initial review-cycle policy",
	})
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("rollback returned %d: %s", response.StatusCode, payload)
	}
	var rolledBack appconfig.Revision
	if err := json.NewDecoder(response.Body).Decode(&rolledBack); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if rolledBack.RollbackOf != initial.ID || rolledBack.Sequence != initial.Sequence+2 {
		t.Fatalf("rollback provenance is incomplete: %#v", rolledBack)
	}
	current, err := store.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var restored appconfig.System
	if err := json.Unmarshal(current.After, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Workflow.MaxReviewCycles != 2 {
		t.Fatalf("rollback restored max_review_cycles=%d", restored.Workflow.MaxReviewCycles)
	}
}

func postJSON(t *testing.T, target string, value any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Maintainer-Actor", "test-administrator")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
