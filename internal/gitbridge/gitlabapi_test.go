package gitbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitLabForgeDiagnosticsAndIdempotentDraftMergeRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "fixture-gitlab-token-value" {
			t.Errorf("missing private token")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/owner%2Frepo":
			json.NewEncoder(w).Encode(map[string]any{"path_with_namespace": "owner/repo", "default_branch": "main", "archived": false, "permissions": map[string]any{"project_access": map[string]int{"access_level": 30}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/owner%2Frepo/merge_requests":
			calls++
			json.NewEncoder(w).Encode([]map[string]any{{"iid": 7, "id": 77, "web_url": "https://gitlab.invalid/owner/repo/-/merge_requests/7", "state": "opened", "draft": true, "source_branch": "maintainer/job-one", "target_branch": "main", "sha": strings.Repeat("a", 40), "description": "result\n<!-- maintainer-operation:job-one_publication -->"}})
		default:
			t.Errorf("unexpected GitLab request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	configuration, err := NewGitLabConfiguration(server.URL, server.URL, StaticGitLabToken("fixture-gitlab-token-value"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api, err := configuration.api("owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := api.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Ready || !diagnostics.CanRead || !diagnostics.CanPublish {
		t.Fatalf("diagnostics %#v", diagnostics)
	}
	merge, err := api.CreateDraftMergeRequest(context.Background(), DraftPullRequestRequest{IdempotencyKey: "job-one_publication", Branch: "maintainer/job-one", Base: "main", Title: "verified repair", Body: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if merge.IID != 7 || calls != 1 {
		t.Fatalf("merge %#v calls=%d", merge, calls)
	}
}

func TestGitLabForgeAdmissionAndRemoteNeverExposeToken(t *testing.T) {
	if _, err := NewGitLabConfiguration("http://gitlab.example.com", "http://gitlab.example.com", StaticGitLabToken("fixture-gitlab-token-value"), nil); err == nil {
		t.Fatal("non-TLS non-loopback GitLab endpoint accepted")
	}
	configuration, err := NewGitLabConfiguration("https://gitlab.example.com", "https://gitlab.example.com", StaticGitLabToken("fixture-gitlab-token-value"), nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := configuration.remote("owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "https://gitlab.example.com/owner/repo.git" || strings.Contains(remote, "fixture-gitlab-token-value") {
		t.Fatalf("unsafe remote %q", remote)
	}
}
