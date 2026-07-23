package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	artifactfiles "github.com/B1-Mordred/CodeMaintainer/internal/artifacts"
	appconfig "github.com/B1-Mordred/CodeMaintainer/internal/config"
	"github.com/B1-Mordred/CodeMaintainer/internal/findings"
	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/intelligence"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	projectmemory "github.com/B1-Mordred/CodeMaintainer/internal/memory"
	"github.com/B1-Mordred/CodeMaintainer/internal/models"
	"github.com/B1-Mordred/CodeMaintainer/internal/risk"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
	"github.com/B1-Mordred/CodeMaintainer/internal/verification"
)

type GitBackend interface {
	Register(context.Context, gitbridge.Registration) error
	Sync(context.Context, string) (gitbridge.SyncResult, error)
	Snapshot(context.Context, string, string) (gitbridge.RepositorySnapshot, error)
	CreateWorktree(context.Context, gitbridge.WorktreeRequest) (gitbridge.WorktreeResult, error)
	Commit(context.Context, gitbridge.CommitRequest) (gitbridge.CommitResult, error)
	Diff(context.Context, gitbridge.DiffRequest) (gitbridge.DiffResult, error)
	Publish(context.Context, gitbridge.PublishRequest) (gitbridge.Publication, error)
}

type coordinatorStore interface {
	storage.WorkflowStore
	storage.ConfigStore
	storage.FindingStore
	storage.ProjectStore
	taskcontract.Store
	risk.Store
	intelligence.Store
	projectmemory.DurableStore
}

type Coordinator struct {
	store           coordinatorStore
	git             GitBackend
	models          models.Manager
	execution       ExecutionBackend
	artifacts       artifactManager
	worktreesRoot   string
	maxReviewCycles int
	intelligence    *intelligence.Service
}

func NewCoordinator(store coordinatorStore, git GitBackend, modelManager models.Manager, execution ExecutionBackend,
	artifacts artifactManager, worktreesRoot string, maxReviewCycles int, analyzers ...intelligence.Analyzer) (*Coordinator, error) {
	if store == nil || git == nil || modelManager == nil || execution == nil || artifacts == nil ||
		!filepath.IsAbs(worktreesRoot) || maxReviewCycles < 1 || maxReviewCycles > 10 {
		return nil, storage.ErrInvalid
	}
	var analyzer intelligence.Analyzer
	if len(analyzers) > 0 {
		analyzer = analyzers[0]
	}
	intelligenceService, err := intelligence.NewService(store, analyzer)
	if err != nil {
		return nil, err
	}
	return &Coordinator{
		store: store, git: git, models: modelManager, execution: execution, artifacts: artifacts,
		worktreesRoot: filepath.Clean(worktreesRoot), maxReviewCycles: maxReviewCycles, intelligence: intelligenceService,
	}, nil
}

func (c *Coordinator) Execute(ctx context.Context, job jobs.Job) (Outcome, error) {
	switch job.State {
	case jobs.StateQueued:
		return detailOutcome(map[string]any{"queue_version": job.Version}), nil
	case jobs.StateSyncing:
		project, err := c.store.GetProject(ctx, job.ProjectID)
		if err != nil || !project.Enabled || project.Repository != job.Repository {
			return Outcome{}, errors.New("job project registration is missing, disabled, or mismatched")
		}
		if err := c.git.Register(ctx, gitbridge.Registration{
			ProjectID: project.ID, Provider: project.Provider, Repository: project.Repository,
			DefaultBranch: project.DefaultBranch, LocalRemoteName: project.LocalRemoteName,
		}); err != nil {
			return Outcome{}, err
		}
		synced, err := c.git.Sync(ctx, job.ProjectID)
		if err != nil {
			return Outcome{}, err
		}
		if !c.jobConfigBool(ctx, job.ID, "intelligence.indexing_enabled", true) {
			return Outcome{Details: mustJSON(map[string]any{"base_sha": synced.BaseSHA, "index_state": "paused_by_job_configuration"}), Metadata: storage.JobMetadataPatch{BaseSHA: &synced.BaseSHA}}, nil
		}
		snapshot, err := c.git.Snapshot(ctx, job.ProjectID, synced.BaseSHA)
		if err != nil {
			return Outcome{}, err
		}
		indexed := intelligence.IndexRun{State: "unavailable"}
		if len(snapshot.Files) != 0 {
			sources := make([]intelligence.SourceFile, 0, len(snapshot.Files))
			for _, file := range snapshot.Files {
				digest := sha256.Sum256(file.Content)
				sources = append(sources, intelligence.SourceFile{Path: file.Path, BlobSHA256: hex.EncodeToString(digest[:]), Content: file.Content})
			}
			indexed, err = c.intelligence.Index(ctx, intelligence.IndexRequest{ProjectID: job.ProjectID, Repository: job.Repository, Revision: synced.BaseSHA, ParserID: "controller-syntax-v2", CacheRetentionDays: c.jobConfigInt(ctx, job.ID, "intelligence.index_retention_days", 30), CacheQuotaBytes: c.jobConfigInt64(ctx, job.ID, "intelligence.cache_quota_bytes", 512<<20), Files: sources})
			if err != nil {
				return Outcome{}, err
			}
		}
		return Outcome{Details: mustJSON(map[string]any{"base_sha": synced.BaseSHA, "index_run_id": indexed.ID, "index_state": indexed.State, "index_files": indexed.Files, "index_failures": indexed.Failures, "snapshot_excluded": snapshot.Excluded}), Metadata: storage.JobMetadataPatch{BaseSHA: &synced.BaseSHA}}, nil
	case jobs.StateCreatingWorktree:
		created, err := c.git.CreateWorktree(ctx, gitbridge.WorktreeRequest{ProjectID: job.ProjectID, JobID: job.ID, BaseSHA: job.BaseSHA})
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"branch": created.Branch, "base_sha": created.BaseSHA}), nil
	case jobs.StatePreparingDependencies:
		if err := c.execution.PrepareDependencies(ctx, job); err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"network": "dependency-egress", "cache_scope": job.ProjectID}), nil
	case jobs.StateLockingAcceptanceCriteria:
		request, err := taskcontract.DraftFromTask(job.ID, job.Task, job.IssueNumber)
		if err != nil {
			return Outcome{}, err
		}
		request.ActorID = "clarifier"
		request.ActorRole = "system"
		request.Reason = "controller generated bounded draft from submitted task"
		contract, err := c.store.EnsureTaskContract(ctx, request)
		if err != nil {
			return Outcome{}, err
		}
		assessment, err := risk.Assess(contract, nil, "")
		if err != nil {
			return Outcome{}, err
		}
		assessment, err = c.store.SaveRiskAssessment(ctx, assessment)
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{
			"contract_version": contract.Version, "contract_sha256": contract.ContractSHA256,
			"risk_assessment_id": assessment.ID, "risk_level": assessment.Level,
			"next_gate": "awaiting_task_approval",
		}), nil
	case jobs.StateLoadingImplementationModel:
		status, err := c.loadRole(ctx, "implementation")
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"profile_id": status.ProfileID, "model_family": status.ModelFamily}), nil
	case jobs.StateReproducing:
		language := detectLanguage(c.worktree(job.ID))
		baseline, found, err := c.findBaseline(ctx, job, language)
		if err != nil {
			return Outcome{}, err
		}
		if !found {
			baselineResult, verifyErr := c.execution.Verify(ctx, job, language, baselineClasses(language), "baseline")
			if verifyErr != nil {
				return Outcome{}, verifyErr
			}
			baseline, err = c.captureBaseline(ctx, job, language, baselineResult)
			if err != nil {
				return Outcome{}, err
			}
		}
		result, err := c.execution.Verify(ctx, job, language, []verification.Class{verification.ClassTargetedTests}, "reproduction")
		if err != nil {
			return Outcome{}, err
		}
		if result.Passed {
			return Outcome{}, errors.New("the locked targeted test passed before implementation; the defect was not reproduced")
		}
		return detailOutcome(map[string]any{"reproduced": true, "targeted_passed": false, "baseline_id": baseline.ID, "baseline_revision": baseline.Revision}), nil
	case jobs.StateImplementing:
		recovered, err := c.git.Commit(ctx, gitbridge.CommitRequest{
			ProjectID: job.ProjectID, JobID: job.ID, ExpectedHead: job.BaseSHA, OperationID: phaseKey(job),
		})
		if err != nil {
			return Outcome{}, err
		}
		if recovered.ResultSHA != job.BaseSHA {
			impact, impactErr := c.recordTestImpact(ctx, job, recovered.ResultSHA)
			if impactErr != nil {
				return Outcome{}, impactErr
			}
			return Outcome{
				Details:  mustJSON(map[string]any{"result_sha": recovered.ResultSHA, "recovered": true, "test_impact_id": impact.ID}),
				Metadata: storage.JobMetadataPatch{ResultSHA: &recovered.ResultSHA},
			}, nil
		}
		packet, err := c.taskPacket(ctx, job, "implementation", nil)
		if err != nil {
			return Outcome{}, err
		}
		implementation, err := c.execution.Implement(ctx, job, packet)
		if err != nil {
			return Outcome{}, err
		}
		committed, err := c.git.Commit(ctx, gitbridge.CommitRequest{
			ProjectID: job.ProjectID, JobID: job.ID, ExpectedHead: job.BaseSHA, OperationID: phaseKey(job),
		})
		if err != nil {
			return Outcome{}, err
		}
		impact, err := c.recordTestImpact(ctx, job, committed.ResultSHA)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Details:  mustJSON(map[string]any{"result_sha": committed.ResultSHA, "edit_count": len(implementation.Edits), "test_impact_id": impact.ID}),
			Metadata: storage.JobMetadataPatch{ResultSHA: &committed.ResultSHA},
		}, nil
	case jobs.StateVerifyingTargeted:
		return c.requiredVerification(ctx, job, []verification.Class{verification.ClassTargetedTests}, "targeted")
	case jobs.StateVerifyingFull:
		return c.requiredVerification(ctx, job, fullClasses(detectLanguage(c.worktree(job.ID))), "full")
	case jobs.StateLoadingQCModel:
		if err := c.validateRoleSeparation(ctx); err != nil {
			return Outcome{}, err
		}
		status, err := c.loadRole(ctx, "qc")
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"profile_id": status.ProfileID, "model_family": status.ModelFamily}), nil
	case jobs.StateQCReview:
		return c.review(ctx, job)
	case jobs.StateAwaitingRepair:
		status, err := c.loadRole(ctx, "implementation")
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"profile_id": status.ProfileID, "repair_cycle": job.ReviewCycle + 1}), nil
	case jobs.StateRepairing:
		return c.repair(ctx, job)
	case jobs.StateFinalVerification:
		outcome, err := c.requiredVerification(ctx, job, fullClasses(detectLanguage(c.worktree(job.ID))), "final")
		if err != nil {
			return Outcome{}, err
		}
		if err := c.transitionFindings(ctx, job.ID, findings.StatusFixed, findings.StatusVerified,
			"allow-listed final verification passed for the repaired exact commit"); err != nil {
			return Outcome{}, err
		}
		return outcome, nil
	case jobs.StatePublishingBranch:
		publication, err := c.git.Publish(ctx, gitbridge.PublishRequest{
			ProjectID: job.ProjectID, JobID: job.ID, BaseSHA: job.BaseSHA, ResultSHA: job.ResultSHA,
		})
		if err != nil {
			return Outcome{}, err
		}
		payload, _ := json.Marshal(publication)
		if _, err := c.artifacts.Put(ctx, artifactfiles.PutRequest{
			JobID: job.ID, ProjectID: job.ProjectID, Kind: "publication", MediaType: "application/json",
			Producer: "workflow-controller", IdempotencyKey: phaseKey(job) + "_publication",
			Metadata: mustJSON(map[string]any{
				"draft": true, "provider": publication.Provider, "number": publication.Number,
				"external_id": publication.ExternalID, "branch": publication.Branch, "result_sha": publication.ResultSHA,
			}), Reader: bytes.NewReader(payload),
		}); err != nil {
			return Outcome{}, err
		}
		return detailOutcome(publication), nil
	case jobs.StateDraftPRCreated:
		return detailOutcome(map[string]any{"completed": true, "publication": "draft"}), nil
	default:
		return Outcome{}, fmt.Errorf("coordinator has no implementation for state %s", job.State)
	}
}

func (c *Coordinator) requiredVerification(ctx context.Context, job jobs.Job, classes []verification.Class, purpose string) (Outcome, error) {
	language := detectLanguage(c.worktree(job.ID))
	executionPurpose := purpose
	cleanFinalCache := purpose == "final" && c.jobConfigBool(ctx, job.ID, "verification.clean_final_cache_required", true)
	if cleanFinalCache {
		executionPurpose = "final_clean"
	}
	result, err := c.execution.Verify(ctx, job, language, classes, executionPurpose)
	if err != nil {
		return Outcome{}, err
	}
	if result.Scan == nil || result.Scan.HeadSHA != job.ResultSHA || len(result.Scan.PatchSHA256) != 64 {
		return Outcome{}, errors.New("required verification did not pass for the exact result commit")
	}
	baseline, found, err := c.findBaseline(ctx, job, language)
	if err != nil {
		return Outcome{}, err
	}
	if !found {
		return Outcome{}, errors.New("required verification has no exact configuration/toolchain baseline")
	}
	comparable := comparableBaseline(baseline, verificationObservations(result))
	differential, err := c.intelligence.CompareAndSave(ctx, comparable, result.Scan.PatchSHA256, purpose, verificationObservations(result))
	if err != nil {
		return Outcome{}, err
	}
	if purpose == "final" {
		impact, found, impactErr := c.intelligence.FindTestImpact(ctx, job.ProjectID, job.ResultSHA)
		if impactErr != nil {
			return Outcome{}, impactErr
		}
		if !found {
			impact, impactErr = c.recordTestImpact(ctx, job, job.ResultSHA)
		}
		if impactErr != nil || !impact.FullSuiteRequired {
			return Outcome{}, errors.New("final verification is missing mandatory full-suite impact policy evidence")
		}
	}
	if !result.Passed {
		return Outcome{}, fmt.Errorf("required verification did not pass for the exact result commit; differential %s retained", differential.ID)
	}
	return detailOutcome(map[string]any{
		"passed": true, "head_sha": result.Scan.HeadSHA, "patch_sha256": result.Scan.PatchSHA256, "classes": classes,
		"baseline_id": baseline.ID, "differential_id": differential.ID, "purpose": purpose, "clean_cache_required": cleanFinalCache,
	}), nil
}

func (c *Coordinator) baselineIdentity(ctx context.Context, job jobs.Job, language verification.Language) (string, string, error) {
	snapshot, err := c.store.GetJobConfigSnapshot(ctx, job.ID)
	configSHA256 := ""
	if err == nil {
		configSHA256 = snapshot.SHA256
	} else if errors.Is(err, storage.ErrNotFound) {
		// Retained Increment 1 databases and focused stores may not have had the
		// registry attached when the job was accepted. Keep that compatibility
		// state explicit and content-bound instead of pretending it is current.
		digest := sha256.Sum256([]byte("increment-1-no-registry-snapshot-v1"))
		configSHA256 = hex.EncodeToString(digest[:])
	} else {
		return "", "", err
	}
	toolchainID := "verification-worker-v1-" + string(language)
	return configSHA256, toolchainID, nil
}

func (c *Coordinator) jobConfigSnapshot(ctx context.Context, jobID string) (appconfig.Snapshot, bool) {
	stored, err := c.store.GetJobConfigSnapshot(ctx, jobID)
	if err != nil {
		return appconfig.Snapshot{}, false
	}
	var snapshot appconfig.Snapshot
	if json.Unmarshal(stored.Document, &snapshot) != nil || snapshot.SHA256 != stored.SHA256 {
		return appconfig.Snapshot{}, false
	}
	return snapshot, true
}

func (c *Coordinator) jobConfigInt(ctx context.Context, jobID, key string, fallback int) int {
	snapshot, ok := c.jobConfigSnapshot(ctx, jobID)
	if !ok {
		return fallback
	}
	value, exists := snapshot.Values[key]
	if !exists || json.Unmarshal(value.Value, &fallback) != nil {
		return fallback
	}
	return fallback
}

func (c *Coordinator) jobConfigInt64(ctx context.Context, jobID, key string, fallback int64) int64 {
	snapshot, ok := c.jobConfigSnapshot(ctx, jobID)
	if !ok {
		return fallback
	}
	value, exists := snapshot.Values[key]
	if !exists || json.Unmarshal(value.Value, &fallback) != nil {
		return fallback
	}
	return fallback
}

func (c *Coordinator) jobConfigBool(ctx context.Context, jobID, key string, fallback bool) bool {
	snapshot, ok := c.jobConfigSnapshot(ctx, jobID)
	if !ok {
		return fallback
	}
	value, exists := snapshot.Values[key]
	if !exists || json.Unmarshal(value.Value, &fallback) != nil {
		return fallback
	}
	return fallback
}

func packSetIdentity() string {
	digest := sha256.Sum256([]byte("built-in-pack-set-v1"))
	return hex.EncodeToString(digest[:])
}

func (c *Coordinator) findBaseline(ctx context.Context, job jobs.Job, language verification.Language) (intelligence.Baseline, bool, error) {
	configSHA256, toolchainID, err := c.baselineIdentity(ctx, job, language)
	if err != nil {
		return intelligence.Baseline{}, false, err
	}
	return c.intelligence.FindBaseline(ctx, job.ProjectID, job.BaseSHA, configSHA256, toolchainID, packSetIdentity())
}

func (c *Coordinator) captureBaseline(ctx context.Context, job jobs.Job, language verification.Language, result VerificationResult) (intelligence.Baseline, error) {
	configSHA256, toolchainID, err := c.baselineIdentity(ctx, job, language)
	if err != nil {
		return intelligence.Baseline{}, err
	}
	return c.intelligence.CaptureBaseline(ctx, intelligence.Baseline{
		ProjectID: job.ProjectID, Revision: job.BaseSHA, ConfigSHA256: configSHA256,
		ToolchainID: toolchainID, PackSetSHA256: packSetIdentity(), ActorID: "workflow-controller",
		Reason:       "policy-defined pre-modification verification in the clean verification worker",
		Observations: verificationObservations(result),
	})
}

func verificationObservations(result VerificationResult) []intelligence.Observation {
	observations := make([]intelligence.Observation, 0, len(result.Results))
	for _, item := range result.Results {
		status := "passed"
		if item.ExitCode != 0 || item.TimedOut {
			status = "failed"
		}
		value, _ := json.Marshal(map[string]any{"exit_code": item.ExitCode, "timed_out": item.TimedOut, "truncated": item.Truncated})
		observations = append(observations, intelligence.Observation{Key: string(item.Class), Kind: "verification", Status: status, Value: value})
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Key < observations[j].Key })
	return observations
}

func comparableBaseline(baseline intelligence.Baseline, candidate []intelligence.Observation) intelligence.Baseline {
	keys := make(map[string]bool, len(candidate))
	for _, item := range candidate {
		keys[item.Kind+"\x00"+item.Key] = true
	}
	filtered := make([]intelligence.Observation, 0, len(candidate))
	for _, item := range baseline.Observations {
		if keys[item.Kind+"\x00"+item.Key] {
			filtered = append(filtered, item)
		}
	}
	baseline.Observations = filtered
	return baseline
}

func (c *Coordinator) recordTestImpact(ctx context.Context, job jobs.Job, revision string) (intelligence.TestImpact, error) {
	if existing, found, err := c.intelligence.FindTestImpact(ctx, job.ProjectID, revision); err != nil || found {
		return existing, err
	}
	diff, err := c.git.Diff(ctx, gitbridge.DiffRequest{ProjectID: job.ProjectID, JobID: job.ID, BaseSHA: job.BaseSHA, ResultSHA: revision})
	if err != nil {
		return intelligence.TestImpact{}, err
	}
	if len(diff.ChangedPaths) == 0 {
		return intelligence.TestImpact{}, errors.New("candidate commit has no changed paths for test-impact analysis")
	}
	changedSet := make(map[string]bool, len(diff.ChangedPaths))
	for _, changed := range diff.ChangedPaths {
		changedSet[changed] = true
		query, queryErr := c.intelligence.Query(ctx, intelligence.Query{ProjectID: job.ProjectID, Revision: job.BaseSHA, Term: changed, Limit: 200})
		if queryErr == nil {
			for _, symbol := range query.Symbols {
				changedSet[symbol.Name] = true
			}
		}
	}
	changed := make([]string, 0, len(changedSet))
	for value := range changedSet {
		changed = append(changed, value)
	}
	sort.Strings(changed)
	files, err := BuildRelevantFiles(c.worktree(job.ID), 64, 2<<20)
	if err != nil {
		return intelligence.TestImpact{}, err
	}
	tests := make(map[string][]string)
	for _, file := range files {
		if targets := likelyTestTargets(file.Path); len(targets) != 0 {
			tests[file.Path] = targets
		}
	}
	if len(tests) == 0 {
		tests["policy:full-suite"] = append([]string(nil), diff.ChangedPaths...)
	}
	impact, err := intelligence.NewTestImpact(job.ProjectID, revision, changed, tests, true)
	if err != nil {
		return intelligence.TestImpact{}, err
	}
	for index := range impact.Selections {
		impact.Selections[index].EstimatedSeconds = 60
	}
	impact.PolicyExplanation = "Changed paths and indexed symbols select the bounded inner-loop suite; every publishable candidate still requires a fresh complete final suite."
	return c.intelligence.RecordTestImpact(ctx, impact)
}

func likelyTestTargets(filePath string) []string {
	slash := filepath.ToSlash(filePath)
	switch {
	case strings.HasSuffix(slash, "_test.go"):
		return []string{slash, strings.TrimSuffix(slash, "_test.go") + ".go"}
	case strings.HasSuffix(slash, ".test.ts"):
		return []string{slash, strings.TrimSuffix(slash, ".test.ts") + ".ts"}
	case strings.HasSuffix(slash, ".test.tsx"):
		return []string{slash, strings.TrimSuffix(slash, ".test.tsx") + ".tsx"}
	case strings.HasSuffix(slash, "Test.php"):
		return []string{slash, strings.TrimSuffix(slash, "Test.php") + ".php"}
	case strings.HasPrefix(filepath.Base(slash), "test_") && strings.HasSuffix(slash, ".py"):
		return []string{slash, filepath.ToSlash(filepath.Join(filepath.Dir(slash), strings.TrimPrefix(filepath.Base(slash), "test_")))}
	default:
		return nil
	}
}

func (c *Coordinator) review(ctx context.Context, job jobs.Job) (Outcome, error) {
	diff, err := c.git.Diff(ctx, gitbridge.DiffRequest{
		ProjectID: job.ProjectID, JobID: job.ID, BaseSHA: job.BaseSHA, ResultSHA: job.ResultSHA,
	})
	if err != nil {
		return Outcome{}, err
	}
	packet, err := c.taskPacket(ctx, job, "qc", nil)
	if err != nil {
		return Outcome{}, err
	}
	packet.Diff = diff.Patch
	packet.ResultSHA = job.ResultSHA
	packet.Verification = []agents.VerificationEvidence{
		{Class: "targeted_tests", Passed: true, Summary: "allow-listed targeted verification passed for the exact result commit"},
		{Class: "full_tests", Passed: true, Summary: "allow-listed full verification passed for the exact result commit"},
		{Class: "diff_policy", Passed: true, Summary: "protected-path and secret policy scan passed for the exact result commit"},
	}
	report, err := c.execution.Review(ctx, job, packet)
	if err != nil {
		return Outcome{}, err
	}
	records, err := c.store.ObserveFindings(ctx, job.ID, job.ReviewCycle, report.Findings)
	if err != nil {
		return Outcome{}, err
	}
	blocking := 0
	for _, record := range records {
		if record.Severity == "blocker" || record.Severity == "must_fix" {
			blocking++
		}
	}
	if blocking != 0 {
		exhausted := job.ReviewCycle+1 >= c.maxReviewCycles
		return Outcome{
			NeedsRepair: !exhausted,
			Details:     mustJSON(map[string]any{"verdict": report.Verdict, "blocking_findings": blocking, "cycle_exhausted": exhausted}),
		}, nil
	}
	if err := c.transitionFindings(ctx, job.ID, findings.StatusVerified, findings.StatusClosed,
		"fresh read-only QC passed after exact-commit verification"); err != nil {
		return Outcome{}, err
	}
	if err := c.writeFinalReport(ctx, job, report); err != nil {
		return Outcome{}, err
	}
	return detailOutcome(map[string]any{"verdict": report.Verdict, "blocking_findings": 0, "review_cycle": job.ReviewCycle}), nil
}

func (c *Coordinator) repair(ctx context.Context, job jobs.Job) (Outcome, error) {
	records, err := c.store.ListFindings(ctx, job.ID)
	if err != nil {
		return Outcome{}, err
	}
	recovered, err := c.git.Commit(ctx, gitbridge.CommitRequest{
		ProjectID: job.ProjectID, JobID: job.ID, ExpectedHead: job.ResultSHA, OperationID: phaseKey(job),
	})
	if err != nil {
		return Outcome{}, err
	}
	if recovered.ResultSHA != job.ResultSHA {
		if err := c.transitionFindings(ctx, job.ID, findings.StatusOpen, findings.StatusFixed,
			"recovered the already committed bounded repair after controller restart"); err != nil {
			return Outcome{}, err
		}
		nextCycle := job.ReviewCycle + 1
		impact, impactErr := c.recordTestImpact(ctx, job, recovered.ResultSHA)
		if impactErr != nil {
			return Outcome{}, impactErr
		}
		return Outcome{
			Details:  mustJSON(map[string]any{"result_sha": recovered.ResultSHA, "review_cycle": nextCycle, "recovered": true, "test_impact_id": impact.ID}),
			Metadata: storage.JobMetadataPatch{ResultSHA: &recovered.ResultSHA, ReviewCycle: &nextCycle},
		}, nil
	}
	blocking := make([]agents.Finding, 0)
	for _, record := range records {
		if record.Status != findings.StatusOpen || (record.Severity != "blocker" && record.Severity != "must_fix") {
			continue
		}
		var location agents.Location
		if err := json.Unmarshal(record.Location, &location); err != nil {
			return Outcome{}, err
		}
		blocking = append(blocking, agents.Finding{
			ID: record.ID, Severity: record.Severity, Category: record.Category, Claim: record.Claim,
			Location: location, Evidence: fmt.Sprintf("Persisted QC observation from review cycle %d.", record.LastSeenCycle),
			RequiredResolution: record.RequiredResolution, VerificationMethod: record.VerificationMethod,
		})
	}
	if len(blocking) == 0 {
		return Outcome{}, errors.New("repair phase has no open blocking findings")
	}
	packet, err := c.taskPacket(ctx, job, "repair", blocking)
	if err != nil {
		return Outcome{}, err
	}
	if _, err := c.execution.Implement(ctx, job, packet); err != nil {
		return Outcome{}, err
	}
	committed, err := c.git.Commit(ctx, gitbridge.CommitRequest{
		ProjectID: job.ProjectID, JobID: job.ID, ExpectedHead: job.ResultSHA, OperationID: phaseKey(job),
	})
	if err != nil {
		return Outcome{}, err
	}
	if committed.ResultSHA == job.ResultSHA {
		return Outcome{}, errors.New("repair produced no exact-commit change")
	}
	if err := c.transitionFindings(ctx, job.ID, findings.StatusOpen, findings.StatusFixed,
		"implementation agent applied the required bounded repair"); err != nil {
		return Outcome{}, err
	}
	nextCycle := job.ReviewCycle + 1
	impact, err := c.recordTestImpact(ctx, job, committed.ResultSHA)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Details:  mustJSON(map[string]any{"result_sha": committed.ResultSHA, "review_cycle": nextCycle, "repaired_findings": len(blocking), "test_impact_id": impact.ID}),
		Metadata: storage.JobMetadataPatch{ResultSHA: &committed.ResultSHA, ReviewCycle: &nextCycle},
	}, nil
}

func (c *Coordinator) taskPacket(ctx context.Context, job jobs.Job, mode string, blockers []agents.Finding) (agents.TaskPacket, error) {
	var criteria []agents.Criterion
	if err := json.Unmarshal(job.AcceptanceCriteria, &criteria); err != nil || len(criteria) == 0 {
		return agents.TaskPacket{}, errors.New("job acceptance criteria are not locked")
	}
	files, err := BuildRelevantFiles(c.worktree(job.ID), 64, 2<<20)
	if err != nil {
		return agents.TaskPacket{}, err
	}
	criteriaPayload, _ := json.Marshal(criteria)
	candidates := []intelligence.ContextCandidate{
		{ID: "task", Source: "task_contract", Version: job.AcceptanceCriteriaHash, Reason: "operator request", Trust: "trusted", Content: []byte(job.Task), Priority: 1000},
		{ID: "criteria", Source: "acceptance_criteria", Version: job.AcceptanceCriteriaHash, Reason: "locked completion contract", Trust: "trusted", Content: criteriaPayload, Priority: 990},
	}
	if snapshot, ok := c.jobConfigSnapshot(ctx, job.ID); ok {
		payload, _ := json.Marshal(snapshot)
		candidates = append(candidates, intelligence.ContextCandidate{ID: "effective-configuration", Source: "effective_configuration", Version: snapshot.SHA256, Reason: "immutable acceptance-time configuration and provenance", Trust: "trusted", Content: payload, Priority: 980})
	}
	if len(blockers) != 0 {
		payload, _ := json.Marshal(blockers)
		candidates = append(candidates, intelligence.ContextCandidate{ID: "unresolved-findings", Source: "unresolved_findings", Version: fmt.Sprintf("review-%d", job.ReviewCycle), Reason: "validated unresolved QC findings for the current repair cycle", Trust: "untrusted", Content: payload, Priority: 970})
	}
	policyPayload, _ := json.Marshal(verification.DefaultPolicy())
	candidates = append(candidates, intelligence.ContextCandidate{ID: "protected-path-policy", Source: "controller_policy", Version: "verification-policy-v1", Reason: "trusted protected-path and bounded-diff policy applied by the verifier", Trust: "trusted", Content: policyPayload, Priority: 960})
	if parts := strings.SplitN(job.Repository, "/", 2); len(parts) == 2 {
		scope := projectmemory.ProjectScope{Owner: parts[0], Repository: parts[1]}
		memories, memoryErr := c.store.ListMemory(ctx, scope, projectmemory.StatusCanonical, 20)
		if memoryErr != nil {
			return agents.TaskPacket{}, memoryErr
		}
		now := time.Now().UTC()
		for _, record := range memories {
			if !record.Verified || !record.SecretScanPass || (record.ExpiresAt != nil && !record.ExpiresAt.After(now)) {
				continue
			}
			candidates = append(candidates, intelligence.ContextCandidate{ID: "memory:" + record.ID, Source: "verified_memory", Version: fmt.Sprintf("v%d:%s", record.Version, record.ContentHash), Reason: "canonical project-namespaced memory with verification provenance", Trust: "verified_memory", Content: []byte(record.Content), Priority: 760, Stale: record.Status == projectmemory.StatusStale})
		}
	}
	baselines, err := c.intelligence.Baselines(ctx, job.ProjectID, 3)
	if err != nil {
		return agents.TaskPacket{}, err
	}
	differentials, err := c.intelligence.Differentials(ctx, job.ProjectID, 3)
	if err != nil {
		return agents.TaskPacket{}, err
	}
	impacts, err := c.intelligence.TestImpacts(ctx, job.ProjectID, 3)
	if err != nil {
		return agents.TaskPacket{}, err
	}
	for _, evidence := range []struct {
		id, source, reason string
		value              any
	}{
		{id: "baseline-evidence", source: "verification_baselines", reason: "recent exact-identity pre-change evidence", value: baselines},
		{id: "differential-evidence", source: "verification_differentials", reason: "recent candidate comparison and unresolved classifications", value: differentials},
		{id: "test-impact-evidence", source: "test_impact", reason: "recent changed-symbol test selection and full-suite policy", value: impacts},
	} {
		payload, _ := json.Marshal(evidence.value)
		if string(payload) != "[]" && string(payload) != "null" {
			candidates = append(candidates, intelligence.ContextCandidate{ID: evidence.id, Source: evidence.source, Version: firstNonempty(job.ResultSHA, job.BaseSHA), Reason: evidence.reason, Trust: "trusted", Content: payload, Priority: 740})
		}
	}
	rangeContexts := map[string][]string{}
	for _, file := range files {
		source, reason, priority := "repository_file", "bounded relevant source selected by controller policy", 400-contextPriority(file.Path)
		if strings.HasSuffix(file.Path, "AGENTS.md") || file.Path == "project.md" || strings.HasPrefix(file.Path, "docs/") {
			source, reason, priority = "repository_instructions", "repository instructions or authoritative documentation", 900
		}
		candidates = append(candidates, intelligence.ContextCandidate{ID: "file:" + file.Path, Source: source, Version: firstNonempty(job.ResultSHA, job.BaseSHA), Reason: reason, Trust: "untrusted", Content: []byte(file.Content), Priority: priority})
		query, queryErr := c.intelligence.Query(ctx, intelligence.Query{ProjectID: job.ProjectID, Revision: job.BaseSHA, Term: file.Path, Limit: 8})
		if errors.Is(queryErr, storage.ErrNotFound) {
			continue
		}
		if queryErr != nil {
			return agents.TaskPacket{}, queryErr
		}
		for _, symbol := range query.Symbols {
			if symbol.Path != file.Path {
				continue
			}
			fragment := sourceLineRange(file.Content, symbol.StartLine, symbol.EndLine)
			if fragment == "" {
				continue
			}
			id := "symbol:" + symbol.ID
			rangeContexts[file.Path] = append(rangeContexts[file.Path], id)
			candidates = append(candidates, intelligence.ContextCandidate{ID: id, Source: "code_intelligence_range", Version: firstNonempty(job.ResultSHA, job.BaseSHA), Reason: fmt.Sprintf("exact indexed %s %s at %s:%d-%d", symbol.Kind, symbol.Name, symbol.Path, symbol.StartLine, symbol.EndLine), Trust: "untrusted", Content: []byte(fragment), Priority: 700})
		}
	}
	inputBudget := c.jobConfigInt(ctx, job.ID, "intelligence.context_input_tokens", 32_768)
	outputReserve := c.jobConfigInt(ctx, job.ID, "intelligence.context_output_reserve_tokens", 8_192)
	if outputReserve >= inputBudget {
		return agents.TaskPacket{}, errors.New("job configuration context output reserve must be below its input budget")
	}
	compiled, err := c.intelligence.CompileContext(ctx, job.ProjectID, job.ID, mode, inputBudget, outputReserve, candidates)
	if err != nil {
		return agents.TaskPacket{}, err
	}
	included := map[string]bool{}
	for _, selection := range compiled.Manifest.Selections {
		if selection.Included {
			included[selection.ID] = true
		}
	}
	selectedFiles := make([]agents.FileContext, 0, len(files))
	for _, file := range files {
		if included["file:"+file.Path] {
			selectedFiles = append(selectedFiles, file)
			continue
		}
		fragments := []string{}
		for _, id := range rangeContexts[file.Path] {
			if included[id] {
				for _, candidate := range candidates {
					if candidate.ID == id {
						fragments = append(fragments, string(candidate.Content))
						break
					}
				}
			}
		}
		if len(fragments) != 0 {
			selectedFiles = append(selectedFiles, agents.FileContext{Path: file.Path, Content: strings.Join(fragments, "\n\n")})
		}
	}
	if len(selectedFiles) == 0 {
		return agents.TaskPacket{}, errors.New("context compiler selected no bounded repository files")
	}
	packet := agents.TaskPacket{
		SchemaVersion: 1, Mode: mode, JobID: job.ID, OriginalTask: job.Task,
		AcceptanceCriteria: criteria, BaseSHA: job.BaseSHA, ResultSHA: job.ResultSHA,
		ReviewCycle: job.ReviewCycle, RelevantFiles: selectedFiles, BlockingFindings: blockers,
	}
	payload, _ := json.Marshal(packet)
	if _, err := agents.DecodeTaskPacket(payload, mode); err != nil {
		return agents.TaskPacket{}, err
	}
	return packet, nil
}

func sourceLineRange(content string, startLine, endLine int) string {
	if startLine < 1 || endLine < startLine || endLine-startLine > 2000 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func (c *Coordinator) loadRole(ctx context.Context, role string) (models.Status, error) {
	if err := c.models.Unload(ctx); err != nil {
		return models.Status{}, err
	}
	return c.models.Load(ctx, role)
}

func (c *Coordinator) validateRoleSeparation(ctx context.Context) error {
	profiles, err := c.models.Profiles(ctx)
	if err != nil {
		return err
	}
	families := make(map[string]string)
	for _, profile := range profiles {
		if profile.Role == "implementation" || profile.Role == "qc" {
			families[profile.Role] = profile.ModelFamily
		}
	}
	return agents.ValidateDifferentFamilies(families["implementation"], families["qc"])
}

func (c *Coordinator) transitionFindings(ctx context.Context, jobID string, from, to findings.Status, rationale string) error {
	records, err := c.store.ListFindings(ctx, jobID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Status != from {
			continue
		}
		if _, err := c.store.TransitionFinding(ctx, jobID, record.ID, findings.TransitionRequest{
			To: to, ActorID: "workflow-controller", ActorRole: "system", Rationale: rationale,
			ExpectedVersion: record.Version,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) writeFinalReport(ctx context.Context, job jobs.Job, report agents.QCReport) error {
	findingsList, err := c.store.ListFindings(ctx, job.ID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "job_id": job.ID, "base_sha": job.BaseSHA, "result_sha": job.ResultSHA,
		"acceptance_criteria_hash": job.AcceptanceCriteriaHash, "review_cycle": job.ReviewCycle,
		"qc_verdict": report.Verdict, "findings": findingsList, "publication_status": "awaiting_operator",
	})
	if err != nil {
		return err
	}
	artifact, err := c.artifacts.Put(ctx, artifactfiles.PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: "final_report", MediaType: "application/json",
		Producer: "workflow-controller", IdempotencyKey: phaseKey(job) + "_final_report",
		Metadata: mustJSON(map[string]any{"result_sha": job.ResultSHA}), Reader: bytes.NewReader(payload),
	})
	if err != nil {
		return err
	}
	return extractVerifiedCase(ctx, c.store, job, artifact.ID)
}

func (c *Coordinator) worktree(jobID string) string { return filepath.Join(c.worktreesRoot, jobID) }

func detectLanguage(worktree string) verification.Language {
	markers := []struct {
		name     string
		language verification.Language
	}{{"go.mod", verification.LanguageGo}, {"pyproject.toml", verification.LanguagePython},
		{"package.json", verification.LanguageNode}, {"Cargo.toml", verification.LanguageRust},
		{"CMakeLists.txt", verification.LanguageCPP}}
	found := make([]verification.Language, 0)
	for _, marker := range markers {
		if info, err := os.Lstat(filepath.Join(worktree, marker.name)); err == nil && info.Mode().IsRegular() {
			found = append(found, marker.language)
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	if len(found) > 1 {
		return verification.LanguageFull
	}
	return verification.LanguageBase
}

func fullClasses(language verification.Language) []verification.Class {
	switch language {
	case verification.LanguageGo:
		return []verification.Class{verification.ClassFormat, verification.ClassCompile, verification.ClassLint, verification.ClassFullTests}
	default:
		return []verification.Class{verification.ClassCompile, verification.ClassFullTests}
	}
}

func baselineClasses(language verification.Language) []verification.Class {
	return append([]verification.Class{verification.ClassTargetedTests}, fullClasses(language)...)
}

func detailOutcome(value any) Outcome { return Outcome{Details: mustJSON(value)} }

func mustJSON(value any) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}
