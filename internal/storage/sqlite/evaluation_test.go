package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/evaluation"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestEvaluationDatasetsAndRunsPersistAppendOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	dataset, err := store.CreateEvaluationDataset(ctx, evaluation.NewDataset(evaluation.CreateDatasetRequest{
		ProjectID: "owner-repo", Name: "historical fixtures", SourceKind: evaluation.SourceHistoricalRange,
		Repository: "owner/repo", BaseRevision: strings.Repeat("a", 40), TargetRevision: strings.Repeat("b", 40),
		KnownPatchSHA256: strings.Repeat("c", 64), Exclusions: []string{"vendor/**", "dist/**"},
	}, "operator", time.Unix(1, 0).UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if dataset.ID == "" || dataset.HiddenPatchSHA256 == dataset.KnownPatchSHA256 || dataset.ReproducibilityKey == "" {
		t.Fatalf("dataset did not retain hidden/reproducible identity: %#v", dataset)
	}
	run, err := evaluation.SimulateRun(dataset, evaluation.LaunchRunRequest{ProfileMatrix: []string{"local", "remote-fake"}, BudgetSeconds: 3600, Concurrency: 2}, "operator", "evalrun_fixture", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.RecordEvaluationRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.IsolatedMemoryNamespace, "eval://memory/") || stored.PromotionRecommendation != evaluation.RecommendationReviewOnly {
		t.Fatalf("run isolation/review-only contract not retained: %#v", stored)
	}
	runs, err := store.ListEvaluationRuns(ctx, dataset.ID, 10)
	if err != nil || len(runs) != 1 || len(runs[0].Results) != 2 || runs[0].ReportSHA256 == "" {
		t.Fatalf("evaluation runs = %#v, %v", runs, err)
	}
	datasets, err := store.ListEvaluationDatasets(ctx, "owner-repo", 10)
	if err != nil || len(datasets) != 1 || datasets[0].ID != dataset.ID {
		t.Fatalf("evaluation datasets = %#v, %v", datasets, err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE evaluation_runs SET reason='changed' WHERE id=?", stored.ID); err == nil {
		t.Fatal("append-only evaluation run update unexpectedly succeeded")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM evaluation_datasets WHERE id=?", dataset.ID); err == nil {
		t.Fatal("append-only evaluation dataset delete unexpectedly succeeded")
	}
}

func TestEvaluationValidationRejectsNamespaceLeakageAtStorageBoundary(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.RecordEvaluationRun(ctx, evaluation.Run{
		ID: "evalrun_bad", SchemaVersion: evaluation.SchemaVersion, DatasetID: "evaldataset_bad", ProjectID: "owner-repo",
		Status: evaluation.RunCompleted, ProfileMatrix: []string{"local"}, IsolatedMemoryNamespace: "viking://resources/projects/owner/repo/memory",
		IsolatedCacheNamespace: "eval://cache/owner-repo/dataset/run", BudgetSeconds: 3600, Concurrency: 1,
		ScoringProfile: "quality_default_v1", ReportSHA256: strings.Repeat("a", 64),
		PromotionRecommendation: evaluation.RecommendationReviewOnly, Reason: "bad", ActorID: "operator",
	})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("expected invalid leakage, got %v", err)
	}
}
