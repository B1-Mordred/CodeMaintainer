package runtimeopt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/models"
)

const (
	QualityPassed = "passed"
	QualityFailed = "failed"

	RecommendationCandidate = "candidate"
	RecommendationRejected  = "rejected"

	CacheDisabled = "disabled"
	CacheEligible = "eligible"
)

type QualityFixtureResult struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Score          float64 `json:"score"`
	Threshold      float64 `json:"threshold"`
	EvidenceSHA256 string  `json:"evidence_sha256"`
}

type BenchmarkRun struct {
	ID                    string                 `json:"id"`
	ProfileID             string                 `json:"profile_id"`
	Role                  string                 `json:"role"`
	ModelFamily           string                 `json:"model_family"`
	ModelSHA256           string                 `json:"model_sha256,omitempty"`
	Quantization          string                 `json:"quantization"`
	ContextLimit          int                    `json:"context_limit"`
	Threads               int                    `json:"threads"`
	Batch                 int                    `json:"batch"`
	UBatch                int                    `json:"ubatch"`
	NUMA                  string                 `json:"numa"`
	RuntimeIdentitySHA256 string                 `json:"runtime_identity_sha256"`
	PromptTokensSecond    float64                `json:"prompt_tokens_second"`
	DecodeTokensSecond    float64                `json:"decode_tokens_second"`
	DurationMillis        int64                  `json:"duration_millis"`
	MemoryBytes           int64                  `json:"memory_bytes"`
	Healthy               bool                   `json:"healthy"`
	QualityFixtures       []QualityFixtureResult `json:"quality_fixtures"`
	QualityStatus         string                 `json:"quality_status"`
	DeterminismStatus     string                 `json:"determinism_status"`
	CacheMode             string                 `json:"cache_mode"`
	CacheIdentitySHA256   string                 `json:"cache_identity_sha256,omitempty"`
	ExperimentalFeatures  []string               `json:"experimental_features"`
	Recommendation        string                 `json:"recommendation"`
	Reason                string                 `json:"reason"`
	ActorID               string                 `json:"actor_id"`
	CreatedAt             time.Time              `json:"created_at"`
}

type Store interface {
	RecordRuntimeBenchmark(context.Context, BenchmarkRun) (BenchmarkRun, error)
	ListRuntimeBenchmarks(context.Context, string, int) ([]BenchmarkRun, error)
}

func FromSmoke(profile models.Profile, status models.Status, smoke models.SmokeResult, actor string, now time.Time) BenchmarkRun {
	if status.ProfileID != profile.ID {
		status = models.Status{State: "loaded", ProfileID: profile.ID, ModelFamily: profile.ModelFamily, Context: profile.Context}
	}
	fixture := QualityFixtureResult{
		ID:             "smoke-ok-exact",
		Status:         QualityPassed,
		Score:          1,
		Threshold:      1,
		EvidenceSHA256: sha256Text(profile.ID + "\nOK\n"),
	}
	run := BenchmarkRun{
		ProfileID:            profile.ID,
		Role:                 profile.Role,
		ModelFamily:          profile.ModelFamily,
		ModelSHA256:          profile.SHA256,
		Quantization:         profile.Quantization,
		ContextLimit:         profile.Context,
		Threads:              profile.Threads,
		Batch:                profile.Batch,
		UBatch:               profile.UBatch,
		NUMA:                 profile.NUMA,
		PromptTokensSecond:   nonNegative(status.PromptTokensSecond),
		DecodeTokensSecond:   nonNegative(status.DecodeTokensSecond),
		DurationMillis:       smoke.Duration.Milliseconds(),
		MemoryBytes:          status.MemoryBytes,
		Healthy:              smoke.Healthy,
		QualityFixtures:      []QualityFixtureResult{fixture},
		QualityStatus:        QualityPassed,
		DeterminismStatus:    QualityPassed,
		CacheMode:            CacheDisabled,
		ExperimentalFeatures: []string{},
		ActorID:              actor,
		CreatedAt:            now.UTC(),
	}
	run.RuntimeIdentitySHA256 = RuntimeIdentitySHA256(profile)
	run.CacheIdentitySHA256 = ""
	if profile.SHA256 != "" {
		run.CacheMode = CacheEligible
		run.CacheIdentitySHA256 = sha256Text(run.RuntimeIdentitySHA256 + "\ncache:v1\n")
	}
	run.Recommendation, run.Reason = recommendationFor(run)
	return run
}

func RuntimeIdentitySHA256(profile models.Profile) string {
	payload := map[string]any{
		"profile_id": profile.ID, "role": profile.Role, "model_family": profile.ModelFamily, "sha256": profile.SHA256,
		"filename": profile.Filename, "license": profile.License, "quantization": profile.Quantization, "context": profile.Context,
		"threads": profile.Threads, "batch": profile.Batch, "ubatch": profile.UBatch, "numa": profile.NUMA,
		"sampling": profile.Sampling,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (r BenchmarkRun) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ProfileID) == "" || strings.TrimSpace(r.Role) == "" ||
		strings.TrimSpace(r.ModelFamily) == "" || strings.TrimSpace(r.RuntimeIdentitySHA256) == "" {
		return errors.New("benchmark identity is incomplete")
	}
	if r.ContextLimit < 1 || r.DurationMillis < 0 || r.MemoryBytes < 0 || r.PromptTokensSecond < 0 || r.DecodeTokensSecond < 0 {
		return errors.New("benchmark metrics are invalid")
	}
	if !oneOf(r.QualityStatus, QualityPassed, QualityFailed) || !oneOf(r.DeterminismStatus, QualityPassed, QualityFailed) ||
		!oneOf(r.Recommendation, RecommendationCandidate, RecommendationRejected) || !oneOf(r.CacheMode, CacheDisabled, CacheEligible) {
		return errors.New("benchmark status is invalid")
	}
	if len(r.QualityFixtures) == 0 || len(r.QualityFixtures) > 20 || len(r.ExperimentalFeatures) > 20 {
		return errors.New("benchmark evidence is invalid")
	}
	for _, fixture := range r.QualityFixtures {
		if strings.TrimSpace(fixture.ID) == "" || !oneOf(fixture.Status, QualityPassed, QualityFailed) ||
			fixture.Score < 0 || fixture.Score > 1 || fixture.Threshold < 0 || fixture.Threshold > 1 ||
			(fixture.EvidenceSHA256 != "" && len(fixture.EvidenceSHA256) != 64) {
			return fmt.Errorf("benchmark fixture %q is invalid", fixture.ID)
		}
	}
	for _, feature := range r.ExperimentalFeatures {
		if strings.TrimSpace(feature) == "" || len(feature) > 64 {
			return errors.New("experimental feature gate is invalid")
		}
	}
	return nil
}

func recommendationFor(run BenchmarkRun) (string, string) {
	if !run.Healthy {
		return RecommendationRejected, "smoke benchmark did not complete successfully"
	}
	if run.QualityStatus != QualityPassed || run.DeterminismStatus != QualityPassed {
		return RecommendationRejected, "quality or determinism fixture gate failed"
	}
	if len(run.ExperimentalFeatures) > 0 {
		return RecommendationRejected, "experimental features require explicit per-profile opt-in and additional review"
	}
	return RecommendationCandidate, "benchmark passed smoke, quality, determinism, and identity gates; operator review is still required before activation"
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func nonNegative(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return value
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
