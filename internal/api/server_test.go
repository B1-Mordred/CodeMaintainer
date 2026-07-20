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

	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func testServer(t *testing.T) (*httptest.Server, *storesqlite.Store) {
	server, store, _ := testServerWithArtifacts(t)
	return server, store
}

func testServerWithArtifacts(t *testing.T) (*httptest.Server, *storesqlite.Store, *artifactfiles.Store) {
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
	if _, err := store.UpsertProject(context.Background(), projects.UpsertRequest{
		ID: "owner-repo", Provider: "local", Repository: "owner/repo",
		DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}, "test-admin"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	artifactStore, err := artifactfiles.New(t.TempDir(), store)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, logger, "mock", WithArtifactReader(artifactStore)))
	t.Cleanup(server.Close)
	t.Cleanup(func() { store.Close() })
	return server, store, artifactStore
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

func TestAuthenticationBootstrapSessionCSRFAndHeaderForgeryRejection(t *testing.T) {
	store, err := storesqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.UpsertProject(context.Background(), projects.UpsertRequest{
		ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}, "setup"); err != nil {
		t.Fatal(err)
	}
	authService, err := maintainerauth.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", WithAuthentication(authService, false)))
	t.Cleanup(server.Close)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/system/status", nil)
	request.Header.Set("X-Maintainer-Actor", "forged")
	request.Header.Set("X-Maintainer-Role", "administrator")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged headers returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/bootstrap", strings.NewReader(
		`{"username":"admin","display_name":"Administrator","password":"correct horse battery staple"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var bootstrap struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&bootstrap); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || bootstrap.CSRFToken == "" {
		t.Fatalf("bootstrap returned %d: %+v", response.StatusCode, bootstrap)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe session cookie: %+v", sessionCookie)
	}

	jobPayload := `{"project_id":"owner-repo","repository":"owner/repo","task":"authenticated task"}`
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(jobPayload))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(jobPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", bootstrap.CSRFToken)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site request returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(jobPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", bootstrap.CSRFToken)
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated mutation returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/session", nil)
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || session.CSRFToken == "" || session.CSRFToken == bootstrap.CSRFToken {
		t.Fatalf("session CSRF rotation failed: status=%d response=%+v", response.StatusCode, session)
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

func TestProjectsAreSchemaValidatedBeforeJobsCanReferenceThem(t *testing.T) {
	server, _ := testServer(t)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects", strings.NewReader(
		`{"id":"second","provider":"local","repository":"fixture/second","default_branch":"main","local_remote_name":"second.git"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("project create returned %d", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/api/v1/projects")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Items []projects.Project `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(listed.Items) != 2 {
		t.Fatalf("project list = %#v", listed.Items)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(
		`{"project_id":"missing","repository":"fixture/missing","task":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unregistered project job returned %d", response.StatusCode)
	}
}

func TestJobArtifactsAreListedAndDownloadedWithIntegrityMetadata(t *testing.T) {
	server, store, artifactStore := testServerWithArtifacts(t)
	ctx := context.Background()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_artifact_api", ProjectID: "project", Repository: "owner/repo", Task: "verify", ActorID: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := artifactStore.Put(ctx, artifactfiles.PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: "command_result",
		MediaType: "application/json", Producer: "verifier", Metadata: json.RawMessage(`{"class":"full_tests"}`),
		Reader: strings.NewReader(`{"exit_code":0}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/v1/jobs/" + job.ID + "/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(listed, []byte(record.ID)) {
		t.Fatalf("artifact list returned %d: %s", response.StatusCode, listed)
	}
	response, err = http.Get(server.URL + "/api/v1/jobs/" + job.ID + "/artifacts/" + record.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(payload) != `{"exit_code":0}` ||
		response.Header.Get("X-Artifact-SHA256") != record.ObjectSHA256 ||
		response.Header.Get("Content-Disposition") == "" {
		t.Fatalf("artifact download returned %d headers=%v body=%s", response.StatusCode, response.Header, payload)
	}
	response, err = http.Get(server.URL + "/api/v1/jobs/another-job/artifacts/" + record.ID)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-job artifact download returned %d", response.StatusCode)
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
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(`{"project_id":"owner-repo","repository":"owner/repo","task":"x"}`))
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
