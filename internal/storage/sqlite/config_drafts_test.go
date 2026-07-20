package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func TestConfigDraftReviewAndApplyAreVersionBound(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "project-one"}
	draft, err := store.CreateConfigDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "initial proposal",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != appconfig.DraftOpen || draft.Version != 1 || len(draft.Entries) != 1 {
		t.Fatalf("created draft = %#v", draft)
	}
	draft, err = store.UpdateConfigDraft(ctx, appconfig.UpdateDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "author", Reason: "refined proposal",
		Entries: []appconfig.DraftEntry{
			{Key: "workflow.max_review_cycles", Value: json.RawMessage(`4`), Configured: true},
			{Key: "notifications.local_inbox_enabled", Value: json.RawMessage(`false`), Configured: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Version != 2 || len(draft.Entries) != 2 {
		t.Fatalf("updated draft = %#v", draft)
	}
	if _, err := store.UpdateConfigDraft(ctx, appconfig.UpdateDraftRequest{
		ID: draft.ID, ExpectedVersion: 1, ActorID: "author", Reason: "stale edit", Entries: draft.Entries,
	}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale draft update error = %v", err)
	}
	draft, err = store.TransitionConfigDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer",
		Target: appconfig.DraftReviewed, Reason: "reviewed exact proposal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != appconfig.DraftReviewed || draft.ReviewerID != "reviewer" || draft.Version != 3 {
		t.Fatalf("reviewed draft = %#v", draft)
	}
	mutatedChanges := []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`5`), Configured: true}, {Key: "notifications.local_inbox_enabled", Value: json.RawMessage(`false`), Configured: true}}
	if _, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "reviewer", ActorRole: "administrator",
		Operation: "apply", Reason: "mutated after review", DraftID: draft.ID, DraftVersion: draft.Version,
		Changes: mutatedChanges,
	}); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("mutated reviewed draft error = %v", err)
	}
	changes := []appconfig.ScopeChange{
		{Key: "workflow.max_review_cycles", Value: json.RawMessage(`4`), Configured: true},
		{Key: "notifications.local_inbox_enabled", Value: json.RawMessage(`false`), Configured: true},
	}
	revision, state, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "reviewer", ActorRole: "administrator",
		Operation: "apply", Reason: "apply reviewed proposal", DraftID: draft.ID, DraftVersion: draft.Version,
		Changes: changes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || revision.DraftID != draft.ID {
		t.Fatalf("applied draft result = %#v %#v", revision, state)
	}
	applied, err := store.GetConfigDraft(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if applied.State != appconfig.DraftApplied || applied.AppliedRevisionID != revision.ID || applied.Version != 4 {
		t.Fatalf("draft apply was not atomic: %#v", applied)
	}
}

func TestConfigDraftBecomesStaleWhenScopeChanges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeSystem}
	draft, err := store.CreateConfigDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "proposal",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "other", ActorRole: "administrator",
		Operation: "apply", Reason: "concurrent apply", Changes: []appconfig.ScopeChange{{Key: "workflow.max_wall_seconds", Value: json.RawMessage(`3600`), Configured: true}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.TransitionConfigDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer",
		Target: appconfig.DraftReviewed, Reason: "should be stale",
	})
	if !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale draft review error = %v", err)
	}
}

func TestConfigChecksAreAppendOnlyAndBoundedToDraft(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	draft, err := store.CreateConfigDraft(ctx, appconfig.CreateDraftRequest{
		Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, BaseScopeVersion: 0,
		AuthorID: "author", Reason: "check proposal",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	check, err := store.SaveConfigCheck(ctx, appconfig.CheckResult{
		DraftID: draft.ID, DraftVersion: draft.Version, Kind: "validation", Handler: "registry", Status: "passed",
		Result: json.RawMessage(`{"valid":true,"errors":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListConfigChecks(ctx, draft.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != check.ID || items[0].Sequence != 1 {
		t.Fatalf("configuration checks = %#v", items)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE config_checks SET result = '{}'"); err == nil {
		t.Fatal("append-only configuration check accepted an update")
	}
}

func TestHistoricalConfigScopeReconstructionIncludesResets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "project-one"}
	first, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "admin", ActorRole: "administrator", Operation: "apply", Reason: "first",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}, {Key: "workflow.max_wall_seconds", Value: json.RawMessage(`3600`), Configured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 1, ActorID: "admin", ActorRole: "administrator", Operation: "reset", Reason: "second",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Configured: false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstState, err := store.GetConfigScopeAtRevision(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondState, err := store.GetConfigScopeAtRevision(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstState.Values) != 2 || len(secondState.Values) != 1 || secondState.Values[0].Key != "workflow.max_wall_seconds" {
		t.Fatalf("historical states = %#v %#v", firstState, secondState)
	}
}
