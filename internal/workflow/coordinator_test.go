package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/agents"
	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/models"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
	"github.com/local-code-maintainer/appliance/internal/verification"
)

type fixtureExecution struct {
	worktrees string
}

func (f fixtureExecution) PrepareDependencies(context.Context, jobs.Job) error { return nil }

func (f fixtureExecution) Implement(_ context.Context, job jobs.Job, packet agents.TaskPacket) (agents.ImplementationResult, error) {
	var edit agents.Edit
	for _, file := range packet.RelevantFiles {
		updated := file.Content
		if packet.Mode == "implementation" {
			updated = strings.Replace(updated, "return a - b", "return a + b", 1)
		} else if strings.HasSuffix(file.Path, "_test.go") && !strings.Contains(file.Content, "TestAddNegative") {
			updated += "\nfunc TestAddNegative(t *testing.T) {\n\tif Add(-2, -3) != -5 { t.Fatal(Add(-2, -3)) }\n}\n"
		}
		if updated != file.Content {
			edit = agents.Edit{Path: file.Path, Content: updated, ExpectedSHA256: agents.HashContent([]byte(file.Content))}
			break
		}
	}
	result := agents.ImplementationResult{
		SchemaVersion: 1, Summary: agents.ActionSummary{
			Reproduction: "fixture reproduced", Plan: "minimal repair", Changes: []string{"fixture edit"},
			RegressionTests: []string{"fixture test"}, Checks: []string{"controller gates"}, Evidence: []string{"fixture"},
		}, Edits: []agents.Edit{edit},
	}
	if edit.Path == "" {
		return agents.ImplementationResult{}, os.ErrNotExist
	}
	return result, agents.ApplyEdits(filepath.Join(f.worktrees, job.ID), result.Edits)
}

func (f fixtureExecution) Verify(_ context.Context, job jobs.Job, _ verification.Language, classes []verification.Class, purpose string) (VerificationResult, error) {
	passed := purpose != "reproduction"
	head := job.ResultSHA
	if head == "" {
		head = job.BaseSHA
	}
	results := make([]verification.ExecutionResult, len(classes))
	for index, class := range classes {
		results[index] = verification.ExecutionResult{Class: class}
		if !passed {
			results[index].ExitCode = 1
		}
	}
	return VerificationResult{
		SchemaVersion: 1, Passed: passed, Scan: &verification.ScanResult{HeadSHA: head, PatchSHA256: strings.Repeat("a", 64)},
		Results: results,
	}, nil
}

func (f fixtureExecution) Review(_ context.Context, job jobs.Job, packet agents.TaskPacket) (agents.QCReport, error) {
	report := agents.QCReport{
		SchemaVersion: 1, JobID: job.ID, BaseSHA: job.BaseSHA, ResultSHA: job.ResultSHA,
		Verdict: "pass", Findings: []agents.Finding{}, VerificationRequests: []agents.VerificationRequest{},
	}
	if job.ReviewCycle == 0 {
		report.Verdict = "blocking_findings"
		report.Findings = []agents.Finding{{
			ID: "QC-FIXTURE-001", Severity: "must_fix", Category: "regression_coverage",
			Claim: "negative coverage is missing", Location: agents.Location{Path: "answer_test.go", Line: 1},
			Evidence: "the negative-input assertion is absent", RequiredResolution: "add a negative-input assertion",
			VerificationMethod: "full_tests",
		}}
	}
	payload, _ := json.Marshal(report)
	return agents.DecodeQCReport(payload, packet)
}

func TestCoordinatorCompletesImplementRejectRepairApproveAndLocalPublish(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remotes := filepath.Join(root, "remotes")
	if err := os.MkdirAll(remotes, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "seed")
	fixtureGit(t, "init", "-b", "main", seed)
	fixtureGitAt(t, seed, "config", "user.name", "Fixture")
	fixtureGitAt(t, seed, "config", "user.email", "fixture@localhost")
	writeFixture(t, filepath.Join(seed, "go.mod"), "module fixture.local/arithmetic\n\ngo 1.25\n")
	writeFixture(t, filepath.Join(seed, "answer.go"), "package answer\n\nfunc Add(a, b int) int { return a - b }\n")
	writeFixture(t, filepath.Join(seed, "answer_test.go"), "package answer\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 { t.Fatal(Add(2, 3)) }\n}\n")
	fixtureGitAt(t, seed, "add", ".")
	fixtureGitAt(t, seed, "commit", "-m", "seed defect")
	fixtureGit(t, "clone", "--bare", seed, filepath.Join(remotes, "fixture.git"))

	store, err := storesqlite.Open(ctx, filepath.Join(root, "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{
		ID: "fixture", Provider: "local", Repository: "fixture/arithmetic",
		DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_e2e", ProjectID: "fixture", Repository: "fixture/arithmetic", Task: "repair Add", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifactfiles.New(filepath.Join(root, "artifacts"), store)
	if err != nil {
		t.Fatal(err)
	}
	git, err := gitbridge.NewManager(filepath.Join(root, "mirrors"), filepath.Join(root, "worktrees"), remotes)
	if err != nil {
		t.Fatal(err)
	}
	modelManager := models.NewFake([]models.Profile{
		{ID: "implementation", Role: "implementation", ModelFamily: "qwen", Context: 32768},
		{ID: "qc", Role: "qc", ModelFamily: "mistral", Context: 32768},
	})
	coordinator, err := NewCoordinator(store, git, modelManager, fixtureExecution{worktrees: filepath.Join(root, "worktrees")},
		artifacts, filepath.Join(root, "worktrees"), 2)
	if err != nil {
		t.Fatal(err)
	}
	engine := New(store, coordinator)
	for steps := 0; steps < 30; steps++ {
		job, err = engine.Step(ctx, job.ID)
		if err != nil {
			t.Fatalf("step %d from %s: %v", steps, job.State, err)
		}
		if job.State == jobs.StateAwaitingOperator {
			break
		}
	}
	if job.State != jobs.StateAwaitingOperator || job.ReviewCycle != 1 || job.BaseSHA == "" || job.ResultSHA == "" {
		t.Fatalf("job did not reach reviewed approval gate: %#v", job)
	}
	storedFindings, err := store.ListFindings(ctx, job.ID)
	if err != nil || len(storedFindings) != 1 || storedFindings[0].Status != findings.StatusClosed {
		t.Fatalf("finding lifecycle = %#v, %v", storedFindings, err)
	}
	job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: jobs.StatePublishingBranch, ActorID: "reviewer", Reason: "approved local draft publication",
		ExpectedVersion: job.Version, Details: json.RawMessage(`{"reauthenticated":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Step(ctx, job.ID)
	if err != nil || job.State != jobs.StateDraftPRCreated {
		t.Fatalf("publication step = %#v, %v", job, err)
	}
	job, err = engine.Step(ctx, job.ID)
	if err != nil || job.State != jobs.StateCompleted {
		t.Fatalf("completion step = %#v, %v", job, err)
	}
	published := strings.TrimSpace(fixtureGit(t, "--git-dir="+filepath.Join(remotes, "fixture.git"), "rev-parse", "refs/heads/maintainer/job_e2e"))
	if published != job.ResultSHA {
		t.Fatalf("published SHA = %s, want %s", published, job.ResultSHA)
	}
	phaseRecords, err := store.ListPhaseRecords(ctx, job.ID, 100)
	if err != nil || len(phaseRecords) < 18 {
		t.Fatalf("phase records = %d, %v", len(phaseRecords), err)
	}
	jobArtifacts, err := store.ListJobArtifacts(ctx, job.ID, 100)
	if err != nil || len(jobArtifacts) < 2 {
		t.Fatalf("job artifacts = %#v, %v", jobArtifacts, err)
	}
	records, err := store.ListMemory(ctx, memory.ProjectScope{Owner: "fixture", Repository: "arithmetic"}, memory.StatusQuarantine, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("verified memory records = %#v, %v", records, err)
	}
	if records[0].Kind != "verified_case" || records[0].Verified || records[0].BaseCommit != job.BaseSHA || !strings.HasPrefix(records[0].SourceURI, "artifact://") {
		t.Fatalf("verified memory record = %#v", records[0])
	}
}

func fixtureGit(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	payload, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, payload)
	}
	return string(payload)
}

func fixtureGitAt(t *testing.T, directory string, args ...string) string {
	t.Helper()
	return fixtureGit(t, append([]string{"-C", directory}, args...)...)
}

func writeFixture(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
