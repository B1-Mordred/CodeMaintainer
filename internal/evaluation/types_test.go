package evaluation

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluationRunUsesIsolatedNamespacesAndBlocksPromotion(t *testing.T) {
	dataset := NewDataset(CreateDatasetRequest{
		ProjectID: "owner-repo", Name: "historical fixes", SourceKind: SourceHistoricalRange, Repository: "owner/repo",
		BaseRevision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TargetRevision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		KnownPatchSHA256: strings.Repeat("c", 64), Exclusions: []string{"vendor/**"},
	}, "operator", time.Unix(1, 0).UTC())
	dataset.ID = "evaluation_dataset_fixture"
	if err := dataset.Validate(); err != nil {
		t.Fatal(err)
	}
	run, err := SimulateRun(dataset, LaunchRunRequest{ProfileMatrix: []string{"local", "remote-fake"}, BudgetSeconds: 3600, Concurrency: 2}, "operator", "evaluation_run_fixture", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(run.IsolatedMemoryNamespace, "eval://memory/") || strings.Contains(run.IsolatedMemoryNamespace, "viking://resources/projects") {
		t.Fatalf("memory namespace is not isolated: %s", run.IsolatedMemoryNamespace)
	}
	if len(run.Results) != 2 || run.Results[0].PromotionAllowed || run.PromotionRecommendation != RecommendationReviewOnly {
		t.Fatalf("run did not retain review-only profile comparison: %#v", run)
	}
}

func TestEvaluationValidationRejectsProjectMemoryLeakage(t *testing.T) {
	run := Run{
		ID: "evaluation_run_bad", SchemaVersion: SchemaVersion, DatasetID: "dataset", ProjectID: "owner-repo", Status: RunCompleted,
		ProfileMatrix: []string{"local"}, IsolatedMemoryNamespace: "viking://resources/projects/owner/repo/memory",
		IsolatedCacheNamespace: "eval://cache/owner-repo/dataset/run", BudgetSeconds: 3600, Concurrency: 1,
		ScoringProfile: "quality_default_v1", ReportSHA256: strings.Repeat("a", 64), PromotionRecommendation: RecommendationReviewOnly,
		Reason: "bad", ActorID: "operator",
	}
	if err := run.Validate(); err == nil {
		t.Fatal("project memory namespace was accepted for evaluation")
	}
}
