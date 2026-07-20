package verification

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	"github.com/local-code-maintainer/appliance/internal/repositories"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func TestOfflineFixtureChecksOutFixVerifiesAndEmitsStandardArtifacts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, source, "init")
	runVerificationGit(t, source, "config", "user.name", "Fixture")
	runVerificationGit(t, source, "config", "user.email", "fixture@example.invalid")
	writeFixture(t, filepath.Join(source, "go.mod"), []byte("module example.invalid/offline\n\ngo 1.25\n"))
	writeFixture(t, filepath.Join(source, "calc.go"), []byte("package calc\n\nfunc Add(a, b int) int { return a - b }\n"))
	writeFixture(t, filepath.Join(source, "calc_test.go"), []byte("package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(1, 2) != 3 { t.Fatal(\"wrong sum\") } }\n"))
	runVerificationGit(t, source, "add", ".")
	runVerificationGit(t, source, "commit", "-m", "seed defect")
	base := strings.TrimSpace(runVerificationGit(t, source, "rev-parse", "HEAD"))
	mirrors := filepath.Join(root, "mirrors")
	if err := os.MkdirAll(mirrors, 0o700); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, root, "clone", "--bare", source, filepath.Join(mirrors, "fixture.git"))
	manager, err := repositories.NewWorktreeManager(mirrors, filepath.Join(root, "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := manager.Create(ctx, "fixture", "job_offline", base)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(worktree.Path, "calc.go"), []byte("package calc\n\nfunc Add(a, b int) int { return a + b }\n"))
	runVerificationGit(t, worktree.Path, "config", "user.name", "Fixture")
	runVerificationGit(t, worktree.Path, "config", "user.email", "fixture@example.invalid")
	runVerificationGit(t, worktree.Path, "add", "calc.go")
	runVerificationGit(t, worktree.Path, "commit", "-m", "fix addition")

	metadata, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	if _, err := metadata.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_offline", ProjectID: "fixture", Repository: "local/fixture", Task: "fix addition", ActorID: "test",
	}); err != nil {
		t.Fatal(err)
	}
	artifactStore, err := artifactfiles.New(filepath.Join(root, "artifacts"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	scanner, _ := NewScanner()
	verifier, err := NewVerifier(NewRegistry(), scanner, LocalExecutor{}, artifactStore)
	if err != nil {
		t.Fatal(err)
	}
	report, err := verifier.Verify(ctx, Request{
		JobID: "job_offline", ProjectID: "fixture", WorktreePath: worktree.Path,
		BaseSHA: base, Language: LanguageGo, Classes: []Class{ClassCompile, ClassFullTests},
		Timeout: 30 * time.Second, MaxLogBytes: 1 << 20, Policy: DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Findings) != 0 || len(report.PatchSHA256) != 64 || len(report.ArtifactIDs) != 6 {
		t.Fatalf("offline verification failed: %#v", report)
	}
	items, err := metadata.ListJobArtifacts(ctx, "job_offline", 20)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]bool)
	for _, item := range items {
		kinds[item.Kind] = true
		_, reader, openErr := artifactStore.Open(ctx, "job_offline", item.ID)
		if openErr != nil {
			t.Fatal(openErr)
		}
		reader.Close()
	}
	for _, kind := range []string{"command_result", "junit", "sarif", "coverage", "verification_report"} {
		if !kinds[kind] {
			t.Fatalf("missing %s artifact: %#v", kind, items)
		}
	}
	reportArtifact := items[len(items)-1]
	if !json.Valid(reportArtifact.Metadata) {
		t.Fatalf("invalid artifact metadata: %s", reportArtifact.Metadata)
	}
}
