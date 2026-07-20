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
