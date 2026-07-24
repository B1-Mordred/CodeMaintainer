package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = 1

	SourceCuratedFixtures = "curated_fixtures"
	SourceHistoricalRange = "historical_range"

	RunQueued    = "queued"
	RunCompleted = "completed"
	RunRejected  = "rejected"

	RecommendationReviewOnly = "review_only"
	RecommendationRejected   = "rejected"
)

type Dataset struct {
	ID                 string          `json:"id"`
	SchemaVersion      int             `json:"schema_version"`
	ProjectID          string          `json:"project_id"`
	Name               string          `json:"name"`
	SourceKind         string          `json:"source_kind"`
	Repository         string          `json:"repository"`
	BaseRevision       string          `json:"base_revision"`
	TargetRevision     string          `json:"target_revision"`
	KnownPatchSHA256   string          `json:"known_patch_sha256"`
	HiddenPatchSHA256  string          `json:"hidden_patch_sha256"`
	Exclusions         []string        `json:"exclusions"`
	ScoringProfile     string          `json:"scoring_profile"`
	RetentionDays      int             `json:"retention_days"`
	ReproducibilityKey string          `json:"reproducibility_key"`
	Metadata           json.RawMessage `json:"metadata"`
	ActorID            string          `json:"actor_id"`
	CreatedAt          time.Time       `json:"created_at"`
}

type ProfileResult struct {
	ProfileID               string  `json:"profile_id"`
	TaskCompletion          float64 `json:"task_completion"`
	TestSuccess             float64 `json:"test_success"`
	RegressionRate          float64 `json:"regression_rate"`
	DiffChurn               float64 `json:"diff_churn"`
	FindingPrecision        float64 `json:"finding_precision"`
	FindingRecall           float64 `json:"finding_recall"`
	ContextTokens           int     `json:"context_tokens"`
	ContextBytes            int     `json:"context_bytes"`
	IrrelevantContextRatio  float64 `json:"irrelevant_context_ratio"`
	WallTimeMillis          int64   `json:"wall_time_millis"`
	CPUTimeMillis           int64   `json:"cpu_time_millis"`
	MemoryBytes             int64   `json:"memory_bytes"`
	CacheEffect             string  `json:"cache_effect"`
	DocumentationCompliance float64 `json:"documentation_compliance"`
	OperatorInterventions   int     `json:"operator_interventions"`
	UnresolvedUncertainty   int     `json:"unresolved_uncertainty"`
	KnownPatchHidden        bool    `json:"known_patch_hidden"`
	PromotionAllowed        bool    `json:"promotion_allowed"`
	PromotionBlockedReason  string  `json:"promotion_blocked_reason"`
}

type Run struct {
	ID                      string          `json:"id"`
	SchemaVersion           int             `json:"schema_version"`
	DatasetID               string          `json:"dataset_id"`
	ProjectID               string          `json:"project_id"`
	Status                  string          `json:"status"`
	ProfileMatrix           []string        `json:"profile_matrix"`
	IsolatedMemoryNamespace string          `json:"isolated_memory_namespace"`
	IsolatedCacheNamespace  string          `json:"isolated_cache_namespace"`
	BudgetSeconds           int             `json:"budget_seconds"`
	Concurrency             int             `json:"concurrency"`
	ScoringProfile          string          `json:"scoring_profile"`
	Results                 []ProfileResult `json:"results"`
	ReportSHA256            string          `json:"report_sha256"`
	PromotionRecommendation string          `json:"promotion_recommendation"`
	Reason                  string          `json:"reason"`
	ActorID                 string          `json:"actor_id"`
	CreatedAt               time.Time       `json:"created_at"`
}

type CreateDatasetRequest struct {
	ProjectID        string          `json:"project_id"`
	Name             string          `json:"name"`
	SourceKind       string          `json:"source_kind"`
	Repository       string          `json:"repository"`
	BaseRevision     string          `json:"base_revision"`
	TargetRevision   string          `json:"target_revision"`
	KnownPatchSHA256 string          `json:"known_patch_sha256"`
	Exclusions       []string        `json:"exclusions"`
	ScoringProfile   string          `json:"scoring_profile"`
	RetentionDays    int             `json:"retention_days"`
	Metadata         json.RawMessage `json:"metadata"`
}

type LaunchRunRequest struct {
	DatasetID      string   `json:"dataset_id"`
	ProfileMatrix  []string `json:"profile_matrix"`
	BudgetSeconds  int      `json:"budget_seconds"`
	Concurrency    int      `json:"concurrency"`
	ScoringProfile string   `json:"scoring_profile"`
}

type Store interface {
	CreateEvaluationDataset(context.Context, Dataset) (Dataset, error)
	GetEvaluationDataset(context.Context, string) (Dataset, error)
	ListEvaluationDatasets(context.Context, string, int) ([]Dataset, error)
	RecordEvaluationRun(context.Context, Run) (Run, error)
	ListEvaluationRuns(context.Context, string, int) ([]Run, error)
}

func NewDataset(request CreateDatasetRequest, actor string, now time.Time) Dataset {
	sourceKind := strings.TrimSpace(request.SourceKind)
	if sourceKind == "" {
		sourceKind = SourceCuratedFixtures
	}
	scoring := strings.TrimSpace(request.ScoringProfile)
	if scoring == "" {
		scoring = "quality_default_v1"
	}
	retention := request.RetentionDays
	if retention == 0 {
		retention = 30
	}
	metadata := request.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	base := strings.TrimSpace(request.BaseRevision)
	target := strings.TrimSpace(request.TargetRevision)
	known := strings.TrimSpace(request.KnownPatchSHA256)
	return Dataset{
		SchemaVersion:      SchemaVersion,
		ProjectID:          strings.TrimSpace(request.ProjectID),
		Name:               strings.TrimSpace(request.Name),
		SourceKind:         sourceKind,
		Repository:         strings.TrimSpace(request.Repository),
		BaseRevision:       base,
		TargetRevision:     target,
		KnownPatchSHA256:   known,
		HiddenPatchSHA256:  hiddenPatchHash(known),
		Exclusions:         normalizeList(request.Exclusions),
		ScoringProfile:     scoring,
		RetentionDays:      retention,
		ReproducibilityKey: reproducibilityKey(request.ProjectID, request.Repository, base, target, known, normalizeList(request.Exclusions), scoring),
		Metadata:           metadata,
		ActorID:            strings.TrimSpace(actor),
		CreatedAt:          now.UTC(),
	}
}

func SimulateRun(dataset Dataset, request LaunchRunRequest, actor, runID string, now time.Time) (Run, error) {
	profiles := normalizeList(request.ProfileMatrix)
	if len(profiles) == 0 {
		profiles = []string{"local-default"}
	}
	scoring := strings.TrimSpace(request.ScoringProfile)
	if scoring == "" {
		scoring = dataset.ScoringProfile
	}
	budget := request.BudgetSeconds
	if budget == 0 {
		budget = 3600
	}
	concurrency := request.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	if runID == "" {
		runID = "evaluation_run_" + shortHash(dataset.ReproducibilityKey+"\n"+strings.Join(profiles, "\n")+"\n"+now.UTC().Format(time.RFC3339Nano))
	}
	memoryNamespace := isolatedNamespace("memory", dataset.ProjectID, dataset.ID, runID)
	cacheNamespace := isolatedNamespace("cache", dataset.ProjectID, dataset.ID, runID)
	run := Run{
		ID: runID, SchemaVersion: SchemaVersion, DatasetID: dataset.ID, ProjectID: dataset.ProjectID,
		Status: RunCompleted, ProfileMatrix: profiles, IsolatedMemoryNamespace: memoryNamespace, IsolatedCacheNamespace: cacheNamespace,
		BudgetSeconds: budget, Concurrency: concurrency, ScoringProfile: scoring, ActorID: strings.TrimSpace(actor), CreatedAt: now.UTC(),
		Reason:                  "offline deterministic evaluation simulator hid the known patch and used isolated memory/cache namespaces",
		PromotionRecommendation: RecommendationReviewOnly,
	}
	for index, profile := range profiles {
		run.Results = append(run.Results, deterministicProfileResult(dataset, profile, index))
	}
	report, _ := json.Marshal(struct {
		DatasetKey string          `json:"dataset_key"`
		Profiles   []string        `json:"profiles"`
		Results    []ProfileResult `json:"results"`
		Namespaces []string        `json:"namespaces"`
	}{DatasetKey: dataset.ReproducibilityKey, Profiles: profiles, Results: run.Results, Namespaces: []string{memoryNamespace, cacheNamespace}})
	run.ReportSHA256 = sha256Text(string(report))
	if err := run.Validate(); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (dataset *Dataset) Validate() error {
	if dataset.SchemaVersion == 0 {
		dataset.SchemaVersion = SchemaVersion
	}
	dataset.ID = strings.TrimSpace(dataset.ID)
	dataset.ProjectID = strings.TrimSpace(dataset.ProjectID)
	dataset.Name = strings.TrimSpace(dataset.Name)
	dataset.SourceKind = strings.TrimSpace(dataset.SourceKind)
	dataset.Repository = strings.TrimSpace(dataset.Repository)
	dataset.BaseRevision = strings.TrimSpace(dataset.BaseRevision)
	dataset.TargetRevision = strings.TrimSpace(dataset.TargetRevision)
	dataset.KnownPatchSHA256 = strings.TrimSpace(dataset.KnownPatchSHA256)
	dataset.HiddenPatchSHA256 = strings.TrimSpace(dataset.HiddenPatchSHA256)
	dataset.ScoringProfile = strings.TrimSpace(dataset.ScoringProfile)
	dataset.ActorID = strings.TrimSpace(dataset.ActorID)
	dataset.Exclusions = normalizeList(dataset.Exclusions)
	if len(dataset.Metadata) == 0 {
		dataset.Metadata = json.RawMessage(`{}`)
	}
	if dataset.SchemaVersion != SchemaVersion || dataset.ID == "" || dataset.ProjectID == "" || dataset.Name == "" ||
		dataset.Repository == "" || dataset.BaseRevision == "" || dataset.TargetRevision == "" ||
		dataset.KnownPatchSHA256 == "" || dataset.HiddenPatchSHA256 == "" || dataset.ScoringProfile == "" || dataset.ActorID == "" {
		return errors.New("evaluation dataset has missing required fields")
	}
	if dataset.SourceKind != SourceCuratedFixtures && dataset.SourceKind != SourceHistoricalRange {
		return errors.New("evaluation dataset source kind is not registered")
	}
	if len(dataset.KnownPatchSHA256) != 64 || len(dataset.HiddenPatchSHA256) != 64 {
		return errors.New("evaluation dataset hashes must be sha256")
	}
	if dataset.RetentionDays < 1 || dataset.RetentionDays > 3650 {
		return errors.New("evaluation dataset retention is out of range")
	}
	if !json.Valid(dataset.Metadata) {
		return errors.New("evaluation dataset metadata must be valid JSON")
	}
	if dataset.ReproducibilityKey == "" {
		dataset.ReproducibilityKey = reproducibilityKey(dataset.ProjectID, dataset.Repository, dataset.BaseRevision, dataset.TargetRevision, dataset.KnownPatchSHA256, dataset.Exclusions, dataset.ScoringProfile)
	}
	if len(dataset.ReproducibilityKey) != 64 {
		return errors.New("evaluation dataset reproducibility key must be sha256")
	}
	return nil
}

func (run *Run) Validate() error {
	if run.SchemaVersion == 0 {
		run.SchemaVersion = SchemaVersion
	}
	run.ID = strings.TrimSpace(run.ID)
	run.DatasetID = strings.TrimSpace(run.DatasetID)
	run.ProjectID = strings.TrimSpace(run.ProjectID)
	run.Status = strings.TrimSpace(run.Status)
	run.ProfileMatrix = normalizeList(run.ProfileMatrix)
	run.IsolatedMemoryNamespace = strings.TrimSpace(run.IsolatedMemoryNamespace)
	run.IsolatedCacheNamespace = strings.TrimSpace(run.IsolatedCacheNamespace)
	run.ScoringProfile = strings.TrimSpace(run.ScoringProfile)
	run.ReportSHA256 = strings.TrimSpace(run.ReportSHA256)
	run.PromotionRecommendation = strings.TrimSpace(run.PromotionRecommendation)
	run.Reason = strings.TrimSpace(run.Reason)
	run.ActorID = strings.TrimSpace(run.ActorID)
	if run.SchemaVersion != SchemaVersion || run.ID == "" || run.DatasetID == "" || run.ProjectID == "" ||
		run.Status == "" || len(run.ProfileMatrix) == 0 || run.IsolatedMemoryNamespace == "" ||
		run.IsolatedCacheNamespace == "" || run.ScoringProfile == "" || run.ReportSHA256 == "" ||
		run.PromotionRecommendation == "" || run.Reason == "" || run.ActorID == "" {
		return errors.New("evaluation run has missing required fields")
	}
	if run.Status != RunQueued && run.Status != RunCompleted && run.Status != RunRejected {
		return errors.New("evaluation run status is not registered")
	}
	if run.PromotionRecommendation != RecommendationReviewOnly && run.PromotionRecommendation != RecommendationRejected {
		return errors.New("evaluation promotion recommendation is not registered")
	}
	if run.BudgetSeconds < 60 || run.BudgetSeconds > 604800 || run.Concurrency < 1 || run.Concurrency > 8 {
		return errors.New("evaluation run budget or concurrency is out of range")
	}
	if len(run.ReportSHA256) != 64 {
		return errors.New("evaluation report hash must be sha256")
	}
	if !strings.HasPrefix(run.IsolatedMemoryNamespace, "eval://memory/") || !strings.HasPrefix(run.IsolatedCacheNamespace, "eval://cache/") ||
		strings.Contains(run.IsolatedMemoryNamespace, "viking://resources/projects/") ||
		strings.Contains(run.IsolatedCacheNamespace, "viking://resources/projects/") ||
		strings.Contains(run.IsolatedMemoryNamespace, run.ProjectID+"/memory") {
		return errors.New("evaluation namespaces must be isolated from project memory/cache")
	}
	for _, result := range run.Results {
		if err := result.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (result ProfileResult) Validate() error {
	if strings.TrimSpace(result.ProfileID) == "" || !boundedUnit(result.TaskCompletion) || !boundedUnit(result.TestSuccess) ||
		!boundedUnit(result.RegressionRate) || !boundedUnit(result.FindingPrecision) || !boundedUnit(result.FindingRecall) ||
		!boundedUnit(result.DocumentationCompliance) || result.ContextTokens < 0 || result.ContextBytes < 0 ||
		result.WallTimeMillis < 0 || result.CPUTimeMillis < 0 || result.MemoryBytes < 0 ||
		result.OperatorInterventions < 0 || result.UnresolvedUncertainty < 0 || strings.TrimSpace(result.CacheEffect) == "" {
		return errors.New("evaluation profile result is invalid")
	}
	if result.IrrelevantContextRatio < 0 || result.IrrelevantContextRatio > 1 || result.DiffChurn < 0 {
		return errors.New("evaluation profile ratio is invalid")
	}
	if result.PromotionAllowed {
		return errors.New("evaluation runs cannot auto-promote profiles")
	}
	if strings.TrimSpace(result.PromotionBlockedReason) == "" {
		return errors.New("evaluation result must explain blocked promotion")
	}
	return nil
}

func deterministicProfileResult(dataset Dataset, profile string, index int) ProfileResult {
	seed := int64(0)
	for _, b := range []byte(sha256Text(dataset.ReproducibilityKey + "\n" + profile))[:8] {
		seed = seed*31 + int64(b)
	}
	score := float64((seed%30)+65) / 100
	if score < 0 {
		score = -score
	}
	if score > 0.99 {
		score = 0.99
	}
	return ProfileResult{
		ProfileID: profile, TaskCompletion: score, TestSuccess: score - 0.05, RegressionRate: float64(index) / 100,
		DiffChurn: float64((seed%12)+1) / 10, FindingPrecision: 0.80, FindingRecall: 0.75,
		ContextTokens: 12000 + index*512, ContextBytes: 48000 + index*2048, IrrelevantContextRatio: 0.12 + float64(index)/100,
		WallTimeMillis: 60000 + int64(index)*5000, CPUTimeMillis: 54000 + int64(index)*4000, MemoryBytes: 512 * 1024 * 1024,
		CacheEffect: "isolated-cold-cache", DocumentationCompliance: 0.90, OperatorInterventions: 1,
		UnresolvedUncertainty: 1, KnownPatchHidden: true, PromotionAllowed: false,
		PromotionBlockedReason: "historical evaluation is review-only and cannot automatically promote a model, policy, or route",
	}
}

func hiddenPatchHash(known string) string {
	if strings.TrimSpace(known) == "" {
		return ""
	}
	return sha256Text("hidden-patch\n" + strings.TrimSpace(known))
}

func reproducibilityKey(projectID, repository, base, target, patch string, exclusions []string, scoring string) string {
	payload, _ := json.Marshal(map[string]any{
		"project_id": projectID, "repository": repository, "base": base, "target": target,
		"known_patch_sha256": patch, "exclusions": normalizeList(exclusions), "scoring": scoring,
	})
	return sha256Text(string(payload))
}

func isolatedNamespace(kind, projectID, datasetID, runID string) string {
	return fmt.Sprintf("eval://%s/%s/%s/%s", kind, safe(projectID), safe(datasetID), safe(runID))
}

func normalizeList(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func boundedUnit(value float64) bool {
	return value >= 0 && value <= 1
}

func safe(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func shortHash(value string) string {
	return sha256Text(value)[:32]
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
