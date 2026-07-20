package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
