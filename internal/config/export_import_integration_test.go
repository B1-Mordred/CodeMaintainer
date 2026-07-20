package config_test

import (
	"context"
	"encoding/json"
	"testing"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func TestDeclarativeExportAndForwardCompatibleImportNeverApplyUnknownKeys(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	active := appconfig.Default(".data")
	document, _ := json.Marshal(active)
	legacy, err := store.CreateConfigRevision(ctx, appconfig.Revision{
		ActorID: "system", SchemaVersion: 1, Before: json.RawMessage(`{}`), After: document,
		Diff: json.RawMessage(`[]`), ValidationResult: json.RawMessage(`{"valid":true}`), Reason: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := appconfig.BuiltInRegistry(active)
	service, _ := appconfig.NewRegistryService(store, registry)
	if _, err := service.EnsureSystemScope(ctx, active, legacy.ID); err != nil {
		t.Fatal(err)
	}
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeSystem}
	exported, err := service.ExportScope(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := appconfig.VerifyDeclarativeConfig(exported); err != nil || len(exported.Values) != 10 {
		t.Fatalf("export = %#v, verify = %v", exported, err)
	}
	unsafePayload, _ := json.Marshal(exported)
	var unsafeImport appconfig.DeclarativeConfig
	if err := json.Unmarshal(unsafePayload, &unsafeImport); err != nil {
		t.Fatal(err)
	}
	for index := range unsafeImport.Values {
		if unsafeImport.Values[index].Key == "qc.waiver_rationale_required" {
			unsafeImport.Values[index].Value = json.RawMessage(`false`)
		}
	}
	unsafeImport.DocumentHash, _ = appconfig.ComputeDeclarativeConfigHash(unsafeImport)
	unsafePreview, err := service.PreviewImportContext(ctx, unsafeImport, "strict", scope, 1)
	if err != nil || unsafePreview.Valid {
		t.Fatalf("contextual import accepted an unsatisfied dependency: %#v %v", unsafePreview, err)
	}
	for index := range exported.Values {
		if exported.Values[index].Key == "workflow.max_review_cycles" {
			exported.Values[index].Value = json.RawMessage(`4`)
		}
	}
	exported.Values = append(exported.Values,
		appconfig.ImportValue{Key: "future.safe_setting", Value: json.RawMessage(`{"opaque":true}`), Configured: true},
		appconfig.ImportValue{Key: "future.write_only", Configured: true, Secret: true, Redacted: true},
	)
	exported.DocumentHash, err = appconfig.ComputeDeclarativeConfigHash(exported)
	if err != nil {
		t.Fatal(err)
	}
	strict := service.PreviewImport(exported, "strict", scope, 1)
	if strict.Valid {
		t.Fatalf("strict import accepted an unknown key: %#v", strict)
	}
	draft, preview, err := service.ImportDraft(ctx, exported, "forward_compatible", scope, 1, "admin", "forward-compatible fixture")
	if err != nil || !preview.Valid || draft.Operation != "import" || len(draft.UnknownEntries) != 2 ||
		draft.UnknownEntries[0].Key != "future.safe_setting" || draft.UnknownEntries[1].Key != "future.write_only" ||
		!draft.UnknownEntries[1].Configured || !draft.UnknownEntries[1].Secret || !draft.UnknownEntries[1].Redacted || len(draft.UnknownEntries[1].Value) != 0 {
		t.Fatalf("forward import = %#v %#v %v", draft, preview, err)
	}
	draft, report, err := service.ReviewDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "reviewer", Target: appconfig.DraftReviewed, Reason: "review import",
	})
	if err != nil || !report.Valid {
		t.Fatalf("review import = %#v %v", report, err)
	}
	revision, state, _, err := service.ApplyDraft(ctx, draft.ID, draft.Version, "reviewer", "administrator", "apply import", false)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Operation != "import" || state.Version != 2 || len(state.Values) != 10 {
		t.Fatalf("applied import = %#v %#v", revision, state)
	}
	for _, value := range state.Values {
		if value.Key == "future.safe_setting" {
			t.Fatal("preserved unknown key was applied")
		}
	}
	effective, err := service.Effective(ctx, []appconfig.ScopeRef{scope})
	if err != nil || string(effective.Values["workflow.max_review_cycles"].Value) != "4" {
		t.Fatalf("known imported value was not applied: %#v %v", effective, err)
	}
}

func TestDeclarativeImportRejectsTampering(t *testing.T) {
	document := appconfig.DeclarativeConfig{
		SchemaVersion: 1, RegistryHash: "registry", Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem},
		Values: []appconfig.ImportValue{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	}
	document.DocumentHash, _ = appconfig.ComputeDeclarativeConfigHash(document)
	if err := appconfig.VerifyDeclarativeConfig(document); err != nil {
		t.Fatal(err)
	}
	document.Values[0].Value = json.RawMessage(`10`)
	if err := appconfig.VerifyDeclarativeConfig(document); err == nil {
		t.Fatal("tampered declarative configuration passed verification")
	}
}
