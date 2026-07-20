package runnerd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/runners"
)

func TestPolicyConstructsHardenedServerOwnedSpecifications(t *testing.T) {
	policy := testPolicy(t)
	implementation, err := policy.Resolve(runners.JobRequest{
		JobID: "job_123", ProjectID: "owner-repo", Kind: runners.KindImplementation,
		InputArtifactIDs: []string{"artifact_abc"},
	}, "run_123")
	if err != nil {
		t.Fatal(err)
	}
	if implementation.Network != NetworkNone || !implementation.ReadOnlyRoot || implementation.User == "" ||
		!implementation.NoNewPrivileges || !implementation.UseDefaultSeccomp ||
		len(implementation.DropCapabilities) != 1 || implementation.DropCapabilities[0] != "ALL" {
		t.Fatalf("implementation specification is not hardened: %#v", implementation)
	}
	if strings.Contains(implementation.Image, "latest") || !strings.Contains(implementation.Image, "@sha256:") {
		t.Fatalf("image is mutable: %q", implementation.Image)
	}
	for _, mount := range implementation.Mounts {
		if !strings.HasPrefix(mount.Source, "/srv/maintainer/") {
			t.Fatalf("mount escaped data root: %#v", mount)
		}
	}

	dependencies, err := policy.Resolve(runners.JobRequest{
		JobID: "job_456", ProjectID: "owner-repo", Kind: runners.KindDependencies,
	}, "run_456")
	if err != nil {
		t.Fatal(err)
	}
	if dependencies.Network != NetworkDependencyEgress {
		t.Fatalf("dependency phase has network %q", dependencies.Network)
	}
	foundScopedCache := false
	for _, mount := range dependencies.Mounts {
		if mount.Target == "/cache" {
			foundScopedCache = mount.Source == "/srv/maintainer/caches/owner-repo"
		}
	}
	if !foundScopedCache {
		t.Fatalf("dependency cache is not project scoped: %#v", dependencies.Mounts)
	}
}

func TestLoadPolicyFileAcceptsOnlyACompletePrivateImmutableAllowList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	digest := strings.Repeat("b", 64)
	document := `{"schema_version":1,"worker_user":"1000:1000","images":{` +
		`"dependencies":"example/dependencies@sha256:` + digest + `",` +
		`"implementation":"example/implementation@sha256:` + digest + `",` +
		`"verification":"example/verification@sha256:` + digest + `",` +
		`"qc":"example/qc@sha256:` + digest + `"}}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicyFile(path, "/srv/maintainer"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o622); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicyFile(path, "/srv/maintainer"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("group-writable policy returned %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(document, "@sha256:", ":latest", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicyFile(path, "/srv/maintainer"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("mutable policy image returned %v", err)
	}
}

func TestPolicyRejectsTraversalMutableImagesAndUnknownKinds(t *testing.T) {
	policy := testPolicy(t)
	tests := []runners.JobRequest{
		{JobID: "../escape", ProjectID: "project", Kind: runners.KindImplementation},
		{JobID: "job", ProjectID: "project/escape", Kind: runners.KindImplementation},
		{JobID: "job", ProjectID: "project", Kind: runners.Kind("image-from-browser")},
		{JobID: "job", ProjectID: "project", Kind: runners.KindQC, InputArtifactIDs: []string{"../../secret"}},
		{JobID: "job", ProjectID: "project", Kind: runners.KindQC, InputArtifactIDs: []string{"same", "same"}},
	}
	for _, request := range tests {
		if _, err := policy.Resolve(request, "run_safe"); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("unsafe request %#v returned %v", request, err)
		}
	}
	images := testImages()
	images[runners.KindQC] = "example/qc:latest"
	if _, err := NewPolicy("/srv/maintainer", images); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("mutable image returned %v", err)
	}
}

func TestPolicyMakesQCWorktreeReadOnlyAndEveryOtherPhaseOfflineExceptDependencies(t *testing.T) {
	policy := testPolicy(t)
	for _, kind := range []runners.Kind{runners.KindImplementation, runners.KindVerification, runners.KindQC} {
		spec, err := policy.Resolve(runners.JobRequest{JobID: "job", ProjectID: "project", Kind: kind}, runners.RunID("run_"+string(kind)))
		if err != nil {
			t.Fatal(err)
		}
		if spec.Network != NetworkNone {
			t.Fatalf("%s unexpectedly has network %s", kind, spec.Network)
		}
		if kind == runners.KindQC && !spec.Mounts[0].ReadOnly {
			t.Fatal("QC worktree is writable")
		}
	}
}

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	policy, err := NewPolicy("/srv/maintainer", testImages())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func testImages() map[runners.Kind]string {
	digest := strings.Repeat("a", 64)
	return map[runners.Kind]string{
		runners.KindDependencies:   "example/dependencies@sha256:" + digest,
		runners.KindImplementation: "example/implementation@sha256:" + digest,
		runners.KindVerification:   "example/verification@sha256:" + digest,
		runners.KindQC:             "example/qc@sha256:" + digest,
	}
}
