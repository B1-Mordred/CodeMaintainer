package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker/simulator"
)

func TestWindowsWorkerProfileAndIdempotentSimulatorRunSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{ID: "windows-project", Provider: "local", Repository: "fixture/windows-project", DefaultBranch: "main", LocalRemoteName: "windows-project.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	service, _ := windowsworker.NewService(store, simulator.New())
	profile := windowsProfileFixture()
	created, err := service.SaveProfile(ctx, windowsworker.SaveProfileRequest{Profile: profile, ActorID: "admin", Reason: "configure deterministic simulator"})
	if err != nil || created.Revision != 1 {
		t.Fatalf("profile = %#v err=%v", created, err)
	}
	request := windowsworker.RunRequest{
		ProfileID: profile.ID, ProjectID: "windows-project", JobID: "windows-job", JobType: windowsworker.JobInstallerEvidence,
		IdempotencyKey: "windows-restart-run", ActorID: "operator", ActorRole: "operator",
		Input: windowsworker.ImmutableInput{
			RepositorySHA: strings.Repeat("a", 40), CapabilityPackChecksum: strings.Repeat("b", 64), ToolchainInventoryChecksum: strings.Repeat("c", 64), SourceArtifactID: "source-windows-project",
		},
	}
	first, err := service.Run(ctx, request)
	if err != nil || first.Replay || len(first.Artifacts) != 1 {
		t.Fatalf("first run = %#v err=%v", first, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, _ = windowsworker.NewService(store, simulator.New())
	replayed, err := service.Run(ctx, request)
	if err != nil || !replayed.Replay || replayed.RunID != first.RunID || replayed.InputSHA256 != first.InputSHA256 {
		t.Fatalf("restart replay = %#v err=%v", replayed, err)
	}
	runs, err := service.Runs(ctx, profile.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %#v err=%v", runs, err)
	}
}

func windowsProfileFixture() windowsworker.Profile {
	return windowsworker.Profile{
		ID: "windows-simulator", Name: "Windows simulator", Mode: "simulator",
		Endpoint: "simulator://windows-worker", EndpointAllowlist: []string{"simulator://windows-worker"}, CredentialStatus: "not_required", Health: "healthy",
		Capacity: 2, VMTemplateID: "windows-2022-sim-v1", Toolchains: map[string]string{"dotnet-sdk": "8.0.302", "powershell": "7.4.4", "pester": "5.6.1", "inno-setup": "6.3.3"},
		AllowedJobTypes: append([]string(nil), windowsworker.ApprovedJobTypes...), TimeoutSeconds: 1800, SimulatorProfileIDs: []string{"instrument-sim-v1"}, ArtifactRetentionDays: 30, Enabled: true,
	}
}
