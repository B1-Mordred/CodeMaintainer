package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/memory"
)

func TestDurableMemoryIsProjectScopedQuarantinedAuditedAndTraceable(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	alpha := memory.ProjectScope{Owner: "owner", Repository: "alpha"}
	beta := memory.ProjectScope{Owner: "owner", Repository: "beta"}
	record, err := store.PutCandidate(ctx, alpha, memory.Record{
		ID: "memory_0123456789abcdef0123456789abcdef", Kind: "verified_case",
		Content: "The fixed full verification command is go test ./...", SourceURI: "job://job_fixture/final-report",
		BaseCommit: "0123456789abcdef0123456789abcdef01234567", AffectedPaths: []string{"go.mod", "internal/example.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != memory.StatusQuarantine || !record.SecretScanPass || record.Namespace != alpha.Namespace() {
		t.Fatalf("unexpected candidate: %+v", record)
	}
	for _, scope := range []memory.ProjectScope{alpha, beta} {
		items, err := store.Search(ctx, scope, "verification", 10)
		if err != nil || len(items) != 0 {
			t.Fatalf("quarantine leaked into %v: %+v %v", scope, items, err)
		}
	}
	if _, err := store.GetMemory(ctx, beta, record.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("cross-project lookup disclosed record: %v", err)
	}
	record, err = store.PromoteMemory(ctx, alpha, record.ID, memory.PromotionRequest{
		ActorID: "reviewer", Rationale: "verified directly from final report", Basis: "human_approval", ExpectedVersion: record.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.Search(ctx, alpha, "verification", 10)
	if err != nil || len(items) != 1 || items[0].ID != record.ID {
		t.Fatalf("canonical search failed: %+v %v", items, err)
	}
	trace, err := store.RecordRetrieval(ctx, memory.RetrievalTrace{
		JobID: "job_fixture", Scope: alpha, Query: "verification", CandidateIDs: []string{record.ID},
		SelectedIDs: []string{record.ID}, BudgetTokens: 4096, AllocatedTokens: 24,
		Trajectory: json.RawMessage(`{"filter":"project-before-ranking","ranker":"lexical-fake"}`),
	})
	if err != nil || trace.QueryHash == "" {
		t.Fatalf("trace failed: %+v %v", trace, err)
	}
	traces, err := store.ListRetrievals(ctx, beta, 10)
	if err != nil || len(traces) != 0 {
		t.Fatalf("retrieval trace leaked: %+v %v", traces, err)
	}
	record, err = store.CorrectMemory(ctx, alpha, record.ID, memory.CorrectionRequest{
		Content: "Run go test -race ./... for the full verified gate.", AffectedPaths: []string{"go.mod"},
		InvalidationRule: "invalidate when go.mod changes", ActorID: "reviewer", Rationale: "correct the required command", ExpectedVersion: record.Version,
	})
	if err != nil || record.Status != memory.StatusQuarantine || record.Verified {
		t.Fatalf("correction did not re-quarantine: %+v %v", record, err)
	}
	record, err = store.VerifyMemory(ctx, alpha, record.ID, memory.VerificationRequest{
		ActorID: "verifier", JobID: "job_fixture", ArtifactID: "artifact_verification",
		Commit: "0123456789abcdef0123456789abcdef01234567", ExpectedVersion: record.Version,
	})
	if err != nil || !record.Verified {
		t.Fatalf("verification evidence was not retained: %+v %v", record, err)
	}
	record, err = store.PromoteMemory(ctx, alpha, record.ID, memory.PromotionRequest{
		ActorID: "controller", Rationale: "all deterministic checks passed", Basis: "deterministic_verification", ExpectedVersion: record.Version,
	})
	if err != nil || record.Status != memory.StatusCanonical {
		t.Fatalf("verified promotion failed: %+v %v", record, err)
	}
	if err := store.Delete(ctx, alpha, record.ID, "administrator"); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.GetMemory(ctx, alpha, record.ID)
	if err != nil || deleted.Status != memory.StatusDeleted || deleted.Content != "" {
		t.Fatalf("delete retained content: %+v %v", deleted, err)
	}
	events, err := store.ListMemoryEvents(ctx, alpha, record.ID, 20)
	if err != nil || len(events) != 6 {
		t.Fatalf("memory history = %+v, %v", events, err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM memory_events WHERE record_id = ?", record.ID); err == nil {
		t.Fatal("append-only memory history was deleted")
	}
}

func TestMemoryIndexQueueIsDurableLeasedAndRetryable(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	clock := time.Date(2026, 7, 20, 19, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	ctx := context.Background()
	scope := memory.ProjectScope{Owner: "owner", Repository: "queue"}
	record, err := store.PutCandidate(ctx, scope, memory.Record{Content: "canonical queue knowledge", Kind: "pattern"})
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.PromoteMemory(ctx, scope, record.ID, memory.PromotionRequest{
		ActorID: "reviewer", Rationale: "reviewed", Basis: "human_approval", ExpectedVersion: record.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.ClaimMemoryIndexOperation(ctx, "indexer-one", 30*time.Second)
	if err != nil || operation.Action != "upsert" || operation.RecordVersion != record.Version || operation.Attempts != 1 {
		t.Fatalf("unexpected claimed operation: %+v, %v", operation, err)
	}
	if _, err := store.ClaimMemoryIndexOperation(ctx, "indexer-two", 30*time.Second); !errors.Is(err, memory.ErrNoIndexOperation) {
		t.Fatalf("active lease was stolen: %v", err)
	}
	if err := store.CompleteMemoryIndexOperation(ctx, operation.ID, "indexer-one"); err != nil {
		t.Fatal(err)
	}
	record, err = store.InvalidateMemory(ctx, scope, record.ID, memory.InvalidationRequest{
		ActorID: "reviewer", Rationale: "source changed", ExpectedVersion: record.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = store.ClaimMemoryIndexOperation(ctx, "indexer-one", 30*time.Second)
	if err != nil || operation.Action != "forget" {
		t.Fatalf("unexpected forget operation: %+v, %v", operation, err)
	}
	if err := store.FailMemoryIndexOperation(ctx, operation.ID, "indexer-one", "temporary\nbackend error", time.Second); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	retried, err := store.ClaimMemoryIndexOperation(ctx, "indexer-two", 30*time.Second)
	if err != nil || retried.ID != operation.ID || retried.Attempts != 2 || strings.Contains(retried.LastError, "\n") {
		t.Fatalf("failed operation was not safely retried: %+v, %v", retried, err)
	}
}

func TestMemoryCandidateRejectsSecretsTraversalAndInvalidCommits(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	for name, candidate := range map[string]memory.Record{
		"secret": {ID: "memory_secret", Content: "password=do-not-store", Kind: "pattern"},
		"path":   {ID: "memory_path", Content: "safe content", Kind: "pattern", AffectedPaths: []string{"../secret"}},
		"commit": {ID: "memory_commit", Content: "safe content", Kind: "pattern", BaseCommit: "main"},
		"source": {ID: "memory_source", Content: "safe content", Kind: "pattern", SourceURI: "https://token@example.invalid/private"},
	} {
		if _, err := store.PutCandidate(context.Background(), scope, candidate); err == nil {
			t.Fatalf("%s candidate was accepted", name)
		}
	}
}

func TestProjectMemoryExportDryRunAndRestoreRemainScopedAndQuarantined(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	scope := memory.ProjectScope{Owner: "owner", Repository: "repo"}
	record, err := store.PutCandidate(ctx, scope, memory.Record{
		Content: "Verified builds use the pinned offline command registry.", Kind: "project_knowledge",
		SourceURI: "job://controller/job_1", BaseCommit: strings.Repeat("a", 40), AffectedPaths: []string{"README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.ExportProjectMemory(ctx, scope)
	if err != nil || len(bundle.Records) != 1 || bundle.ManifestHash == "" || bundle.Records[0].OriginalID != record.ID {
		t.Fatalf("export = %+v, %v", bundle, err)
	}
	other := memory.ProjectScope{Owner: "owner", Repository: "other"}
	if _, err := store.RestoreProjectMemory(ctx, other, bundle, true, "admin"); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("cross-project restore returned %v", err)
	}
	tampered := bundle
	tampered.Records = append([]memory.ExportRecord(nil), bundle.Records...)
	tampered.Records[0].Content += " tampered"
	if _, err := store.RestoreProjectMemory(ctx, scope, tampered, true, "admin"); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("tampered restore returned %v", err)
	}
	dryRun, err := store.RestoreProjectMemory(ctx, scope, bundle, true, "admin")
	if err != nil || dryRun.Imported != 0 || dryRun.Skipped != 1 {
		t.Fatalf("same-project dry run = %+v, %v", dryRun, err)
	}
	if err := store.DeleteMemory(ctx, scope, record.ID, memory.DeletionRequest{ActorID: "admin", Rationale: "exercise restore", ExpectedVersion: record.Version}); err != nil {
		t.Fatal(err)
	}
	restored, err := store.RestoreProjectMemory(ctx, scope, bundle, false, "admin")
	if err != nil || restored.Imported != 1 || restored.Skipped != 0 {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	items, err := store.ListMemory(ctx, scope, memory.StatusQuarantine, 10)
	if err != nil || len(items) != 1 || items[0].Verified || items[0].ID == record.ID {
		t.Fatalf("restored items = %+v, %v", items, err)
	}
}
