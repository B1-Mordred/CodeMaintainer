package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/models"
	"github.com/B1-Mordred/CodeMaintainer/internal/runtimeopt"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestRuntimeBenchmarksPersistAppendOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := runtimeopt.FromSmoke(models.Profile{
		ID: "implementation", Role: "implementation", ModelFamily: "fixture", Context: 4096, Quantization: "Q8_0",
		Threads: 4, Batch: 512, UBatch: 128, NUMA: "disabled",
	}, models.Status{ProfileID: "implementation", PromptTokensSecond: 10, DecodeTokensSecond: 8},
		models.SmokeResult{ProfileID: "implementation", Duration: time.Second, Healthy: true}, "operator", time.Unix(1, 0).UTC())
	stored, err := store.RecordRuntimeBenchmark(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == "" || stored.Recommendation != runtimeopt.RecommendationCandidate {
		t.Fatalf("unexpected stored benchmark: %#v", stored)
	}
	items, err := store.ListRuntimeBenchmarks(ctx, "implementation", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RuntimeIdentitySHA256 != run.RuntimeIdentitySHA256 || len(items[0].QualityFixtures) != 1 {
		t.Fatalf("runtime benchmarks not retained: %#v", items)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE runtime_benchmarks SET reason='changed' WHERE id=?", stored.ID); err == nil {
		t.Fatal("append-only runtime benchmark update unexpectedly succeeded")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM runtime_benchmarks WHERE id=?", stored.ID); err == nil {
		t.Fatal("append-only runtime benchmark delete unexpectedly succeeded")
	}
}

func TestRuntimeBenchmarkValidationRejectsInvalidEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.RecordRuntimeBenchmark(ctx, runtimeopt.BenchmarkRun{ID: "bad"})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("expected invalid runtime benchmark, got %v", err)
	}
}
