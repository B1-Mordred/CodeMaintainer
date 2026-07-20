package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func TestRegistryServiceDraftValidateReviewApplyAndResolve(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		t.Fatal(err)
	}
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeSystem}
	invalid, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "invalid",
		Entries: []appconfig.DraftEntry{{Key: "runner.arbitrary_command", Value: json.RawMessage(`"rm"`), Configured: true}},
	})
	if err != nil || report.Valid || invalid.ID != "" || len(report.Issues) != 1 || report.Issues[0].Code != "unregistered_key" {
		t.Fatalf("unregistered setting validation = %#v %#v %v", invalid, report, err)
	}
	draft, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "raise review depth",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	})
	if err != nil || !report.Valid || draft.ID == "" {
		t.Fatalf("create draft = %#v %#v %v", draft, report, err)
	}
	checks, err := service.DryRunDraft(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Kind != "validation" || checks[1].Kind != "dry_run" || checks[1].Status != "passed" {
		t.Fatalf("dry-run checks = %#v", checks)
	}
	draft, report, err = service.ReviewDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer",
		Target: appconfig.DraftReviewed, Reason: "reviewed exact diff",
	})
	if err != nil || !report.Valid || draft.State != appconfig.DraftReviewed {
		t.Fatalf("review draft = %#v %#v %v", draft, report, err)
	}
	revision, state, report, err := service.ApplyDraft(ctx, draft.ID, draft.Version, "reviewer", "administrator", "apply reviewed diff", false)
	if err != nil || !report.Valid || revision.ID == "" || state.Version != 1 {
		t.Fatalf("apply draft = %#v %#v %#v %v", revision, state, report, err)
	}
	effective, err := service.Effective(ctx, []appconfig.ScopeRef{scope})
	if err != nil {
		t.Fatal(err)
	}
	value := effective.Values["workflow.max_review_cycles"]
	if string(value.Value) != "3" || value.SourceScope.Kind != appconfig.ScopeSystem || len(value.Contributions) != 2 {
		t.Fatalf("effective review cycles = %#v", value)
	}
	if _, _, _, err := service.ApplyDraft(ctx, draft.ID, draft.Version, "reviewer", "administrator", "replay", false); err == nil {
		t.Fatal("applied draft replay unexpectedly succeeded")
	}
}

func TestRegistryServiceEnforcesContextualDependencies(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		t.Fatal(err)
	}
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeSystem}

	draft, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "unsafe waiver policy",
		Entries: []appconfig.DraftEntry{{Key: "qc.waiver_rationale_required", Value: json.RawMessage(`false`), Configured: true}},
	})
	if err != nil || report.Valid || draft.ID != "" || len(report.Issues) != 1 || report.Issues[0].Code != "dependency_unsatisfied" || report.Issues[0].Key != "qc.human_waiver_enabled" {
		t.Fatalf("unsafe dependency result = %#v %#v %v", draft, report, err)
	}

	draft, report, err = service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "disable waiver feature and policy together",
		Entries: []appconfig.DraftEntry{
			{Key: "qc.human_waiver_enabled", Value: json.RawMessage(`false`), Configured: true},
			{Key: "qc.waiver_rationale_required", Value: json.RawMessage(`false`), Configured: true},
		},
	})
	if err != nil || !report.Valid || draft.ID == "" {
		t.Fatalf("inactive dependency result = %#v %#v %v", draft, report, err)
	}

	draft, report, err = service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: 0, AuthorID: "author", Reason: "invalid context allocation",
		Entries: []appconfig.DraftEntry{
			{Key: "intelligence.context_input_tokens", Value: json.RawMessage(`4096`), Configured: true},
			{Key: "intelligence.context_output_reserve_tokens", Value: json.RawMessage(`4096`), Configured: true},
		},
	})
	if err != nil || report.Valid || draft.ID != "" || len(report.Issues) != 1 || report.Issues[0].Code != "cross_field_validation" {
		t.Fatalf("context budget relation result = %#v %#v %v", draft, report, err)
	}
}

func TestRegistryServiceRunsTrustedPrerequisiteAndDryRunHandlers(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		t.Fatal(err)
	}
	prerequisites := service.Prerequisites(ctx)
	if len(prerequisites) != 1 || prerequisites[0].Prerequisite != "durable-controller-storage" || prerequisites[0].Status != "passed" {
		t.Fatalf("prerequisite health = %#v", prerequisites)
	}
	draft, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, BaseScopeVersion: 0,
		AuthorID: "author", Reason: "exercise local inbox readiness",
		Entries: []appconfig.DraftEntry{{Key: "notifications.local_inbox_enabled", Value: json.RawMessage(`false`), Configured: true}},
	})
	if err != nil || !report.Valid {
		t.Fatalf("create notification draft = %#v %v", report, err)
	}
	checks, err := service.DryRunDraft(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 3 {
		t.Fatalf("checks = %#v", checks)
	}
	got := map[string]string{}
	for _, check := range checks {
		got[check.Handler] = check.Status
	}
	for _, handler := range []string{"registry", "local-inbox-readiness", "durable-controller-storage"} {
		if got[handler] != "passed" {
			t.Errorf("handler %q status = %q, all checks %#v", handler, got[handler], checks)
		}
	}
}

func TestRegistryServiceRollbackRestoresWholeHistoricalScope(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	service, _ := appconfig.NewRegistryService(store, registry)
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "project-one"}
	applyDraft := func(base int64, entries []appconfig.DraftEntry) appconfig.RegistryRevision {
		t.Helper()
		draft, report, createErr := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
			Scope: scope, BaseScopeVersion: base, AuthorID: "author", Reason: "proposal", Entries: entries,
		})
		if createErr != nil || !report.Valid {
			t.Fatalf("create: %#v %v", report, createErr)
		}
		draft, report, createErr = service.ReviewDraft(ctx, appconfig.TransitionDraftRequest{
			ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer", Target: appconfig.DraftReviewed, Reason: "review",
		})
		if createErr != nil || !report.Valid {
			t.Fatalf("review: %#v %v", report, createErr)
		}
		revision, _, report, createErr := service.ApplyDraft(ctx, draft.ID, draft.Version, "reviewer", "administrator", "apply", false)
		if createErr != nil || !report.Valid {
			t.Fatalf("apply: %#v %v", report, createErr)
		}
		return revision
	}
	first := applyDraft(0, []appconfig.DraftEntry{
		{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true},
		{Key: "workflow.max_wall_seconds", Value: json.RawMessage(`3600`), Configured: true},
	})
	_ = applyDraft(1, []appconfig.DraftEntry{
		{Key: "workflow.max_review_cycles", Value: json.RawMessage(`5`), Configured: true},
		{Key: "workflow.max_wall_seconds", Reset: true},
	})
	revision, state, err := service.Rollback(ctx, first.ID, 2, "admin", "administrator", "restore first revision", false)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Operation != "rollback" || revision.RollbackOf != first.ID || state.Version != 3 || len(state.Values) != 2 {
		t.Fatalf("rollback result = %#v %#v", revision, state)
	}
	effective, err := service.Effective(ctx, []appconfig.ScopeRef{scope})
	if err != nil {
		t.Fatal(err)
	}
	if string(effective.Values["workflow.max_review_cycles"].Value) != "3" || string(effective.Values["workflow.max_wall_seconds"].Value) != "3600" {
		t.Fatalf("rollback effective configuration = %#v", effective.Values)
	}
	if _, _, err := service.Rollback(ctx, first.ID, 2, "admin", "administrator", "stale", false); err == nil {
		t.Fatal("stale rollback unexpectedly succeeded")
	}
}

func TestRegistryServiceSnapshotsCompleteEffectiveConfigurationOnce(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	service, _ := appconfig.NewRegistryService(store, registry)
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job-service-snapshot", ProjectID: "project-one", Repository: "owner/repo", Task: "snapshot", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.SnapshotJob(ctx, job.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var document appconfig.Snapshot
	if err := json.Unmarshal(snapshot.Document, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Values) != len(registry.Descriptors()) || document.SHA256 != snapshot.SHA256 {
		t.Fatalf("incomplete job snapshot: %#v", document)
	}
	replayed, err := service.SnapshotJob(ctx, job.ID, nil)
	if err != nil || replayed.CreatedAt != snapshot.CreatedAt {
		t.Fatalf("snapshot replay = %#v %v", replayed, err)
	}
	_, err = store.SaveJobConfigSnapshot(ctx, appconfig.JobSnapshot{JobID: "missing", SchemaVersion: 1, RegistryHash: snapshot.RegistryHash, SHA256: snapshot.SHA256, Document: snapshot.Document})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing job snapshot error = %v", err)
	}
}

func TestRegistryServiceBootstrapsAndProjectsSystemScopeToIncrementOneDocument(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	active := appconfig.Default(".data")
	document, _ := json.Marshal(active)
	legacy, err := store.CreateConfigRevision(ctx, appconfig.Revision{
		ActorID: "system", SchemaVersion: appconfig.SchemaVersion, Before: json.RawMessage(`{}`),
		After: document, Diff: json.RawMessage(`[]`), ValidationResult: json.RawMessage(`{"valid":true}`), Reason: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := appconfig.BuiltInRegistry(active)
	service, _ := appconfig.NewRegistryService(store, registry)
	state, err := service.EnsureSystemScope(ctx, active, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || len(state.Values) != 10 {
		t.Fatalf("bootstrapped system scope = %#v", state)
	}
	revisions, err := store.ListConfigRevisions(ctx, 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("no-op bootstrap changed legacy history: %#v %v", revisions, err)
	}
	draft, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, BaseScopeVersion: 1,
		AuthorID: "author", Reason: "change legacy value",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`4`), Configured: true}},
	})
	if err != nil || !report.Valid {
		t.Fatalf("create system draft = %#v %v", report, err)
	}
	draft, _, err = service.ReviewDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer", Target: appconfig.DraftReviewed, Reason: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.ApplyDraft(ctx, draft.ID, draft.Version, "reviewer", "administrator", "apply", false); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var projected appconfig.System
	if err := json.Unmarshal(current.After, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.Workflow.MaxReviewCycles != 4 || projected.Deployment != active.Deployment {
		t.Fatalf("legacy projection changed the wrong behavior: %#v", projected)
	}
}
