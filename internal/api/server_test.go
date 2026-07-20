package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/agents"
	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/models"
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
	configRevision, err := store.CreateConfigRevision(context.Background(), appconfig.Revision{
		ActorID: "system", SchemaVersion: 1, Before: json.RawMessage(`{}`), After: after,
		Diff: json.RawMessage(`[]`), ValidationResult: json.RawMessage(`{"valid":true}`),
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	registry, err := appconfig.BuiltInRegistry(document)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	configRegistry, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := configRegistry.EnsureSystemScope(context.Background(), document, configRevision.ID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.SetConfigurationRegistry(registry); err != nil {
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
	intelligenceService, err := intelligence.NewService(store, nil)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, logger, "mock", WithArtifactReader(artifactStore), WithConfigRegistry(configRegistry), WithIntelligence(intelligenceService)))
	t.Cleanup(server.Close)
	t.Cleanup(func() { store.Close() })
	return server, store, artifactStore
}

func TestIntelligenceAPIIsProjectScopedAndNeverAcceptsBrowserSource(t *testing.T) {
	server, store := testServer(t)
	service, _ := intelligence.NewService(store, nil)
	content := []byte("package fixture\nfunc Target() {}\n")
	digest := sha256.Sum256(content)
	_, err := service.Index(context.Background(), intelligence.IndexRequest{ProjectID: "owner-repo", Repository: "owner/repo", Revision: "abc123", ParserID: "controller-syntax-v1", Files: []intelligence.SourceFile{{Path: "target.go", BlobSHA256: hex.EncodeToString(digest[:]), Content: content}}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/v1/projects/owner-repo/intelligence/status")
	if err != nil {
		t.Fatal(err)
	}
	var status intelligence.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || status.Files != 1 || status.ProjectID != "owner-repo" || !status.IndexingEnabled || status.RetentionDays != 30 || status.CacheQuotaBytes != 512<<20 {
		t.Fatalf("status %d %#v", response.StatusCode, status)
	}
	response, err = http.Post(server.URL+"/api/v1/projects/owner-repo/intelligence/query", "application/json", strings.NewReader(`{"term":"Target","limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var query intelligence.QueryResult
	if err := json.NewDecoder(response.Body).Decode(&query); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(query.Symbols) != 1 {
		t.Fatalf("query %d %#v", response.StatusCode, query)
	}
	packet, err := service.CompileContext(context.Background(), "owner-repo", "", "qc", 512, 128, []intelligence.ContextCandidate{{ID: "target", Source: "index", Version: "abc123", Reason: "changed symbol", Trust: "untrusted", Content: content, Priority: 1}})
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/api/v1/projects/owner-repo/context-manifests/" + packet.Manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	var manifest intelligence.ContextManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || manifest.ProjectID != "owner-repo" || len(manifest.Selections) != 1 {
		t.Fatalf("manifest %d %#v", response.StatusCode, manifest)
	}
	response, err = http.Post(server.URL+"/api/v1/projects/owner-repo/intelligence/actions/refresh", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("refresh without trusted source = %d", response.StatusCode)
	}
	response, err = http.Post(server.URL+"/api/v1/projects/owner-repo/intelligence/actions/rebuild", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("rebuild without audited reason = %d", response.StatusCode)
	}
	response, err = http.Post(server.URL+"/api/v1/projects/owner-repo/intelligence/actions/rebuild", "application/json", strings.NewReader(`{"reason":"replace stale derived index"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reasoned rebuild = %d", response.StatusCode)
	}
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

func TestConfigurationRegistryAPIUsesTypedDraftsETagsAndRollback(t *testing.T) {
	server, store := testServer(t)
	response, err := http.Get(server.URL + "/api/v1/config/descriptors?advanced=true")
	if err != nil {
		t.Fatal(err)
	}
	var descriptors struct {
		SchemaVersion int                    `json:"schema_version"`
		Items         []appconfig.Descriptor `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&descriptors); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || descriptors.SchemaVersion != 1 || len(descriptors.Items) != 19 {
		t.Fatalf("descriptors returned %d: %#v", response.StatusCode, descriptors)
	}
	response, err = http.Get(server.URL + "/api/v1/config/prerequisites")
	if err != nil {
		t.Fatal(err)
	}
	var prerequisiteResult struct {
		Items []appconfig.PrerequisiteStatus `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&prerequisiteResult); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(prerequisiteResult.Items) != 1 || prerequisiteResult.Items[0].Status != "passed" {
		t.Fatalf("prerequisite health returned %d: %#v", response.StatusCode, prerequisiteResult)
	}

	response, err = http.Get(server.URL + "/api/v1/config/values?scope_kind=system")
	if err != nil {
		t.Fatal(err)
	}
	var scope appconfig.ScopeState
	if err := json.NewDecoder(response.Body).Decode(&scope); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") != `"config-scope-1"` || len(scope.Values) != 10 {
		t.Fatalf("system scope returned %d %q: %#v", response.StatusCode, response.Header.Get("ETag"), scope)
	}

	draftPayload := `{"scope":{"kind":"system"},"reason":"increase review depth","entries":[{"key":"workflow.max_review_cycles","value":4,"configured":true,"secret":false,"reset":false}]}`
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/config/drafts", strings.NewReader(draftPayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("draft without If-Match returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/config/drafts", strings.NewReader(draftPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"config-scope-1"`)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Draft      appconfig.Draft            `json:"draft"`
		Validation appconfig.ValidationReport `json:"validation"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || response.Header.Get("ETag") != `"config-draft-1"` || !created.Validation.Valid {
		t.Fatalf("create draft returned %d %q: %#v", response.StatusCode, response.Header.Get("ETag"), created)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/config/drafts/"+created.Draft.ID+"/actions/dry-run", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var checks struct {
		Items []appconfig.CheckResult `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&checks); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(checks.Items) != 2 || checks.Items[0].DraftVersion != 1 {
		t.Fatalf("dry run returned %d: %#v", response.StatusCode, checks)
	}

	action := func(path, etag, reason string) (*http.Response, []byte) {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(`{"reason":"`+reason+`"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", etag)
		result, actionErr := http.DefaultClient.Do(request)
		if actionErr != nil {
			t.Fatal(actionErr)
		}
		payload, _ := io.ReadAll(result.Body)
		result.Body.Close()
		return result, payload
	}
	response, payload := action("/api/v1/config/drafts/"+created.Draft.ID+"/actions/review", `"config-draft-1"`, "review exact draft")
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") != `"config-draft-2"` {
		t.Fatalf("review returned %d %q: %s", response.StatusCode, response.Header.Get("ETag"), payload)
	}
	response, payload = action("/api/v1/config/drafts/"+created.Draft.ID+"/actions/apply", `"config-draft-1"`, "stale apply")
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale apply returned %d: %s", response.StatusCode, payload)
	}
	response, payload = action("/api/v1/config/drafts/"+created.Draft.ID+"/actions/apply", `"config-draft-2"`, "apply exact draft")
	if response.StatusCode != http.StatusCreated || response.Header.Get("ETag") != `"config-scope-2"` {
		t.Fatalf("apply returned %d %q: %s", response.StatusCode, response.Header.Get("ETag"), payload)
	}
	current, err := store.CurrentConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var system appconfig.System
	if err := json.Unmarshal(current.After, &system); err != nil || system.Workflow.MaxReviewCycles != 4 {
		t.Fatalf("Increment 1 projection = %#v %v", system, err)
	}

	response, err = http.Get(server.URL + "/api/v1/config/registry-revisions?scope_kind=system")
	if err != nil {
		t.Fatal(err)
	}
	var revisions struct {
		Items []appconfig.RegistryRevision `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&revisions); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(revisions.Items) != 2 || revisions.Items[0].Operation != "apply" || revisions.Items[1].Operation != "import" {
		t.Fatalf("registry revisions = %#v", revisions.Items)
	}
	response, payload = action("/api/v1/config/registry-revisions/"+revisions.Items[1].ID+"/actions/rollback", `"config-scope-2"`, "restore imported values")
	if response.StatusCode != http.StatusCreated || response.Header.Get("ETag") != `"config-scope-3"` {
		t.Fatalf("rollback returned %d %q: %s", response.StatusCode, response.Header.Get("ETag"), payload)
	}
	current, _ = store.CurrentConfig(context.Background())
	_ = json.Unmarshal(current.After, &system)
	if system.Workflow.MaxReviewCycles != 2 {
		t.Fatalf("rollback did not restore Increment 1 effective behavior: %#v", system.Workflow)
	}
}

func TestConfigurationRegistryAPIExportsRedactedAndPreviewsUnknownImport(t *testing.T) {
	server, _ := testServer(t)
	response, err := http.Get(server.URL + "/api/v1/config/export?scope_kind=system")
	if err != nil {
		t.Fatal(err)
	}
	var document appconfig.DeclarativeConfig
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") != `"config-scope-1"` || appconfig.VerifyDeclarativeConfig(document) != nil {
		t.Fatalf("configuration export returned %d %q: %#v", response.StatusCode, response.Header.Get("ETag"), document)
	}
	document.Values = append(document.Values, appconfig.ImportValue{Key: "future.setting", Value: json.RawMessage(`{"opaque":true}`), Configured: true})
	document.DocumentHash, err = appconfig.ComputeDeclarativeConfigHash(document)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"mode": "forward_compatible", "target": appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, "reason": "API import fixture", "document": document,
	})
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/config/import/preview", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"config-scope-1"`)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var preview appconfig.ImportPreview
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !preview.Valid || len(preview.PreservedUnknown) != 1 {
		t.Fatalf("import preview returned %d: %#v", response.StatusCode, preview)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/config/import", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"config-scope-1"`)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var imported struct {
		Draft   appconfig.Draft         `json:"draft"`
		Preview appconfig.ImportPreview `json:"preview"`
	}
	if err := json.NewDecoder(response.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || imported.Draft.Operation != "import" || len(imported.Draft.UnknownEntries) != 1 {
		t.Fatalf("import draft returned %d: %#v", response.StatusCode, imported)
	}
}

type rejectingWebhookValidator struct{}

func (rejectingWebhookValidator) ValidateWebhook(context.Context, gitbridge.WebhookValidationRequest) (gitbridge.PullRequestEvent, error) {
	return gitbridge.PullRequestEvent{}, errors.New("fixture signature rejected")
}

func TestGitHubWebhookIsPublicButFailsClosedAtIsolatedValidator(t *testing.T) {
	store, err := storesqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	authService, err := maintainerauth.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock",
		WithAuthentication(authService, false), WithGitHubWebhookValidator(rejectingWebhookValidator{})))
	t.Cleanup(server.Close)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/github/webhooks", strings.NewReader(`{"action":"closed"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Delivery", "delivery-fixture")
	request.Header.Set("X-GitHub-Event", "pull_request")
	request.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid public webhook returned %d", response.StatusCode)
	}
}

func TestNotificationInboxListsAndAcknowledgesDeliveredEvents(t *testing.T) {
	server, store := testServer(t)
	job, err := store.CreateJob(context.Background(), storage.CreateJobParams{
		ProjectID: "owner-repo", Repository: "owner/repo", Task: "fixture", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionJob(context.Background(), job.ID, jobs.TransitionRequest{
		To: jobs.StateFailed, ActorID: "controller", Reason: "fixture", ExpectedVersion: job.Version,
	}); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/v1/notifications?state=delivered")
	if err != nil {
		t.Fatal(err)
	}
	var inbox struct {
		Items []struct{ ID, State, Kind string } `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&inbox); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(inbox.Items) != 1 || inbox.Items[0].Kind != "job_failed" {
		t.Fatalf("notification inbox returned %d: %#v", response.StatusCode, inbox)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/notifications/"+inbox.Items[0].ID+"/actions/read", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&read); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || read.State != "read" {
		t.Fatalf("notification acknowledgement returned %d: %#v", response.StatusCode, read)
	}
}

func TestProjectMemoryAPIIsScopedQuarantinedAndTraceable(t *testing.T) {
	server, store := testServer(t)
	if _, err := store.UpsertProject(context.Background(), projects.UpsertRequest{
		ID: "other-repo", Provider: "local", Repository: "other/repo", DefaultBranch: "main", LocalRemoteName: "other.git",
	}, "test-admin"); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory", strings.NewReader(
		`{"content":"Run go test -race ./... before publication.","kind":"verified_case","source_uri":"job://job_fixture/final-report","base_commit":"0123456789abcdef0123456789abcdef01234567","affected_paths":["go.mod"]}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Version   int64  `json:"version"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || record.ID == "" || record.Status != "quarantine" || record.Namespace != "viking://resources/projects/owner/repo/" {
		t.Fatalf("unexpected memory candidate: status=%d record=%+v", response.StatusCode, record)
	}

	response, err = http.Get(server.URL + "/api/v1/projects/other-repo/memory/" + record.ID)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-project memory lookup returned %d", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/v1/projects/owner-repo/memory?q=publication")
	if err != nil {
		t.Fatal(err)
	}
	var before struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&before); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(before.Items) != 0 {
		t.Fatalf("quarantined memory was retrieved: %s", before.Items)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/"+record.ID+"/actions/promote", strings.NewReader(
		`{"rationale":"reviewed against repository evidence","basis":"human_approval","expected_version":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Maintainer-Actor", "reviewer")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("promotion returned %d", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/v1/projects/owner-repo/memory?q=publication")
	if err != nil {
		t.Fatal(err)
	}
	var search struct {
		Items     []json.RawMessage `json:"items"`
		Retrieval struct {
			CandidateIDs    []string `json:"candidate_ids"`
			SelectedIDs     []string `json:"selected_ids"`
			BudgetTokens    int      `json:"budget_tokens"`
			AllocatedTokens int      `json:"allocated_tokens"`
		} `json:"retrieval"`
	}
	if err := json.NewDecoder(response.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(search.Items) != 1 || len(search.Retrieval.SelectedIDs) != 1 || search.Retrieval.BudgetTokens != memoryRetrievalBudget || search.Retrieval.AllocatedTokens <= 0 {
		t.Fatalf("unexpected bounded search: %+v", search)
	}

	response, err = http.Get(server.URL + "/api/v1/projects/other-repo/memory/retrievals")
	if err != nil {
		t.Fatal(err)
	}
	var otherTraces struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&otherTraces); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(otherTraces.Items) != 0 {
		t.Fatalf("cross-project retrieval trace leaked: %s", otherTraces.Items)
	}
}

func TestProjectMemoryExportAndRestoreAPI(t *testing.T) {
	server, store := testServer(t)
	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	record, err := store.PutCandidate(context.Background(), scope, memory.Record{
		Content: "The project uses an offline deterministic verification gate.", Kind: "project_knowledge",
		SourceURI: "job://controller/job_export", BaseCommit: strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/v1/projects/owner-repo/memory/export")
	if err != nil {
		t.Fatal(err)
	}
	var bundle memory.ExportBundle
	if err := json.NewDecoder(response.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || bundle.ManifestHash == "" || len(bundle.Records) != 1 {
		t.Fatalf("export returned %d: %+v", response.StatusCode, bundle)
	}
	if err := store.DeleteMemory(context.Background(), scope, record.ID, memory.DeletionRequest{ActorID: "admin", Rationale: "API restore fixture", ExpectedVersion: record.Version}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"dry_run": true, "bundle": bundle})
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/restore", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var dryRun memory.RestoreReport
	if err := json.NewDecoder(response.Body).Decode(&dryRun); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !dryRun.DryRun || dryRun.Imported != 1 {
		t.Fatalf("dry run returned %d: %+v", response.StatusCode, dryRun)
	}
	payload, _ = json.Marshal(map[string]any{"dry_run": false, "bundle": bundle})
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/restore", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var restored memory.RestoreReport
	if err := json.NewDecoder(response.Body).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || restored.Imported != 1 {
		t.Fatalf("restore returned %d: %+v", response.StatusCode, restored)
	}
}

func TestMemoryRoutePermissionsSeparateReviewFromAdministration(t *testing.T) {
	cases := map[string]maintainerauth.Permission{
		"GET /api/v1/projects/p/memory":                       maintainerauth.PermissionRead,
		"GET /api/v1/projects/p/memory/export":                maintainerauth.PermissionAdminister,
		"POST /api/v1/projects/p/memory":                      maintainerauth.PermissionAdminister,
		"POST /api/v1/projects/p/memory/actions/restore":      maintainerauth.PermissionAdminister,
		"POST /api/v1/projects/p/memory/m/actions/promote":    maintainerauth.PermissionReview,
		"POST /api/v1/projects/p/memory/m/actions/correct":    maintainerauth.PermissionReview,
		"POST /api/v1/projects/p/memory/m/actions/invalidate": maintainerauth.PermissionReview,
		"POST /api/v1/projects/p/memory/m/actions/delete":     maintainerauth.PermissionAdminister,
	}
	for description, expected := range cases {
		parts := strings.SplitN(description, " ", 2)
		request := httptest.NewRequest(parts[0], parts[1], nil)
		if actual := routePermission(request); actual != expected {
			t.Fatalf("%s permission = %s, want %s", description, actual, expected)
		}
	}
}

func TestProjectMemoryReindexQueuesCanonicalRecords(t *testing.T) {
	_, store := testServer(t)
	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	record, err := store.PutCandidate(context.Background(), scope, memory.Record{Content: "rebuild this record", Kind: "pattern"})
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.PromoteMemory(context.Background(), scope, record.ID, memory.PromotionRequest{
		ActorID: "reviewer", Rationale: "reviewed", Basis: "human_approval", ExpectedVersion: record.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	index := memory.NewFakeIndex()
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", WithMemoryIndex(index)))
	t.Cleanup(server.Close)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/reindex", strings.NewReader(`{"mode":"vectors_only"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status        string `json:"status"`
		RecordsQueued int    `json:"records_queued"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted || result.Status != "queued" || result.RecordsQueued != 1 {
		t.Fatalf("reindex returned %d: %+v", response.StatusCode, result)
	}
	operation, err := store.ClaimMemoryIndexOperation(context.Background(), "test-indexer", 30*time.Second)
	if err != nil || operation.RecordID != record.ID || operation.Action != "upsert" {
		t.Fatalf("rebuild operation = %+v, %v", operation, err)
	}
}

func TestConfiguredMemoryIndexSmokeEndpointIsScopedAndSelfCleaning(t *testing.T) {
	_, store := testServer(t)
	index := memory.NewFakeIndex()
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", WithMemoryIndex(index)))
	t.Cleanup(server.Close)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/smoke-index", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var result memory.SmokeResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || result.Status != "passed" || !result.SearchSeen || result.Namespace != "viking://resources/projects/owner/repo/" {
		t.Fatalf("smoke endpoint returned %d: %#v", response.StatusCode, result)
	}
	if matches, err := index.Find(context.Background(), memory.ProjectScope{Owner: "owner", Repository: "repo"}, "bounded OpenViking smoke marker", 10); err != nil || len(matches) != 0 {
		t.Fatalf("smoke marker remained indexed: %#v, %v", matches, err)
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

	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	if _, err := store.PutCandidate(context.Background(), scope, memory.Record{Content: "authenticated restore fixture", Kind: "pattern"}); err != nil {
		t.Fatal(err)
	}
	bundle, err := store.ExportProjectMemory(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	restorePayload, _ := json.Marshal(map[string]any{"dry_run": false, "bundle": bundle})
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/restore", bytes.NewReader(restorePayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("restore without recent reauthentication returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/reauthenticate", strings.NewReader(`{"password":"correct horse battery staple"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reauthentication returned %d", response.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects/owner-repo/memory/actions/restore", bytes.NewReader(restorePayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(sessionCookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("recently reauthenticated restore returned %d", response.StatusCode)
	}
}

func TestHermesBoundaryRequiresServiceTokenAndCannotApproveOrActivate(t *testing.T) {
	_, store := testServer(t)
	token := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", WithHermesToken(token)))
	t.Cleanup(server.Close)

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/hermes/tools/jobs/submit", strings.NewReader(`{"project_id":"owner-repo","task":"inspect the registered repository"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Hermes request returned %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/hermes/tools/jobs/submit", strings.NewReader(`{"project_id":"owner-repo","task":"inspect the registered repository"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(token))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || job.ID == "" {
		t.Fatalf("Hermes submit returned %d: %+v", response.StatusCode, job)
	}
	transitions, err := store.ListTransitions(context.Background(), job.ID, 0, 10)
	if err != nil || len(transitions) != 1 || transitions[0].ActorID != "hermes-service" {
		t.Fatalf("Hermes actor was not server-derived: %+v, %v", transitions, err)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/hermes/tools/jobs/"+job.ID+"/request-publication-approval", strings.NewReader(`{"rationale":"ready for an operator decision"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(token))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("publication request returned %d", response.StatusCode)
	}
	approvals, err := store.ListApprovals(context.Background(), job.ID)
	if err != nil || len(approvals) != 0 {
		t.Fatalf("Hermes gained publication authority: %+v, %v", approvals, err)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/hermes/tools/skill-proposals", strings.NewReader(
		`{"name":"report-summary","description":"Summarize final reports.","content":"# Report summary\n\nUse only controller-provided evidence."}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(token))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Status    string `json:"status"`
		Activated bool   `json:"activated"`
	}
	if err := json.NewDecoder(response.Body).Decode(&proposal); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || proposal.Status != "proposed" || proposal.Activated {
		t.Fatalf("Hermes skill proposal gained authority: status=%d proposal=%+v", response.StatusCode, proposal)
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
		Transitions           []any                 `json:"transitions"`
		ConfigurationSnapshot appconfig.JobSnapshot `json:"configuration_snapshot"`
	}
	if err := json.NewDecoder(response.Body).Decode(&inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Job.State != "queued" || inspected.Job.Version != 3 || len(inspected.Transitions) != 3 ||
		inspected.ConfigurationSnapshot.JobID != created.ID || len(inspected.ConfigurationSnapshot.Document) == 0 {
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
	request, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/projects/second", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("project disable returned %d", response.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs", strings.NewReader(
		`{"project_id":"second","repository":"fixture/second","task":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("disabled project job returned %d", response.StatusCode)
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

func TestModelLoadBenchmarkAndUnloadUseAllowListedProfiles(t *testing.T) {
	store, err := storesqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := models.NewFake([]models.Profile{{ID: "implementation", Role: "implementation", ModelFamily: "fixture", Context: 4096, Quantization: "fake"}})
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", WithModelManager(manager)))
	t.Cleanup(server.Close)
	for _, path := range []string{"/api/v1/models/implementation/actions/load", "/api/v1/models/implementation/actions/benchmark", "/api/v1/models/actions/unload"} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+path, nil)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("POST %s returned %d: %s", path, response.StatusCode, body)
		}
	}
	status, _ := manager.Status(context.Background())
	if status.State != "unloaded" {
		t.Fatalf("status after unload = %#v", status)
	}
}

func TestFindingActionsEnforceLifecycleAndRetainRationale(t *testing.T) {
	server, store := testServer(t)
	job, err := store.CreateJob(context.Background(), storage.CreateJobParams{ID: "job_finding_api", ProjectID: "owner-repo", Repository: "owner/repo", Task: "review", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ObserveFindings(context.Background(), job.ID, 0, []agents.Finding{{
		ID: "QC-API-1", Severity: "should_fix", Category: "correctness", Claim: "fixture claim",
		Location: agents.Location{Path: "main.go", Line: 1}, Evidence: "fixture evidence",
		RequiredResolution: "fix it", VerificationMethod: "run fixture test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	version := records[0].Version
	for _, action := range []string{"dispute", "accept"} {
		payload := fmt.Sprintf(`{"rationale":"reviewed fixture evidence","expected_version":%d}`, version)
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs/"+job.ID+"/findings/QC-API-1/actions/"+action, strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		var updated struct {
			Version int64  `json:"version"`
			Status  string `json:"status"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&updated)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil {
			t.Fatalf("%s returned %d: %#v (%v)", action, response.StatusCode, updated, decodeErr)
		}
		version = updated.Version
	}
	payload := fmt.Sprintf(`{"rationale":"needs operator attention","expected_version":%d}`, version)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/jobs/"+job.ID+"/findings/QC-API-1/actions/escalate", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("escalate returned %d", response.StatusCode)
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
