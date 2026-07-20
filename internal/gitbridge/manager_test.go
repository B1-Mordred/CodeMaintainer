package gitbridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalManagerSyncsCommitsDiffsAndPublishesIdempotently(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remotes := filepath.Join(root, "remotes")
	if err := os.MkdirAll(remotes, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "seed")
	runGit(t, "init", "-b", "main", seed)
	runGitAt(t, seed, "config", "user.name", "Fixture")
	runGitAt(t, seed, "config", "user.email", "fixture@localhost")
	if err := os.WriteFile(filepath.Join(seed, "answer.go"), []byte("package answer\n\nfunc Add(a, b int) int { return a - b }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, seed, "add", "answer.go")
	runGitAt(t, seed, "commit", "-m", "seed defect")
	runGit(t, "clone", "--bare", seed, filepath.Join(remotes, "fixture.git"))

	manager, err := NewManager(filepath.Join(root, "mirrors"), filepath.Join(root, "worktrees"), remotes)
	if err != nil {
		t.Fatal(err)
	}
	registration := Registration{
		ProjectID: "fixture", Provider: "local", Repository: "fixture/arithmetic",
		DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}
	if err := manager.Register(ctx, registration); err != nil {
		t.Fatal(err)
	}
	synced, err := manager.Sync(ctx, "fixture")
	if err != nil || !commit.MatchString(synced.BaseSHA) {
		t.Fatalf("sync = %#v, %v", synced, err)
	}
	snapshot, err := manager.Snapshot(ctx, "fixture", synced.BaseSHA)
	if err != nil || snapshot.Repository != registration.Repository || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "answer.go" || !strings.Contains(string(snapshot.Files[0].Content), "func Add") {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	worktree, err := manager.CreateWorktree(ctx, WorktreeRequest{
		ProjectID: "fixture", JobID: "job_fixture", BaseSHA: synced.BaseSHA,
	})
	if err != nil || worktree.Branch != "maintainer/job_fixture" {
		t.Fatalf("worktree = %#v, %v", worktree, err)
	}
	worktreePath := filepath.Join(root, "worktrees", "job_fixture")
	if err := os.WriteFile(filepath.Join(worktreePath, "answer.go"), []byte("package answer\n\nfunc Add(a, b int) int { return a + b }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitRequest := CommitRequest{
		ProjectID: "fixture", JobID: "job_fixture", ExpectedHead: synced.BaseSHA, OperationID: "phase_implement",
	}
	committed, err := manager.Commit(ctx, commitRequest)
	if err != nil || committed.ResultSHA == synced.BaseSHA {
		t.Fatalf("commit = %#v, %v", committed, err)
	}
	repeated, err := manager.Commit(ctx, commitRequest)
	if err != nil || repeated.ResultSHA != committed.ResultSHA {
		t.Fatalf("repeated commit = %#v, %v", repeated, err)
	}
	diff, err := manager.Diff(ctx, DiffRequest{
		ProjectID: "fixture", JobID: "job_fixture", BaseSHA: synced.BaseSHA, ResultSHA: committed.ResultSHA,
	})
	if err != nil || !strings.Contains(diff.Patch, "return a + b") {
		t.Fatalf("diff = %q, %v", diff.Patch, err)
	}
	publication, err := manager.Publish(ctx, PublishRequest{
		ProjectID: "fixture", JobID: "job_fixture", BaseSHA: synced.BaseSHA, ResultSHA: committed.ResultSHA,
	})
	if err != nil || !publication.Draft || publication.Branch != "maintainer/job_fixture" {
		t.Fatalf("publication = %#v, %v", publication, err)
	}
	repeatedPublication, err := manager.Publish(ctx, PublishRequest{
		ProjectID: "fixture", JobID: "job_fixture", BaseSHA: synced.BaseSHA, ResultSHA: committed.ResultSHA,
	})
	if err != nil || repeatedPublication.ExternalID != publication.ExternalID {
		t.Fatalf("repeated publication = %#v, %v", repeatedPublication, err)
	}
	remoteHead := strings.TrimSpace(runGit(t, "--git-dir="+filepath.Join(remotes, "fixture.git"), "rev-parse", "refs/heads/maintainer/job_fixture"))
	if remoteHead != committed.ResultSHA {
		t.Fatalf("published head = %s, want %s", remoteHead, committed.ResultSHA)
	}
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	payload, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, payload)
	}
	return string(payload)
}

func runGitAt(t *testing.T, directory string, args ...string) string {
	t.Helper()
	return runGit(t, append([]string{"-C", directory}, args...)...)
}
