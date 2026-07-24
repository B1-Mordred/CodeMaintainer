package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	artifactfiles "github.com/B1-Mordred/CodeMaintainer/internal/artifacts"
	"github.com/B1-Mordred/CodeMaintainer/internal/backup"
	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/models"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
	"github.com/B1-Mordred/CodeMaintainer/internal/workflow"
)

func TestLocalFakeBrowserToPublicationAcceptancePreservesEvidenceThroughRestore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	databasePath := filepath.Join(dataRoot, "database", "controller.db")
	remotesRoot := filepath.Join(dataRoot, "remotes")
	if err := os.MkdirAll(remotesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	seedArithmeticRemote(t, root, remotesRoot)

	store, configService, engine := openLocalFakeController(t, ctx, dataRoot, databasePath, remotesRoot)
	job := runLocalFakePreflight(t, ctx, store, configService)
	runLocalFakeProviderGateway(t, ctx, store)
	job = runLocalFakeWorkflowToOperatorGate(t, ctx, store, engine, job.ID)

	backupManager, err := backup.New(dataRoot, "mock", store, "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := backupManager.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	report, err := backupManager.Validate(ctx, record.ID)
	if err != nil || !report.Compatible || !report.ChecksumsValid || report.Files == 0 {
		t.Fatalf("backup validation = %#v, %v", report, err)
	}
	staged, err := backupManager.StageRestore(ctx, record.ID)
	if err != nil || !staged.Staged || !staged.RequiresRestart {
		t.Fatalf("stage restore = %#v, %v", staged, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	applied, err := backup.ApplyPending(dataRoot)
	if err != nil || !applied {
		t.Fatalf("apply pending restore = %v, %v", applied, err)
	}

	reopened, _, restoredEngine := openLocalFakeController(t, ctx, dataRoot, databasePath, remotesRoot)
	defer reopened.Close()
	restoredJob, err := reopened.GetJob(ctx, job.ID)
	if err != nil || restoredJob.State != jobs.StateAwaitingOperator || restoredJob.ResultSHA == "" {
		t.Fatalf("restored job = %#v, %v", restoredJob, err)
	}
	assignments, err := reopened.ListCapabilityAssignments(ctx, "fixture", 10)
	if err != nil || len(assignments) != 1 || assignments[0].PackID != "php83-intranet" || !assignments[0].Enabled {
		t.Fatalf("restored capability assignment = %#v, %v", assignments, err)
	}
	manifests, err := reopened.ListEgressManifests(ctx, "fixture", 10)
	if err != nil || len(manifests) < 3 {
		t.Fatalf("restored provider manifests = %#v, %v", manifests, err)
	}

	restoredJob, err = reopened.TransitionJob(ctx, restoredJob.ID, jobs.TransitionRequest{
		To: jobs.StatePublishingBranch, ActorID: "operator", Reason: "browser-approved restored local-fake publication",
		ExpectedVersion: restoredJob.Version, Details: json.RawMessage(`{"reauthenticated":true,"source":"local_fake_acceptance"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []jobs.State{jobs.StateDraftPRCreated, jobs.StateCompleted} {
		restoredJob, err = restoredEngine.Step(ctx, restoredJob.ID)
		if err != nil || restoredJob.State != want {
			t.Fatalf("resume publication to %s = %#v, %v", want, restoredJob, err)
		}
	}
	published := strings.TrimSpace(runGit(t, "--git-dir="+filepath.Join(remotesRoot, "fixture.git"), "rev-parse", "refs/heads/maintainer/job_local_fake_e2e"))
	if published != restoredJob.ResultSHA {
		t.Fatalf("published SHA = %s, want %s", published, restoredJob.ResultSHA)
	}
	graph, err := reopened.ListJobEvidenceGraph(ctx, restoredJob.ID)
	if err != nil || graph.JobID != restoredJob.ID || len(graph.Nodes) < 2 || len(graph.Edges) < 1 {
		t.Fatalf("restored evidence graph = %#v, %v", graph, err)
	}
	phases, err := reopened.ListPhaseRecords(ctx, restoredJob.ID, 100)
	if err != nil || len(phases) < 22 {
		t.Fatalf("restored phase records = %d, %v", len(phases), err)
	}
}

func runLocalFakePreflight(t *testing.T, ctx context.Context, store *storesqlite.Store, configService *appconfig.RegistryService) jobs.Job {
	t.Helper()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{
		ID: "fixture", Provider: "local", Repository: "fixture/arithmetic", DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}, "browser-operator"); err != nil {
		t.Fatal(err)
	}
	capabilityService, err := capabilities.NewService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = capabilityService.Transition(ctx, capabilities.TransitionRequest{
		PackID: "php83-intranet", Action: "install", TargetVersion: "1.0.0", ActorID: "admin", Reason: "browser reviewed trusted php fixture pack",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := capabilityService.Scan(ctx, capabilities.ScanInput{
		ProjectID: "fixture", Repository: "fixture/arithmetic", Revision: strings.Repeat("a", 40),
		Files: []capabilities.SourceFile{
			{Path: "composer.json", Content: []byte(`{"require":{"php":"^8.3"}}`)},
			{Path: "public/index.php", Content: []byte("<?php echo 'ok';\n")},
			{Path: "package-lock.json", Content: []byte(`{"lockfileVersion":3}`)},
			{Path: "vite.config.ts", Content: []byte("export default {}\n")},
			{Path: "tailwind.config.ts", Content: []byte("export default {}\n")},
		},
	}, "browser-operator")
	if err != nil {
		t.Fatal(err)
	}
	var proposal capabilities.Proposal
	for _, candidate := range scan.Proposals {
		if candidate.Key == "php83-intranet" {
			proposal = candidate
		}
	}
	if proposal.ID == "" {
		t.Fatalf("repo doctor proposals omitted php83 pack: %#v", scan.Proposals)
	}
	reviewed, assignment, err := capabilityService.Review(ctx, capabilities.ReviewRequest{
		ProjectID: scan.ProjectID, ScanID: scan.ID, ProposalID: proposal.ID, ExpectedVersion: proposal.Version,
		Accept: true, ActorID: "browser-operator", Reason: "browser accepted Repo Doctor php fixture proposal", Config: json.RawMessage(`{}`),
	})
	if err != nil || reviewed.State != "accepted" || assignment == nil || !assignment.Enabled {
		t.Fatalf("repo doctor review = %#v assignment=%#v err=%v", reviewed, assignment, err)
	}
	if _, err = capabilityService.PreviewAssignmentConfiguration(ctx, "fixture", "php83-intranet", assignment.Revision, assignment.Config); err != nil {
		t.Fatal(err)
	}

	scope := appconfig.ScopeRef{Kind: appconfig.ScopeProject, ID: "fixture"}
	first := applyLocalFakeConfigDraft(t, ctx, configService, scope, 0, 4)
	second := applyLocalFakeConfigDraft(t, ctx, configService, scope, 1, 5)
	if _, _, err = configService.Rollback(ctx, first.ID, 2, "browser-operator", "administrator", "browser rolled configuration back after API and CLI parity inspection", false); err != nil {
		t.Fatal(err)
	}
	effective, err := configService.Effective(ctx, []appconfig.ScopeRef{{Kind: appconfig.ScopeSystem}, scope})
	if err != nil {
		t.Fatal(err)
	}
	if string(effective.Values["workflow.max_review_cycles"].Value) != "4" {
		t.Fatalf("rollback did not restore first project value after %s: %#v", second.ID, effective.Values["workflow.max_review_cycles"])
	}

	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_local_fake_e2e", ProjectID: "fixture", Repository: "fixture/arithmetic", Task: "repair Add auth regression", ActorID: "browser-operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.GetJobConfigSnapshot(ctx, job.ID)
	if err != nil || snapshot.SHA256 == "" {
		t.Fatalf("job configuration snapshot = %#v, %v", snapshot, err)
	}
	return job
}

func runLocalFakeProviderGateway(t *testing.T, ctx context.Context, store *storesqlite.Store) {
	t.Helper()
	providerService := providers.NewService(store)
	status, err := providerService.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range status.Providers {
		if profile.Remote && profile.Enabled {
			t.Fatalf("remote provider defaulted enabled: %#v", profile)
		}
	}
	for _, profile := range status.Providers {
		if profile.ID == "fake-openai-responses" {
			profile.Enabled = true
			if _, err := store.UpsertProviderProfile(ctx, profile, "browser-operator"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, route := range status.Routes {
		if route.ID == "remote-documentation-ci-preview" {
			route.Enabled = true
			route.MaxCostUSD = 1
			route.RetryBudget = 1
			route.BatchPolicy = "project_isolated"
			if _, err := store.UpsertRouteProfile(ctx, route, "browser-operator"); err != nil {
				t.Fatal(err)
			}
		}
	}
	allowed, err := providerService.SimulateExecution(ctx, providers.ExecutionRequest{
		Route: providers.RouteRequest{
			JobID: "job_local_fake_e2e", ProjectID: "fixture", Role: "documentation", Purpose: "browser approved fake remote documentation route",
			DataClasses: []string{"task_metadata", "documentation_public_source"}, EstimatedBytes: 2048, EstimatedTokens: 2000,
			RequiresStructuredOutput: true,
		},
		Scenario: providers.ExecutionScenario{Behavior: providers.ExecutionBehaviorSuccess},
	}, "browser-operator")
	if err != nil || allowed.Status != providers.ExecutionStatusSucceeded || allowed.NetworkContacted || allowed.EgressManifestSHA256 == "" {
		t.Fatalf("allowed fake provider execution = %#v, %v", allowed, err)
	}
	forbidden, err := providerService.SimulateExecution(ctx, providers.ExecutionRequest{
		Route: providers.RouteRequest{
			JobID: "job_local_fake_e2e", ProjectID: "fixture", Role: "documentation", Purpose: "forbidden remote data boundary",
			DataClasses: []string{"memory_excerpt"}, EstimatedBytes: 1024, EstimatedTokens: 1000, RequiresStructuredOutput: true,
		},
		Scenario: providers.ExecutionScenario{Behavior: providers.ExecutionBehaviorSuccess},
	}, "browser-operator")
	if err != nil || forbidden.Status != providers.ExecutionStatusDenied || forbidden.FailureClass != providers.ExecutionFailureRouteDenied {
		t.Fatalf("forbidden data route = %#v, %v", forbidden, err)
	}
	lowerTrustFallback, err := providerService.SimulateBatchExecution(ctx, providers.BatchExecutionRequest{Items: []providers.ExecutionRequest{
		{
			Route: providers.RouteRequest{
				JobID: "job_local_fake_e2e", ProjectID: "fixture", Role: "documentation", Purpose: "first provider batch item",
				DataClasses: []string{"task_metadata"}, EstimatedBytes: 512, EstimatedTokens: 500, RequiresStructuredOutput: true,
			},
			Scenario: providers.ExecutionScenario{Behavior: providers.ExecutionBehaviorSuccess},
		},
		{
			Route: providers.RouteRequest{
				JobID: "job_local_fake_e2e", ProjectID: "fixture", Role: "documentation", Purpose: "batch timeout partial failure",
				DataClasses: []string{"task_metadata"}, EstimatedBytes: 512, EstimatedTokens: 500, RequiresStructuredOutput: true,
			},
			Scenario: providers.ExecutionScenario{Behavior: providers.ExecutionBehaviorTimeout},
		},
	}}, "browser-operator")
	if err != nil || lowerTrustFallback.Status != providers.ExecutionStatusPartialFailure || lowerTrustFallback.PartialFailures != 1 {
		t.Fatalf("batch partial failure = %#v, %v", lowerTrustFallback, err)
	}
}

func runLocalFakeWorkflowToOperatorGate(t *testing.T, ctx context.Context, store *storesqlite.Store, engine *workflow.Engine, jobID string) jobs.Job {
	t.Helper()
	job := stepUntilState(t, ctx, engine, jobID, jobs.StateAwaitingTaskApproval, 30)
	contract, err := store.GetTaskContract(ctx, job.ID)
	if err != nil || contract.Status != "draft" || contract.ContractSHA256 == "" {
		t.Fatalf("task contract = %#v, %v", contract, err)
	}
	approved, err := store.ApproveTaskContract(ctx, taskcontract.ApprovalRequest{
		JobID: job.ID, ExpectedVersion: contract.Version, ActorID: "browser-operator", ActorRole: "operator",
		Reason: "browser approved exact local-fake contract",
	})
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approve contract = %#v, %v", approved, err)
	}
	job = stepUntilState(t, ctx, engine, job.ID, jobs.StateAwaitingTestDesignDisposition, 30)
	testReports, err := store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil || len(testReports) != 1 {
		t.Fatalf("test designer reports = %#v, %v", testReports, err)
	}
	if _, err := store.SaveTestDesignerDisposition(ctx, testdesigner.Disposition{
		ReportID: testReports[0].ID, ProposalID: "TD-FIXTURE-001", Disposition: "accepted",
		Reason: "browser accepted independent Test Designer proposal", ActorID: "browser-reviewer", ActorRole: "reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: jobs.StateGoldenRehearsalReview, ActorID: "browser-reviewer",
		Reason:          "all local-fake Test Designer proposals were reviewed",
		ExpectedVersion: job.Version, Details: json.RawMessage(`{"source":"local_fake_acceptance"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	job = stepUntilState(t, ctx, engine, job.ID, jobs.StateAwaitingOperator, 40)
	if job.ResultSHA == "" || job.BaseSHA == "" || job.ReviewCycle != 1 {
		t.Fatalf("operator gate job = %#v", job)
	}
	return job
}

func stepUntilState(t *testing.T, ctx context.Context, engine *workflow.Engine, jobID string, state jobs.State, limit int) jobs.Job {
	t.Helper()
	var (
		job jobs.Job
		err error
	)
	for step := 0; step < limit; step++ {
		job, err = engine.Step(ctx, jobID)
		if err != nil {
			t.Fatalf("step %d toward %s: %#v, %v", step, state, job, err)
		}
		if job.State == state {
			return job
		}
	}
	t.Fatalf("job did not reach %s after %d steps; last=%#v", state, limit, job)
	return jobs.Job{}
}

func openLocalFakeController(t *testing.T, ctx context.Context, dataRoot, databasePath, remotesRoot string) (*storesqlite.Store, *appconfig.RegistryService, *workflow.Engine) {
	t.Helper()
	store, err := storesqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	document := appconfig.Default(dataRoot)
	after, _ := json.Marshal(document)
	configRevision, err := store.CreateConfigRevision(ctx, appconfig.Revision{
		ActorID: "system", SchemaVersion: 1, Before: json.RawMessage(`{}`), After: after,
		Diff: json.RawMessage(`[]`), ValidationResult: json.RawMessage(`{"valid":true}`), Reason: "local fake bootstrap",
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	registry, err := appconfig.BuiltInRegistry(document)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	configService, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := configService.EnsureSystemScope(ctx, document, configRevision.ID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.SetConfigurationRegistry(registry); err != nil {
		store.Close()
		t.Fatal(err)
	}
	artifactStore, err := artifactfiles.New(filepath.Join(dataRoot, "artifacts"), store)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	gitManager, err := gitbridge.NewManager(filepath.Join(dataRoot, "mirrors"), filepath.Join(dataRoot, "worktrees"), remotesRoot)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if project, projectErr := store.GetProject(ctx, "fixture"); projectErr == nil {
		if err := gitManager.Register(ctx, gitbridge.Registration{
			ProjectID: project.ID, Provider: project.Provider, Repository: project.Repository,
			DefaultBranch: project.DefaultBranch, LocalRemoteName: project.LocalRemoteName,
		}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	modelManager := models.NewFake([]models.Profile{
		{ID: "implementation", Role: "implementation", ModelFamily: "qwen", Context: 32768},
		{ID: "test_designer", Role: "test_designer", ModelFamily: "gemma", Context: 32768},
		{ID: "documentation", Role: "documentation", ModelFamily: "llama", Context: 32768},
		{ID: "qc", Role: "qc", ModelFamily: "mistral", Context: 32768},
	})
	execution, err := workflow.NewDeterministicBackend(filepath.Join(dataRoot, "worktrees"))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	coordinator, err := workflow.NewCoordinator(store, gitManager, modelManager, execution, artifactStore, filepath.Join(dataRoot, "worktrees"), 2)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, configService, workflow.New(store, coordinator)
}

func applyLocalFakeConfigDraft(t *testing.T, ctx context.Context, service *appconfig.RegistryService, scope appconfig.ScopeRef, base int64, reviewCycles int) appconfig.RegistryRevision {
	t.Helper()
	draft, report, err := service.CreateDraft(ctx, appconfig.CreateDraftRequest{
		Scope: scope, BaseScopeVersion: base, AuthorID: "browser-operator", Reason: "browser changed workflow review depth",
		Entries: []appconfig.DraftEntry{{Key: "workflow.max_review_cycles", Value: json.RawMessage(strconv.Itoa(reviewCycles)), Configured: true}},
	})
	if err != nil || !report.Valid {
		t.Fatalf("create config draft = %#v, %v", report, err)
	}
	if _, err := service.DryRunDraft(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	draft, report, err = service.ReviewDraft(ctx, appconfig.TransitionDraftRequest{
		ID: draft.ID, ExpectedVersion: draft.Version, ActorID: "browser-reviewer", Target: appconfig.DraftReviewed, Reason: "browser reviewed exact config diff",
	})
	if err != nil || !report.Valid {
		t.Fatalf("review config draft = %#v, %v", report, err)
	}
	revision, _, report, err := service.ApplyDraft(ctx, draft.ID, draft.Version, "browser-reviewer", "administrator", "browser applied reviewed config diff", false)
	if err != nil || !report.Valid {
		t.Fatalf("apply config draft = %#v, %v", report, err)
	}
	return revision
}

func seedArithmeticRemote(t *testing.T, root, remotesRoot string) {
	t.Helper()
	seed := filepath.Join(root, "seed")
	runGit(t, "init", "-b", "main", seed)
	runGitAt(t, seed, "config", "user.name", "Fixture")
	runGitAt(t, seed, "config", "user.email", "fixture@localhost")
	writeTestFile(t, filepath.Join(seed, "go.mod"), "module fixture.local/arithmetic\n\ngo 1.25\n")
	writeTestFile(t, filepath.Join(seed, "answer.go"), "package answer\n\nfunc Add(a, b int) int { return a - b }\n")
	writeTestFile(t, filepath.Join(seed, "answer_test.go"), "package answer\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 { t.Fatal(Add(2, 3)) }\n}\n")
	writeTestFile(t, filepath.Join(seed, "composer.json"), `{"require":{"php":"^8.3"}}`+"\n")
	writeTestFile(t, filepath.Join(seed, "package-lock.json"), `{"lockfileVersion":3}`+"\n")
	writeTestFile(t, filepath.Join(seed, "vite.config.ts"), "export default {}\n")
	writeTestFile(t, filepath.Join(seed, "tailwind.config.ts"), "export default {}\n")
	runGitAt(t, seed, "add", ".")
	runGitAt(t, seed, "commit", "-m", "seed local fake fixture")
	runGit(t, "clone", "--bare", seed, filepath.Join(remotesRoot, "fixture.git"))
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

func writeTestFile(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
