package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadJSONRejectsBytesBeyondLimit(t *testing.T) {
	payload := `"` + strings.Repeat("a", maxConfigDocumentBytes-2) + `" `
	if _, err := readJSON(strings.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized valid-prefix JSON returned %v", err)
	}
}

func TestParseConfigScopeRejectsAmbiguousAndUnsafeScopes(t *testing.T) {
	for value, wantKind := range map[string]string{
		"system": "system", "project:owner-repo": "project", "job_template:nightly": "job_template",
	} {
		kind, _, err := parseConfigScope(value)
		if err != nil || kind != wantKind {
			t.Errorf("parse %q = %q, %v", value, kind, err)
		}
	}
	for _, value := range []string{"built_in", "system:one", "project", "unknown:value", "project:"} {
		if _, _, err := parseConfigScope(value); err == nil {
			t.Errorf("unsafe scope %q was accepted", value)
		}
	}
}

func TestConfigDraftCreateSendsTypedBodyAndExactETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/config/drafts" || r.Header.Get("If-Match") != `"config-scope-7"` {
			t.Errorf("unexpected request %s %s If-Match=%q", r.Method, r.URL.Path, r.Header.Get("If-Match"))
		}
		payload, _ := io.ReadAll(r.Body)
		var body struct {
			Scope struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"scope"`
			Reason  string           `json:"reason"`
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Error(err)
		}
		if body.Scope.Kind != "project" || body.Scope.ID != "owner-repo" || body.Reason != "CLI parity" || len(body.Entries) != 1 {
			t.Errorf("unexpected draft body: %s", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draft":{"id":"configdraft_0123456789abcdef0123456789abcdef"}}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "entries.json")
	if err := os.WriteFile(path, []byte(`[{"key":"workflow.max_review_cycles","value":4,"configured":true,"reset":false,"secret":false}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.config([]string{"draft", "create", "--scope", "project:owner-repo", "--scope-version", "7", "--reason", "CLI parity", path}); err != nil {
		t.Fatal(err)
	}
}

func TestReadJSONAcceptsBoundedDocument(t *testing.T) {
	payload, err := readJSON(strings.NewReader(`{"schema_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"schema_version":1}` {
		t.Fatalf("unexpected payload %s", payload)
	}
}

func TestIntelligenceQueryUsesSharedBoundedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/intelligence/query" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["term"] != "Target" || body["limit"] != float64(100) {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","revision":"abc","term":"Target","symbols":[],"relations":[],"partial":false,"failures":0}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"query", "project-one", "Target"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntelligenceRebuildRequiresAndSendsAuditedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/intelligence/actions/rebuild" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["reason"] != "replace stale derived index" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"cleared","next_action":"refresh","reason":"replace stale derived index"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"rebuild", "project-one"}); err == nil {
		t.Fatal("rebuild accepted without audited reason")
	}
	if err := client.intelligence([]string{"rebuild", "project-one", "--reason", "replace stale derived index"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntelligenceCacheSimulationUsesTypedBoundedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/caches/actions/simulate" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["kind"] != "source-parse" || body["trust_domain"] != "trusted" || body["estimated_bytes"] != float64(4096) {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","trust_domain":"trusted","kind":"source-parse","current_bytes":0,"estimated_bytes":4096,"quota_bytes":536870912,"would_fit":true,"explanation":"fits"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"simulate-cache", "project-one", "--kind", "source-parse", "--trust-domain", "trusted", "--estimated-bytes", "4096"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntelligenceCorrectionUsesTypedAuditedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/project-one/differentials/differential-one/actions/correct" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["observation_kind"] != "test" || body["observation_key"] != "unit" || body["after_classification"] != "indeterminate" || body["reason"] != "evidence incomplete" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"correction_one"}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.intelligence([]string{"correct-differential", "project-one", "differential-one", "--kind", "test", "--key", "unit", "--classification", "indeterminate", "--reason", "evidence incomplete"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackInstallUsesVersionRevisionAndAuditedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/capability-packs/php83-intranet/actions/install" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["target_version"] != "1.0.0" || body["expected_revision"] != float64(0) || body["reason"] != "reviewed trusted catalog" {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"installation":{"pack_id":"php83-intranet","pack_version":"1.0.0"},"event":{"action":"install"}}`))
	}))
	defer server.Close()
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.pack([]string{"install", "--version", "1.0.0", "--revision", "0", "--reason", "reviewed trusted catalog", "php83-intranet"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackConfigurationUsesTypedSharedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/projects/project-one/capability-packs/r-statistical-validation/configuration" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		config, _ := body["config"].(map[string]any)
		tolerance, _ := config["tolerance"].(map[string]any)
		if body["expected_revision"] != float64(3) || body["reason"] != "reviewed statistical policy" || tolerance["absolute"] != 0.02 {
			t.Errorf("unexpected body %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"project-one","pack_id":"r-statistical-validation","pack_version":"1.0.0","enabled":true,"config":{},"revision":4,"updated_at":"2026-07-22T00:00:00Z"}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "r-config.json")
	if err := os.WriteFile(configPath, []byte(`{"tolerance":{"absolute":0.02}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := client.pack([]string{"configure", "--revision", "3", "--config", configPath, "--reason", "reviewed statistical policy", "project-one", "r-statistical-validation"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackConfigurationPreservesDuplicateKeysForControllerRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.Count(string(body), `"absolute"`) != 2 {
			t.Errorf("CLI rewrote duplicate configuration keys before controller validation: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"duplicate configuration key"}}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "duplicate-config.json")
	if err := os.WriteFile(configPath, []byte(`{"tolerance":{"absolute":0.01,"absolute":0.02}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := client{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	err := client.pack([]string{"configure-preview", "--revision", "1", "--config", configPath, "--reason", "validate exact bytes", "project-one", "r-statistical-validation"})
	if err == nil || !strings.Contains(err.Error(), "duplicate configuration key") {
		t.Fatalf("controller rejection = %v", err)
	}
}
