package memory

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

func TestFakeIndexFiltersBeforeRanking(t *testing.T) {
	index := NewFakeIndex()
	alpha := ProjectScope{Owner: "owner", Repository: "alpha"}
	beta := ProjectScope{Owner: "owner", Repository: "beta"}
	for _, record := range []Record{
		{ID: "memory_0123456789abcdef0123456789abcdef", Scope: alpha, Namespace: alpha.Namespace(), Status: StatusCanonical, Content: "shared verification phrase"},
		{ID: "memory_abcdef0123456789abcdef0123456789", Scope: beta, Namespace: beta.Namespace(), Status: StatusCanonical, Content: "shared verification phrase"},
	} {
		if err := index.Upsert(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := index.Find(context.Background(), alpha, "verification", 10)
	if err != nil || len(matches) != 1 || matches[0].RecordID != "memory_0123456789abcdef0123456789abcdef" {
		t.Fatalf("project-before-ranking failed: %+v, %v", matches, err)
	}
}

func TestOpenVikingIndexUsesFixedScopedRoutesAndFileSecret(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "openviking-api-key")
	if err := os.WriteFile(keyPath, []byte("test-openviking-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := ProjectScope{Owner: "owner", Repository: "repo"}
	recordID := "memory_0123456789abcdef0123456789abcdef"
	writeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-openviking-key" || r.Header.Get("X-OpenViking-Account") != "maintainer" || r.Header.Get("X-OpenViking-User") != "controller" {
			t.Errorf("request lacked bounded authentication headers")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "healthy": true})
		case "POST /api/v1/content/write":
			writeCalls++
			var body struct {
				URI     string `json:"uri"`
				Content string `json:"content"`
				Mode    string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.URI != scope.Namespace()+"records/"+recordID+".md" || !strings.Contains(body.Content, `"content":"verified command"`) {
				t.Errorf("unsafe write body: %+v", body)
			}
			if writeCalls == 1 && body.Mode == "create" {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": map[string]string{"code": "ALREADY_EXISTS"}})
				return
			}
			if body.Mode != "replace" {
				t.Errorf("second write mode = %q", body.Mode)
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "result": map[string]any{"uri": body.URI}})
		case "POST /api/v1/search/find":
			var body struct {
				TargetURI string `json:"target_uri"`
				NodeLimit int    `json:"node_limit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.TargetURI != scope.Namespace()+"records/" || body.NodeLimit != 5 {
				t.Errorf("search was not pre-filtered: %+v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "result": map[string]any{"resources": []map[string]any{
				{"uri": scope.Namespace() + "records/" + recordID + ".md", "score": 0.9, "abstract": "verified"},
				{"uri": "viking://resources/projects/other/repo/records/memory_abcdef0123456789abcdef0123456789.md", "score": 1.0},
			}}})
		case "DELETE /api/v1/fs":
			if r.URL.Query().Get("uri") != scope.Namespace()+"records/"+recordID+".md" {
				t.Errorf("unexpected forget URI: %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": map[string]string{"code": "NOT_FOUND"}})
		case "POST /api/v1/content/reindex":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "result": map[string]string{"status": "accepted"}})
		default:
			t.Errorf("unexpected OpenViking route: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	index, err := NewOpenVikingIndex(server.URL, keyPath, "maintainer", "controller")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := index.Health(ctx); err != nil {
		t.Fatal(err)
	}
	record := Record{
		ID: recordID, Scope: scope, Namespace: scope.Namespace(), Status: StatusCanonical,
		Content: "verified command", ContentHash: strings.Repeat("a", 64), AffectedPaths: []string{"go.mod"},
	}
	if err := index.Upsert(ctx, record); err != nil {
		t.Fatal(err)
	}
	if writeCalls != 2 {
		t.Fatalf("idempotent create/replace calls = %d", writeCalls)
	}
	matches, err := index.Find(ctx, scope, "verified", 5)
	if err != nil || len(matches) != 1 || matches[0].RecordID != recordID {
		t.Fatalf("scoped results = %+v, %v", matches, err)
	}
	if err := index.Forget(ctx, scope, recordID); err != nil {
		t.Fatal(err)
	}
	if err := index.Reindex(ctx, scope, "vectors_only"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenVikingIndexRejectsAuthorityBearingBaseURLs(t *testing.T) {
	for _, rawURL := range []string{"http://user:pass@openviking:1933", "http://openviking:1933/api", "file:///etc/passwd", "http://openviking:1933?target=other"} {
		if _, err := NewOpenVikingIndex(rawURL, "", "", ""); err == nil {
			t.Fatalf("accepted unsafe base URL %q", rawURL)
		}
	}
}
