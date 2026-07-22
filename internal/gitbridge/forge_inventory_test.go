package gitbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitHubInventoryRetriesPaginatesNormalizesAndReportsUnsupported(t *testing.T) {
	var issueCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-installation-token" {
			t.Errorf("missing GitHub authorization")
		}
		w.Header().Set("X-RateLimit-Remaining", "42")
		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			if issueCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<`+serverURL(r)+`?page=2&per_page=10>; rel="next"`)
				writeJSON(w, http.StatusOK, []any{map[string]any{"id": 101, "number": 7, "title": "untrusted issue", "state": "open", "html_url": "https://example.test/issues/7", "updated_at": "2026-07-22T01:00:00Z"}})
				return
			}
			writeJSON(w, http.StatusOK, []any{map[string]any{"id": 102, "number": 8, "title": "PR-shaped issue", "pull_request": map[string]any{"url": "ignored"}}})
		case "/repos/owner/repo/pulls":
			writeJSON(w, http.StatusOK, []any{map[string]any{"id": 201, "number": 9, "title": "draft repair", "state": "open", "html_url": "https://example.test/pulls/9", "head": map[string]any{"ref": "maintainer/job", "sha": strings.Repeat("a", 40)}}})
		case "/repos/owner/repo/discussions":
			w.WriteHeader(http.StatusForbidden)
		case "/repos/owner/repo/actions/runs":
			writeJSON(w, http.StatusOK, map[string]any{"workflow_runs": []any{map[string]any{"id": 301, "name": "CI", "status": "completed", "head_sha": strings.Repeat("b", 40), "html_url": "https://example.test/runs/301"}}})
		case "/repos/owner/repo/actions/artifacts":
			writeJSON(w, http.StatusOK, map[string]any{"artifacts": []any{map[string]any{"id": 401, "name": "evidence", "expired": false, "archive_download_url": "https://example.test/artifacts/401"}}})
		default:
			writeJSON(w, http.StatusOK, []any{})
		}
	}))
	defer server.Close()

	api, err := NewGitHubAPI(server.URL, "owner", "repo", fixedInstallationToken{"fixture-installation-token"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	first, err := api.inventory(context.Background(), "project-one", 0, 1, 10)
	if err != nil || first.Done || first.Endpoint != 0 || first.Page != 2 || len(first.Objects) != 1 || issueCalls.Load() != 2 {
		t.Fatalf("first inventory = %#v calls=%d err=%v", first, issueCalls.Load(), err)
	}
	second, err := api.inventory(context.Background(), "project-one", first.Endpoint, first.Page, 20)
	if err != nil || !second.Done || second.RateLimitRemaining != 42 {
		t.Fatalf("second inventory = %#v err=%v", second, err)
	}
	kinds := map[string]int{}
	for _, object := range second.Objects {
		kinds[object.Kind]++
		if object.ProjectID != "project-one" || object.Provider != "github" || !json.Valid(object.ProviderMetadata) {
			t.Fatalf("invalid normalized object %#v", object)
		}
	}
	if kinds["issue"] != 0 || kinds["change_request"] != 1 || kinds["pipeline"] != 1 || kinds["artifact"] != 1 {
		t.Fatalf("normalized kinds = %#v", kinds)
	}
	if len(second.Unsupported) != 1 || !strings.HasPrefix(second.Unsupported[0], "discussion:") {
		t.Fatalf("unsupported = %#v", second.Unsupported)
	}
}

func TestGitLabInventoryNormalizesJobsAndArtifactMetadataAndRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "fixture-gitlab-token-value" {
			t.Errorf("missing GitLab token")
		}
		if r.URL.Path == "/api/v4/projects/owner%2Frepo/jobs" {
			writeJSON(w, http.StatusOK, []any{map[string]any{
				"id": 77, "name": "test", "status": "success", "web_url": "https://gitlab.test/jobs/77",
				"pipeline": map[string]any{"id": 55}, "artifacts_file": map[string]any{"filename": "evidence.zip", "size": 1234},
			}})
			return
		}
		if r.URL.Path == "/api/v4/projects/owner%2Frepo/issues" {
			w.Header().Set("Retry-After", "17")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, http.StatusOK, []any{})
	}))
	defer server.Close()
	configuration, err := NewGitLabConfiguration(server.URL, server.URL, StaticGitLabToken("fixture-gitlab-token-value"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api, _ := configuration.api("owner/repo")
	rateLimited, err := api.inventory(context.Background(), "project-one", 0, 1, 20)
	if err != nil || rateLimited.RetryAfterSeconds != 17 || rateLimited.Endpoint != 0 || rateLimited.Page != 1 {
		t.Fatalf("rate-limited inventory = %#v err=%v", rateLimited, err)
	}
	result, err := api.inventory(context.Background(), "project-one", 3, 1, 20)
	if err != nil || !result.Done || len(result.Objects) != 2 {
		t.Fatalf("job inventory = %#v err=%v", result, err)
	}
	if result.Objects[0].Kind != "job" || result.Objects[1].Kind != "artifact" || result.Objects[1].ParentID != "77" {
		t.Fatalf("job and artifact normalization = %#v", result.Objects)
	}
}

func TestForgeCursorRejectsMalformedOrOversizedState(t *testing.T) {
	cursor := forgeCursor{BaseSHA: strings.Repeat("a", 40), Endpoint: 4, Page: 3}
	encoded := encodeForgeCursor(cursor)
	decoded, ok := decodeForgeCursor(encoded)
	if !ok || decoded != cursor {
		t.Fatalf("cursor round trip = %#v ok=%v", decoded, ok)
	}
	for index, value := range []string{"forge-v1:not-base64", forgeCursorPrefix + strings.Repeat("x", 5000), forgeCursorPrefix + strconv.Itoa(7)} {
		if _, valid := decodeForgeCursor(value); valid {
			t.Fatalf("malformed cursor %d accepted", index)
		}
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host + r.URL.Path
}
