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

	"github.com/local-code-maintainer/appliance/internal/agents"
	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/models"
	"github.com/local-code-maintainer/appliance/internal/storage"
	"github.com/local-code-maintainer/appliance/internal/verification"
)

type GitBackend interface {
	Register(context.Context, gitbridge.Registration) error
	Sync(context.Context, string) (gitbridge.SyncResult, error)
	CreateWorktree(context.Context, gitbridge.WorktreeRequest) (gitbridge.WorktreeResult, error)
	Commit(context.Context, gitbridge.CommitRequest) (gitbridge.CommitResult, error)
	Diff(context.Context, gitbridge.DiffRequest) (gitbridge.DiffResult, error)
	Publish(context.Context, gitbridge.PublishRequest) (gitbridge.Publication, error)
}

type coordinatorStore interface {
	storage.WorkflowStore
	storage.FindingStore
	storage.ProjectStore
	intelligence.Store
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
	artifacts artifactManager, worktreesRoot string, maxReviewCycles int) (*Coordinator, error) {
	if store == nil || git == nil || modelManager == nil || execution == nil || artifacts == nil ||
		!filepath.IsAbs(worktreesRoot) || maxReviewCycles < 1 || maxReviewCycles > 10 {
		return nil, storage.ErrInvalid
	}
	intelligenceService, err := intelligence.NewService(store, nil)
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
		return Outcome{Details: mustJSON(map[string]any{"base_sha": synced.BaseSHA}), Metadata: storage.JobMetadataPatch{BaseSHA: &synced.BaseSHA}}, nil
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
		criteria := []agents.Criterion{
			{ID: "AC-REPRODUCE", Statement: "The reported behavior is reproduced by an allow-listed targeted test before implementation.", VerificationMethod: "targeted_tests"},
			{ID: "AC-VERIFY", Statement: "The exact committed repair passes allow-listed targeted and full verification.", VerificationMethod: "full_tests"},
			{ID: "AC-POLICY", Statement: "The exact committed diff passes protected-path, secret, binary, symlink, submodule, and size policy scans.", VerificationMethod: "diff_policy"},
		}
		payload, err := json.Marshal(criteria)
		if err != nil {
			return Outcome{}, err
		}
		digest := sha256.Sum256(payload)
		hash := hex.EncodeToString(digest[:])
		raw := json.RawMessage(payload)
		return Outcome{
			Details:  mustJSON(map[string]any{"criteria_hash": hash, "criteria_count": len(criteria)}),
			Metadata: storage.JobMetadataPatch{AcceptanceCriteria: &raw, AcceptanceCriteriaHash: &hash},
		}, nil
	case jobs.StateLoadingImplementationModel:
		status, err := c.loadRole(ctx, "implementation")
		if err != nil {
			return Outcome{}, err
		}
		return detailOutcome(map[string]any{"profile_id": status.ProfileID, "model_family": status.ModelFamily}), nil
	case jobs.StateReproducing:
		result, err := c.execution.Verify(ctx, job, detectLanguage(c.worktree(job.ID)), []verification.Class{verification.ClassTargetedTests}, "reproduction")
		if err != nil {
			return Outcome{}, err
		}
		if result.Passed {
			return Outcome{}, errors.New("the locked targeted test passed before implementation; the defect was not reproduced")
		}
		return detailOutcome(map[string]any{"reproduced": true, "targeted_passed": false}), nil
	case jobs.StateImplementing:
		recovered, err := c.git.Commit(ctx, gitbridge.CommitRequest{
			ProjectID: job.ProjectID, JobID: job.ID, ExpectedHead: job.BaseSHA, OperationID: phaseKey(job),
		})
		if err != nil {
			return Outcome{}, err
		}
		if recovered.ResultSHA != job.BaseSHA {
			return Outcome{
				Details:  mustJSON(map[string]any{"result_sha": recovered.ResultSHA, "recovered": true}),
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
		return Outcome{
			Details:  mustJSON(map[string]any{"result_sha": committed.ResultSHA, "edit_count": len(implementation.Edits)}),
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
	result, err := c.execution.Verify(ctx, job, detectLanguage(c.worktree(job.ID)), classes, purpose)
	if err != nil {
		return Outcome{}, err
	}
	if !result.Passed || result.Scan == nil || result.Scan.HeadSHA != job.ResultSHA {
		return Outcome{}, errors.New("required verification did not pass for the exact result commit")
	}
	return detailOutcome(map[string]any{
		"passed": true, "head_sha": result.Scan.HeadSHA, "patch_sha256": result.Scan.PatchSHA256, "classes": classes,
	}), nil
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
		return Outcome{
			Details:  mustJSON(map[string]any{"result_sha": recovered.ResultSHA, "review_cycle": nextCycle, "recovered": true}),
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
	return Outcome{
		Details:  mustJSON(map[string]any{"result_sha": committed.ResultSHA, "review_cycle": nextCycle, "repaired_findings": len(blocking)}),
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
	for _, file := range files {
		candidates = append(candidates, intelligence.ContextCandidate{ID: "file:" + file.Path, Source: "repository_file", Version: firstNonempty(job.ResultSHA, job.BaseSHA), Reason: "bounded relevant source selected by controller policy", Trust: "untrusted", Content: []byte(file.Content), Priority: 500 - contextPriority(file.Path)})
	}
	compiled, err := c.intelligence.CompileContext(ctx, job.ProjectID, job.ID, mode, 32_768, 8_192, candidates)
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

func detailOutcome(value any) Outcome { return Outcome{Details: mustJSON(value)} }

func mustJSON(value any) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}
