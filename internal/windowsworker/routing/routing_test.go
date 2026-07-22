package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker/simulator"
)

type tokenFixture []byte

func (t tokenFixture) ResolveWindowsWorkerToken(context.Context, string) ([]byte, error) {
	return t, nil
}

type remoteFixture struct{ now time.Time }

func (f remoteFixture) ProbeWindowsWorker(_ context.Context, profile windowsworker.Profile) (windowsworker.Probe, error) {
	return windowsworker.Probe{ProfileID: profile.ID, Ready: true, Mode: "remote", Health: "healthy", Capacity: profile.Capacity, VMTemplateID: profile.VMTemplateID, Toolchains: profile.Toolchains, Problems: []string{}, CheckedAt: f.now}, nil
}

func (f remoteFixture) RunWindowsJob(_ context.Context, profile windowsworker.Profile, request windowsworker.RunRequest) (windowsworker.Result, error) {
	return windowsworker.Result{RunID: "remote-run", ProfileID: profile.ID, ProjectID: request.ProjectID, JobID: request.JobID, JobType: request.JobType, InputSHA256: windowsworker.InputSHA(request), State: "completed", Checks: []windowsworker.Check{}, Artifacts: []windowsworker.Artifact{}, IdempotencyKey: request.IdempotencyKey, StartedAt: f.now, CompletedAt: f.now}, nil
}

func TestRouterUsesAuthenticatedRemoteProtocolWithoutExposingTokenInProfile(t *testing.T) {
	token := tokenFixture("0123456789abcdef0123456789abcdef")
	profile := windowsworker.DefaultSimulatorProfile()
	profile.ID, profile.Name, profile.Mode = "remote-worker", "Remote worker", "remote"
	profile.CredentialReference, profile.CredentialStatus = "remote-main", "configured"
	profile.ManualGates = windowsworker.ManualGates{}
	profile.Endpoint, profile.EndpointAllowlist = "http://127.0.0.1", []string{"http://127.0.0.1"}
	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(mustHandler(t, []byte(token), profile, remoteFixture{now: now}))
	defer server.Close()
	profile.Endpoint, profile.EndpointAllowlist = server.URL, []string{server.URL}

	router, err := New(simulator.New(), token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := router.ProbeWindowsWorker(context.Background(), profile)
	if err != nil || !probe.Ready || probe.Mode != "remote" {
		t.Fatalf("probe %#v error %v", probe, err)
	}
	request := windowsworker.RunRequest{ProfileID: profile.ID, ProjectID: "project", JobID: "job", JobType: windowsworker.JobDotNet, Input: immutableFixture(), IdempotencyKey: "remote-key"}
	result, err := router.RunWindowsJob(context.Background(), profile, request)
	if err != nil || result.RunID != "remote-run" || result.InputSHA256 != windowsworker.InputSHA(request) {
		t.Fatalf("result %#v error %v", result, err)
	}
}

func TestDirectoryTokensRejectsTraversalSymlinksAndOversize(t *testing.T) {
	root := t.TempDir()
	resolver := DirectoryTokens{Root: root}
	if _, err := resolver.ResolveWindowsWorkerToken(context.Background(), "../outside"); err == nil {
		t.Fatal("traversal credential reference accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.token")
	if err := os.WriteFile(outside, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.token")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveWindowsWorkerToken(context.Background(), "linked"); err == nil {
		t.Fatal("symlink credential accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "large.token"), make([]byte, maxTokenBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveWindowsWorkerToken(context.Background(), "large"); err == nil {
		t.Fatal("oversized credential accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "good.token"), []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := resolver.ResolveWindowsWorkerToken(context.Background(), "good")
	if err != nil || string(token) != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("token %q error %v", token, err)
	}
}

func mustHandler(t *testing.T, token []byte, profile windowsworker.Profile, adapter windowsworker.Adapter) http.Handler {
	t.Helper()
	handler, err := windowsworker.NewProtocolHandler(token, []windowsworker.Profile{profile}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func immutableFixture() windowsworker.ImmutableInput {
	return windowsworker.ImmutableInput{RepositorySHA: "0123456789abcdef0123456789abcdef01234567", CapabilityPackChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ToolchainInventoryChecksum: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SourceArtifactID: "source"}
}
