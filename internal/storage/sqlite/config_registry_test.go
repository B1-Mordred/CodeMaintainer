package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/automation"
	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestConfigScopeApplyIsAtomicAuditedAndOptimisticallyLocked(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeSystem}
	empty, err := store.GetConfigScope(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Version != 0 || len(empty.Values) != 0 {
		t.Fatalf("new scope = %#v", empty)
	}
	request := appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "admin", ActorRole: "administrator",
		Operation: "apply", Reason: "exercise scoped persistence",
		Changes: []appconfig.ScopeChange{
			{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true},
			{Key: "notifications.local_inbox_enabled", Value: json.RawMessage(`false`), Configured: true},
		},
	}
	revision, state, err := store.ApplyConfigScope(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if revision.ScopeVersion != 1 || state.Version != 1 || state.RevisionID != revision.ID || len(state.Values) != 2 {
		t.Fatalf("unexpected applied revision/state: %#v %#v", revision, state)
	}
	if state.Values[0].Key != "notifications.local_inbox_enabled" || state.Values[1].Key != "workflow.max_review_cycles" {
		t.Fatalf("scope values are not stable-key ordered: %#v", state.Values)
	}
	if _, _, err := store.ApplyConfigScope(ctx, request); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale apply error = %v, want conflict", err)
	}
	unchanged, err := store.GetConfigScope(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != 1 || len(unchanged.Values) != 2 {
		t.Fatalf("stale write changed scope: %#v", unchanged)
	}
	auditEvents, err := store.ListAudit(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(auditEvents) != 1 || auditEvents[0].Action != "config_registry.apply" || strings.Contains(string(auditEvents[0].Details), "after_value") || strings.Contains(string(auditEvents[0].Details), "\"value\"") {
		t.Fatalf("unexpected or value-bearing audit event: %#v", auditEvents)
	}
}

func TestConfigScopeResetRevisionHistoryAndSecretRedaction(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "project-one"}
	first, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 0, ActorID: "admin", ActorRole: "administrator",
		Operation: "apply", Reason: "set project values",
		Changes: []appconfig.ScopeChange{
			{Key: "workflow.max_review_cycles", Value: json.RawMessage(`4`), Configured: true},
			{Key: "provider.api_key", Configured: true, Secret: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, state, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: scope, ExpectedVersion: 1, ActorID: "admin", ActorRole: "administrator",
		Operation: "reset", Reason: "inherit review-cycle policy",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Configured: false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 2 || len(state.Values) != 1 || state.Values[0].Key != "provider.api_key" || !state.Values[0].Secret || len(state.Values[0].Value) != 0 || !state.Values[0].Configured {
		t.Fatalf("reset or secret storage is unsafe: %#v", state)
	}
	items, err := store.ListConfigRegistryRevisions(ctx, scope, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != second.ID || items[1].ID != first.ID || len(items[0].Entries) != 1 || items[0].Entries[0].AfterConfigured {
		t.Fatalf("unexpected revision history: %#v", items)
	}
	restored, err := store.GetConfigRegistryRevision(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(restored)
	if strings.Contains(string(encoded), "api_key\":\"") || strings.Contains(string(encoded), "super-secret") {
		t.Fatalf("secret revision leaked a value: %s", encoded)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE config_registry_revisions SET reason = 'changed'"); err == nil {
		t.Fatal("append-only registry revision accepted an update")
	}
}

func TestConfigRollbackTargetMustBelongToSameScope(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectScope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "project-one"}
	revision, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: projectScope, ExpectedVersion: 0, ActorID: "admin", ActorRole: "administrator",
		Operation: "apply", Reason: "project value", Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`4`), Configured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, ExpectedVersion: 0,
		ActorID: "admin", ActorRole: "administrator", Operation: "rollback",
		Reason: "invalid cross-scope rollback", RollbackOf: revision.ID,
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`2`), Configured: true}},
	})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("cross-scope rollback error = %v", err)
	}
}

func TestJobConfigurationSnapshotIsImmutableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ID: "job-config-snapshot", ProjectID: "project-one", Repository: "owner/repo", Task: "test snapshots", ActorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	snapshotValue, err := registry.Snapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	document, _ := json.Marshal(snapshotValue)
	snapshot := appconfig.JobSnapshot{
		JobID: job.ID, SchemaVersion: snapshotValue.SchemaVersion,
		RegistryHash: snapshotValue.RegistryHash, SHA256: snapshotValue.SHA256, Document: document,
	}
	created, err := store.SaveJobConfigSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.SaveJobConfigSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedAt != replayed.CreatedAt {
		t.Fatalf("idempotent snapshot changed creation time: %v %v", created.CreatedAt, replayed.CreatedAt)
	}
	snapshot.SHA256 = strings.Repeat("a", 64)
	if _, err := store.SaveJobConfigSnapshot(ctx, snapshot); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("different snapshot error = %v, want conflict", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE job_config_snapshots SET snapshot_sha256 = ?", strings.Repeat("b", 64)); err == nil {
		t.Fatal("immutable snapshot accepted an update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM job_config_snapshots"); err == nil {
		t.Fatal("immutable snapshot accepted a delete")
	}
	loaded, err := store.GetJobConfigSnapshot(ctx, job.ID)
	if err != nil || loaded.SHA256 != created.SHA256 {
		t.Fatalf("load snapshot = %#v, %v", loaded, err)
	}
}

func TestConfiguredRegistrySnapshotsDirectAndScheduledJobsAtomicallyAtAcceptance(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := time.Date(2026, 7, 20, 22, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	registry, err := appconfig.BuiltInRegistry(appconfig.Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: appconfig.ScopeRef{Kind: appconfig.ScopeSystem}, ExpectedVersion: 0,
		ActorID: "admin", ActorRole: "administrator", Operation: "apply", Reason: "system snapshot fixture",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`3`), Configured: true}},
	}); err != nil {
		t.Fatal(err)
	}
	projectScope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "owner-repo"}
	if _, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: projectScope, ExpectedVersion: 0, ActorID: "admin", ActorRole: "administrator",
		Operation: "apply", Reason: "project snapshot fixture",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`5`), Configured: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfigurationRegistry(registry); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job-automatic-config", ProjectID: "owner-repo", Repository: "owner/repo",
		Task: "prove atomic configuration", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	first := readJobSnapshotDocument(t, store, job.ID)
	if got := string(first.Values["workflow.max_review_cycles"].Value); got != "5" {
		t.Fatalf("accepted direct job review cycles = %s, want project override 5", got)
	}
	if len(first.Values) != len(registry.Descriptors()) {
		t.Fatalf("automatic snapshot has %d values, want %d", len(first.Values), len(registry.Descriptors()))
	}

	if _, _, err := store.ApplyConfigScope(ctx, appconfig.ApplyScopeRequest{
		Scope: projectScope, ExpectedVersion: 1, ActorID: "admin", ActorRole: "administrator",
		Operation: "apply", Reason: "later project change",
		Changes: []appconfig.ScopeChange{{Key: "workflow.max_review_cycles", Value: json.RawMessage(`6`), Configured: true}},
	}); err != nil {
		t.Fatal(err)
	}
	replayed := readJobSnapshotDocument(t, store, job.ID)
	if got := string(replayed.Values["workflow.max_review_cycles"].Value); got != "5" || replayed.SHA256 != first.SHA256 {
		t.Fatalf("later change altered accepted job snapshot: got=%s first=%s replay=%s", got, first.SHA256, replayed.SHA256)
	}

	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{
		ID: "owner-repo", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSchedule(ctx, automation.ScheduleRequest{
		ProjectID: "owner-repo", Name: "configuration snapshot schedule", TaskType: "maintenance",
		Task: "snapshot scheduled configuration", IntervalSeconds: 3600, WindowStartMinute: 0, WindowEndMinute: 0,
		MaxWallSeconds: 600, MaxTokens: 1000, Enabled: true, NextRunAt: clock.Add(-time.Minute),
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	run, err := store.DispatchDueSchedule(ctx, "controller-scheduler")
	if err != nil {
		t.Fatal(err)
	}
	scheduled := readJobSnapshotDocument(t, store, run.JobID)
	if got := string(scheduled.Values["workflow.max_review_cycles"].Value); got != "6" {
		t.Fatalf("scheduled job review cycles = %s, want acceptance-time value 6", got)
	}
}

func readJobSnapshotDocument(t *testing.T, store *Store, jobID string) appconfig.Snapshot {
	t.Helper()
	stored, err := store.GetJobConfigSnapshot(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	var document appconfig.Snapshot
	if err := json.Unmarshal(stored.Document, &document); err != nil {
		t.Fatal(err)
	}
	if document.SHA256 != stored.SHA256 || document.RegistryHash != stored.RegistryHash {
		t.Fatalf("snapshot envelope mismatch: %#v %#v", document, stored)
	}
	return document
}
