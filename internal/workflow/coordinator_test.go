package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	artifactfiles "github.com/B1-Mordred/CodeMaintainer/internal/artifacts"
	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
	"github.com/B1-Mordred/CodeMaintainer/internal/findings"
	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/memory"
	"github.com/B1-Mordred/CodeMaintainer/internal/models"
	"github.com/B1-Mordred/CodeMaintainer/internal/projects"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
	"github.com/B1-Mordred/CodeMaintainer/internal/verification"
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

func (f fixtureExecution) DesignTests(_ context.Context, job jobs.Job, packet agents.TaskPacket) (testdesigner.Report, error) {
	report := testdesigner.Report{
		JobID: job.ID, SchemaVersion: 1, ContractSHA256: packet.ContractSHA256,
		RiskLevel: packet.RiskLevel, ResultSHA: packet.ResultSHA, SourceContext: "independent_test_designer_context_v1",
		Status: "proposed", DispositionsRequired: true,
		Proposals: []testdesigner.Proposal{{
			ID: "TD-FIXTURE-001", Category: "boundary_case", Claim: "negative coverage is required",
			Rationale: "candidate diff changes arithmetic behavior", EvidenceIDs: []string{"candidate_diff"},
			SuggestedTests: []string{"TestAddNegative"}, GoldenRehearsals: []string{}, Disposition: "pending",
		}},
	}
	return report, report.Validate()
}

func (f fixtureExecution) Document(_ context.Context, job jobs.Job, packet agents.TaskPacket) (documentation.Manifest, error) {
	if packet.RiskLevel == "low" {
		return documentation.NoDocumentationRequired(job.ID, job.ProjectID, packet.ContractSHA256, packet.RiskLevel, packet.ResultSHA)
	}
	path := "docs/maintenance-" + job.ID + ".md"
	action := "created"
	target := filepath.Join(f.worktrees, job.ID, path)
	content := "# Maintenance evidence\n\nExact documentation evidence for " + job.ID + "\n"
	if err := os.MkdirAll(filepath.Join(f.worktrees, job.ID, "docs"), 0o700); err != nil {
		return documentation.Manifest{}, err
	}
	if existing, err := os.ReadFile(target); err == nil && string(existing) == content {
		action = "not_changed"
	} else {
		if _, err := os.Stat(target); err == nil {
			action = "updated"
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			return documentation.Manifest{}, err
		}
	}
	status := "changes_applied"
	if action == "not_changed" {
		status = "passed"
	}
	manifest := documentation.Manifest{
		JobID: job.ID, ProjectID: job.ProjectID, SchemaVersion: 1, ContractSHA256: packet.ContractSHA256,
		RiskLevel: packet.RiskLevel, ResultSHA: packet.ResultSHA, SourceContext: "documentation_agent_context_v1",
		PolicyVersion: "documentation-policy-v1",
		Requirements: []documentation.Requirement{{
			ID: "DOC-FIXTURE-001", Document: path, Reason: "medium/high risk fixture requires documentation evidence",
			Source: "risk_router", RenderTargets: []string{"markdown"}, Required: true, Status: "satisfied",
		}},
		Changes: []documentation.Change{{
			Path: path, Action: action, PolicyRule: "risk-documentation",
			SourceOfTruth: "approved_task_contract", LinkedEvidenceIDs: []string{"task_contract", "candidate_diff"},
		}},
		Checks: []documentation.Check{{
			ID: "DOC-CHECK-FIXTURE", Kind: "markdown", Target: path, Status: "passed",
			Summary: "fixture documentation was written",
		}},
		UnsupportedClaims: []documentation.UnsupportedClaim{}, Edits: []documentation.Edit{}, Status: status,
		PolicySummary: "fixture documentation policy required source-controlled evidence",
	}
	return manifest, manifest.Validate()
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
		ID: "job_e2e", ProjectID: "fixture", Repository: "fixture/arithmetic", Task: "repair Add auth regression", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshotSHA := strings.Repeat("c", 64)
	snapshotDocument, _ := json.Marshal(appconfig.Snapshot{SchemaVersion: 1, RegistryHash: strings.Repeat("b", 64), SHA256: snapshotSHA, Values: map[string]appconfig.EffectiveValue{
		"intelligence.indexing_enabled":              {Key: "intelligence.indexing_enabled", Value: json.RawMessage(`true`)},
		"intelligence.index_retention_days":          {Key: "intelligence.index_retention_days", Value: json.RawMessage(`7`)},
		"intelligence.cache_quota_bytes":             {Key: "intelligence.cache_quota_bytes", Value: json.RawMessage(`10485760`)},
		"intelligence.context_input_tokens":          {Key: "intelligence.context_input_tokens", Value: json.RawMessage(`4096`)},
		"intelligence.context_output_reserve_tokens": {Key: "intelligence.context_output_reserve_tokens", Value: json.RawMessage(`1024`)},
		"verification.clean_final_cache_required":    {Key: "verification.clean_final_cache_required", Value: json.RawMessage(`true`)},
	}})
	if _, err := store.SaveJobConfigSnapshot(ctx, appconfig.JobSnapshot{JobID: job.ID, SchemaVersion: 1, RegistryHash: strings.Repeat("b", 64), SHA256: snapshotSHA, Document: snapshotDocument}); err != nil {
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
		{ID: "test_designer", Role: "test_designer", ModelFamily: "gemma", Context: 32768},
		{ID: "documentation", Role: "documentation", ModelFamily: "llama", Context: 32768},
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
		if job.State == jobs.StateAwaitingTaskApproval {
			break
		}
	}
	if job.State != jobs.StateAwaitingTaskApproval {
		t.Fatalf("job did not pause for task contract approval: %#v", job)
	}
	contract, err := store.GetTaskContract(ctx, job.ID)
	if err != nil || contract.Status != "draft" || contract.ContractSHA256 == "" {
		t.Fatalf("task contract = %#v, %v", contract, err)
	}
	assessments, err := store.ListRiskAssessments(ctx, job.ID, 10)
	if err != nil || len(assessments) == 0 {
		t.Fatalf("risk assessments = %#v, %v", assessments, err)
	}
	contract, err = store.ApproveTaskContract(ctx, taskcontract.ApprovalRequest{
		JobID: job.ID, ExpectedVersion: contract.Version, ActorID: "operator", ActorRole: "operator",
		Reason: "approve exact bounded fixture contract",
	})
	if err != nil || contract.Status != "approved" {
		t.Fatalf("approve contract = %#v, %v", contract, err)
	}
	job, err = store.GetJob(ctx, job.ID)
	if err != nil || job.State != jobs.StateLoadingImplementationModel || job.AcceptanceCriteriaHash == "" {
		t.Fatalf("approved job = %#v, %v", job, err)
	}
	for steps := 0; steps < 30; steps++ {
		job, err = engine.Step(ctx, job.ID)
		if err != nil {
			t.Fatalf("post-approval step %d from %s: %v", steps, job.State, err)
		}
		if job.State == jobs.StateAwaitingOperator {
			break
		}
	}
	if job.State != jobs.StateAwaitingOperator || job.ReviewCycle != 1 || job.BaseSHA == "" || job.ResultSHA == "" {
		t.Fatalf("job did not reach reviewed approval gate: %#v", job)
	}
	testReports, err := store.ListTestDesignerReports(ctx, job.ID, 10)
	if err != nil || len(testReports) != 1 || testReports[0].Status != "proposed" || testReports[0].Proposals[0].Disposition != "pending" {
		t.Fatalf("test designer reports = %#v, %v", testReports, err)
	}
	goldenReports, err := store.ListGoldenReports(ctx, job.ID, 10)
	if err != nil || len(goldenReports) != 1 || goldenReports[0].Status != "no_rehearsals" {
		t.Fatalf("golden rehearsal reports = %#v, %v", goldenReports, err)
	}
	docManifests, err := store.ListDocumentationManifests(ctx, job.ID, 10)
	docChangesApplied := false
	for _, manifest := range docManifests {
		if manifest.Status == "changes_applied" && len(manifest.Changes) != 0 {
			docChangesApplied = true
		}
	}
	if err != nil || !docChangesApplied {
		t.Fatalf("documentation manifests = %#v, %v", docManifests, err)
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
	if err != nil || len(phaseRecords) < 22 {
		t.Fatalf("phase records = %d, %v", len(phaseRecords), err)
	}
	manifests, err := store.ListContextManifests(ctx, "fixture", 100)
	if err != nil || len(manifests) < 3 {
		t.Fatalf("context manifests = %#v, %v", manifests, err)
	}
	manifestPayload, _ := json.Marshal(manifests)
	if strings.Contains(string(manifestPayload), "package arithmetic") || manifests[0].BudgetTokens != 4096 || manifests[0].ReservedOutputTokens != 1024 {
		t.Fatalf("context manifest stored source content or omitted its output reservation: %s", manifestPayload)
	}
	sources := map[string]bool{}
	for _, manifest := range manifests {
		for _, selection := range manifest.Selections {
			if selection.Included {
				sources[selection.Source] = true
			}
		}
	}
	for _, required := range []string{"effective_configuration", "controller_policy", "code_intelligence_range", "verification_baselines", "unresolved_findings", "independent_test_designer", "golden_rehearsals", "documentation_manifests"} {
		if !sources[required] {
			t.Fatalf("context manifests omitted production source %q: %#v", required, sources)
		}
	}
	indexStatus, err := store.IntelligenceStatus(ctx, "fixture")
	if err != nil || indexStatus.LatestRevision != job.BaseSHA || indexStatus.Files != 3 {
		t.Fatalf("workflow index status = %#v, %v", indexStatus, err)
	}
	baselines, err := store.ListBaselines(ctx, "fixture", 10)
	if err != nil || len(baselines) != 1 || baselines[0].Revision != job.BaseSHA || len(baselines[0].Observations) < 2 {
		t.Fatalf("workflow baselines = %#v, %v", baselines, err)
	}
	differentials, err := store.ListDifferentials(ctx, "fixture", 10)
	if err != nil || len(differentials) < 3 {
		t.Fatalf("workflow differentials = %#v, %v", differentials, err)
	}
	purposes := map[string]bool{}
	for _, differential := range differentials {
		purposes[differential.Purpose] = true
	}
	if !purposes["targeted"] || !purposes["full"] || !purposes["documentation"] || !purposes["final"] {
		t.Fatalf("workflow differential purposes = %#v", purposes)
	}
	impacts, err := store.ListTestImpacts(ctx, "fixture", 10)
	if err != nil || len(impacts) < 3 {
		t.Fatalf("workflow test impacts = %#v, %v", impacts, err)
	}
	for _, impact := range impacts {
		if !impact.FullSuiteRequired {
			t.Fatalf("workflow impact omitted final full-suite policy: %#v", impact)
		}
	}
	caches, err := store.ListCacheEntries(ctx, "fixture", 10)
	if err != nil || len(caches) != 3 {
		t.Fatalf("workflow parsed-blob caches = %#v, %v", caches, err)
	}
	for _, cache := range caches {
		if cache.Kind != "source-parse" || !cache.Verified || cache.ObjectSHA256 == "" {
			t.Fatalf("unverified workflow cache = %#v", cache)
		}
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
