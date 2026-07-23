package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestDocumentationManifestsAreAppendOnlyAndJobScoped(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_docs_storage", ProjectID: "project-doc", Repository: "owner/repo", Task: "document api change", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := documentation.Manifest{
		JobID: job.ID, ProjectID: job.ProjectID, SchemaVersion: 1,
		ContractSHA256: strings.Repeat("a", 64), RiskLevel: "medium", ResultSHA: strings.Repeat("b", 40),
		SourceContext: "documentation_agent_context_v1", PolicyVersion: "documentation-policy-v1",
		Requirements: []documentation.Requirement{{
			ID: "DOC-API", Document: "docs/api.md", Reason: "public API changed", Source: "candidate_diff",
			RenderTargets: []string{"markdown"}, Required: true, Status: "satisfied",
		}},
		Changes: []documentation.Change{{
			Path: "docs/api.md", Action: "updated", PolicyRule: "public-api-documentation",
			SourceOfTruth: "internal/api/openapi.yaml", LinkedEvidenceIDs: []string{"candidate_diff"},
		}},
		Checks: []documentation.Check{{
			ID: "DOC-CHECK-API", Kind: "schema", Target: "internal/api/openapi.yaml", Status: "passed",
			Summary: "OpenAPI source of truth was linked",
		}},
		UnsupportedClaims: []documentation.UnsupportedClaim{}, Edits: []documentation.Edit{},
		Status: "changes_applied", PolicySummary: "documentation policy required API docs evidence",
	}
	saved, err := store.SaveDocumentationManifest(ctx, manifest)
	if err != nil || saved.ID == "" {
		t.Fatalf("save manifest = %#v, %v", saved, err)
	}
	items, err := store.ListDocumentationManifests(ctx, job.ID, 10)
	if err != nil || len(items) != 1 || items[0].Changes[0].Path != "docs/api.md" {
		t.Fatalf("list manifests = %#v, %v", items, err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE documentation_manifests SET status='blocked' WHERE id=?", saved.ID); err == nil {
		t.Fatal("documentation manifest update unexpectedly succeeded")
	}
	if _, err := store.SaveDocumentationManifest(ctx, documentation.Manifest{JobID: job.ID}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("invalid manifest error = %v", err)
	}
}
