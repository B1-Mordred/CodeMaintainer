package forges_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/forges"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

type fakeForge struct {
	page forges.SyncPage
}

func (f fakeForge) ProbeForge(_ context.Context, projectID string) (forges.Probe, error) {
	return forges.Probe{ProjectID: projectID, Provider: f.page.Provider, Ready: true, CheckedAt: time.Now().UTC()}, nil
}

func (f fakeForge) SyncForge(_ context.Context, request forges.SyncRequest) (forges.SyncPage, error) {
	page := f.page
	page.ProjectID = request.ProjectID
	page.Cursor = request.Cursor
	for index := range page.Objects {
		if page.Objects[index].ProjectID == "" {
			page.Objects[index].ProjectID = request.ProjectID
		}
	}
	return page, nil
}

func TestProfilesRequireExactEndpointsReauthenticationAndRevision(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	createProject(t, store, "forge-project", "gitlab")
	provider := fakeForge{page: forges.SyncPage{Provider: "gitlab"}}
	service, err := forges.NewService(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	profile := validProfile("forge-project", "gitlab")
	profile.CredentialReference = "gitbridge-secret:gitlab-main"
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, Reason: "configure GitLab", ActorID: "admin"}); err == nil {
		t.Fatal("credential reference creation succeeded without reauthentication")
	}
	request := forges.SaveProfileRequest{Profile: profile, Reason: "configure GitLab", ActorID: "admin", Reauthenticated: true}
	created, err := service.SaveProfile(ctx, request)
	if err != nil || created.Revision != 1 {
		t.Fatalf("create profile = %#v, %v", created, err)
	}
	profile.Revision = created.Revision
	profile.PollingMinutes = 10
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, ExpectedRevision: 0, Reason: "stale update", ActorID: "admin"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	profile.Endpoint = "https://attacker.invalid"
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, ExpectedRevision: 1, Reason: "bad endpoint", ActorID: "admin"}); err == nil {
		t.Fatal("endpoint outside exact allow-list was accepted")
	}
}

func TestSyncPersistsNormalizedMetadataAndReplaysIdempotently(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	createProject(t, store, "local-project", "local")
	metadata := json.RawMessage(`{"local_ref":"refs/heads/main"}`)
	provider := fakeForge{page: forges.SyncPage{
		Provider: "local", NextCursor: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Objects:      []forges.Object{{Provider: "local", Kind: "branch", ExternalID: "main", Ref: "main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProviderMetadata: metadata}},
		Capabilities: []forges.Capability{{Feature: "repository", Status: forges.Supported}}, RateLimitRemaining: 1,
	}}
	service, _ := forges.NewService(store, provider)
	profile := validProfile("local-project", "local")
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, Reason: "configure local forge", ActorID: "admin"}); err != nil {
		t.Fatal(err)
	}
	first, err := service.Sync(ctx, "local-project", "", "sync-local-1", "operator")
	if err != nil || first.Objects != 1 || first.Replay {
		t.Fatalf("first sync = %#v, %v", first, err)
	}
	replay, err := service.Sync(ctx, "local-project", "", "sync-local-1", "operator")
	if err != nil || !replay.Replay || replay.ID != first.ID {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	objects, err := service.Objects(ctx, "local-project", "branch")
	if err != nil || len(objects) != 1 || string(objects[0].ProviderMetadata) != string(metadata) {
		t.Fatalf("objects = %#v, %v", objects, err)
	}
	provider.page.Provider = "github"
	secondService, _ := forges.NewService(store, provider)
	if _, err := secondService.Sync(ctx, "local-project", "", "sync-local-1", "operator"); err == nil {
		t.Fatal("idempotency key reuse with changed provider succeeded")
	}
}

func TestProviderOutputIsBoundedAndNamespaceChecked(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	createProject(t, store, "github-project", "github")
	provider := fakeForge{page: forges.SyncPage{Provider: "github", RateLimitRemaining: 1, Objects: []forges.Object{{ProjectID: "other", Provider: "github", Kind: "issue", ExternalID: "1", ProviderMetadata: json.RawMessage(`{}`)}}}}
	service, _ := forges.NewService(store, provider)
	profile := validProfile("github-project", "github")
	profile.CredentialReference = "gitbridge-secret:github-app"
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, Reason: "configure GitHub", ActorID: "admin", Reauthenticated: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(ctx, "github-project", "", "sync-github-1", "operator"); err == nil {
		t.Fatal("cross-project provider object was accepted")
	}
}

func TestProfileCannotChangeRegisteredProviderOrRepositoryIdentity(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	createProject(t, store, "identity-project", "gitlab")
	service, _ := forges.NewService(store, fakeForge{page: forges.SyncPage{Provider: "gitlab"}})
	profile := validProfile("identity-project", "github")
	profile.CredentialReference = "gitbridge-secret:github-app"
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, Reason: "attempt provider swap", ActorID: "admin", Reauthenticated: true}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("provider swap error = %v", err)
	}
	profile = validProfile("identity-project", "gitlab")
	profile.Repository = "attacker/other"
	profile.CredentialReference = "gitbridge-secret:gitlab-main"
	if _, err := service.SaveProfile(ctx, forges.SaveProfileRequest{Profile: profile, Reason: "attempt repository swap", ActorID: "admin", Reauthenticated: true}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("repository swap error = %v", err)
	}
}

func validProfile(projectID, provider string) forges.Profile {
	endpoint := "https://forge.example.test"
	repository := "fixture/" + projectID
	if provider == "local" {
		endpoint = "local://bare-git"
	}
	return forges.Profile{
		ProjectID: projectID, Provider: provider, Endpoint: endpoint, EndpointAllowlist: []string{endpoint}, Repository: repository,
		CredentialStatus: "configured", WebhookStatus: "polling", SyncDirection: "pull", PollingMinutes: 5,
		BranchConvention: "maintainer/{job_id}", ChangeRequestConvention: "draft", LabelMapping: map[string]string{"defect": "bug"},
		CIArtifactPolicy: "metadata_only", ReleasePolicy: "observe", SubmodulesEnabled: true, Enabled: true,
	}
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createProject(t *testing.T, store *sqlite.Store, id, provider string) {
	t.Helper()
	request := projects.UpsertRequest{ID: id, Provider: provider, Repository: "fixture/" + id, DefaultBranch: "main"}
	if provider == "local" {
		request.LocalRemoteName = id + ".git"
	}
	if _, err := store.UpsertProject(context.Background(), request, "admin"); err != nil {
		t.Fatal(err)
	}
}
