package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

func TestEvaluationAPIStoresDatasetAndIsolatedRun(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock"))
	defer server.Close()
	body := `{"project_id":"owner-repo","name":"historical fixtures","source_kind":"historical_range","repository":"owner/repo","base_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","target_revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","known_patch_sha256":"` + strings.Repeat("c", 64) + `","exclusions":["vendor/**"]}`
	response, err := http.Post(server.URL+"/api/v1/evaluations/datasets", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("dataset status = %d", response.StatusCode)
	}
	var created struct {
		Dataset struct {
			ID string `json:"id"`
		} `json:"dataset"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	runBody := `{"dataset_id":"` + created.Dataset.ID + `","profile_matrix":["local","remote-fake"],"budget_seconds":3600,"concurrency":2}`
	runResponse, err := http.Post(server.URL+"/api/v1/evaluations/runs", "application/json", bytes.NewBufferString(runBody))
	if err != nil {
		t.Fatal(err)
	}
	defer runResponse.Body.Close()
	if runResponse.StatusCode != http.StatusCreated {
		t.Fatalf("run status = %d", runResponse.StatusCode)
	}
	var launched struct {
		Run struct {
			IsolatedMemoryNamespace string `json:"isolated_memory_namespace"`
			PromotionRecommendation string `json:"promotion_recommendation"`
			Results                 []any  `json:"results"`
		} `json:"run"`
	}
	if err := json.NewDecoder(runResponse.Body).Decode(&launched); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(launched.Run.IsolatedMemoryNamespace, "eval://memory/") || launched.Run.PromotionRecommendation != "review_only" || len(launched.Run.Results) != 2 {
		t.Fatalf("unexpected launched run: %#v", launched.Run)
	}
}
