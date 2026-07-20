package repositories

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeManagerCreatesExactOfflineNeverReusedWorktrees(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mirrors := filepath.Join(root, "mirrors")
	worktrees := filepath.Join(root, "worktrees")
	if err := os.MkdirAll(mirrors, 0o700); err != nil {
		t.Fatal(err)
	}
	baseSHA := createBareFixture(t, root, filepath.Join(mirrors, "project.git"))
	manager, err := NewWorktreeManager(mirrors, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(ctx, "project", "job_one", baseSHA)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(created.Path, "fixture.txt"))
	if err != nil || string(payload) != "offline fixture\n" {
		t.Fatalf("unexpected fixture %q, %v", payload, err)
	}
	again, err := manager.Create(ctx, "project", "job_one", baseSHA)
	if err != nil || again.Path != created.Path {
		t.Fatalf("idempotent resume returned %#v, %v", again, err)
	}
	if err := manager.Remove(ctx, "project", "job_one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed worktree remains: %v", err)
	}
	if _, err := manager.Create(ctx, "project", "job_one", baseSHA); !errors.Is(err, ErrWorktreeRetired) {
		t.Fatalf("retired worktree was reused: %v", err)
	}
	second, err := manager.Create(ctx, "project", "job_two", baseSHA)
	if err != nil || second.Path == created.Path {
		t.Fatalf("second job did not get a distinct worktree: %#v, %v", second, err)
	}
}

func TestWorktreeManagerRejectsTraversalWrongSHAAndEscapingMirrorSymlink(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mirrors := filepath.Join(root, "mirrors")
	worktrees := filepath.Join(root, "worktrees")
	if err := os.MkdirAll(mirrors, 0o700); err != nil {
		t.Fatal(err)
	}
	baseSHA := createBareFixture(t, root, filepath.Join(mirrors, "project.git"))
	manager, err := NewWorktreeManager(mirrors, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ project, job, sha string }{
		{"../project", "job", baseSHA}, {"project", "../job", baseSHA},
		{"project", "job", "HEAD"}, {"project", "job", strings.Repeat("f", 40)},
	} {
		if _, err := manager.Create(ctx, request.project, request.job, request.sha); err == nil {
			t.Fatalf("unsafe request %#v was accepted", request)
		}
	}
	outside := filepath.Join(root, "outside.git")
	createBareFixture(t, root, outside)
	if err := os.Symlink(outside, filepath.Join(mirrors, "escape.git")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(ctx, "escape", "job_escape", baseSHA); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping mirror symlink returned %v", err)
	}
}

func createBareFixture(t *testing.T, root, barePath string) string {
	t.Helper()
	source, err := os.MkdirTemp(root, "source-")
	if err != nil {
		t.Fatal(err)
	}
	runGitTest(t, "init", source)
	if err := os.WriteFile(filepath.Join(source, "fixture.txt"), []byte("offline fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, "-C", source, "add", "fixture.txt")
	runGitTest(t, "-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
	sha := strings.TrimSpace(runGitTest(t, "-C", source, "rev-parse", "HEAD"))
	runGitTest(t, "clone", "--bare", source, barePath)
	return sha
}

func runGitTest(t *testing.T, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}
