package runtimeopt

import (
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/models"
)

func TestFromSmokeCreatesCandidateWithoutActivatingExperimentalFeatures(t *testing.T) {
	profile := models.Profile{
		ID: "implementation", Role: "implementation", ModelFamily: "fixture",
		SHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Context: 4096, Quantization: "Q8_0", Threads: 4, Batch: 512, UBatch: 128, NUMA: "disabled",
	}
	status := models.Status{ProfileID: "implementation", PromptTokensSecond: 12.5, DecodeTokensSecond: 9.25, MemoryBytes: 1048576}
	run := FromSmoke(profile, status, models.SmokeResult{ProfileID: "implementation", Duration: 2 * time.Second, Healthy: true}, "operator", time.Unix(10, 0))
	run.ID = "runtime_benchmark_fixture"
	if run.Recommendation != RecommendationCandidate || run.CacheMode != CacheEligible || run.CacheIdentitySHA256 == "" {
		t.Fatalf("unexpected benchmark run: %#v", run)
	}
	if err := run.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBenchmarkRejectsExperimentalFeatureGate(t *testing.T) {
	run := FromSmoke(models.Profile{ID: "qc", Role: "qc", ModelFamily: "fixture", Context: 4096, Quantization: "Q4_0"},
		models.Status{ProfileID: "qc"}, models.SmokeResult{ProfileID: "qc", Duration: time.Millisecond, Healthy: true}, "operator", time.Now())
	run.ExperimentalFeatures = []string{"speculative_decoding"}
	run.Recommendation, run.Reason = recommendationFor(run)
	if run.Recommendation != RecommendationRejected {
		t.Fatalf("experimental benchmark should require explicit review, got %#v", run)
	}
}
