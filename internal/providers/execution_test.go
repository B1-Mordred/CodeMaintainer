package providers

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProviderExecutionFakesTimeoutAndRateLimitRetryCircuits(t *testing.T) {
	service, _ := newRemoteExecutionService(t)
	request := remoteDocumentationRequest("provider-timeout-job")

	timeout, err := service.SimulateExecution(context.Background(), ExecutionRequest{
		Route:    request,
		Scenario: ExecutionScenario{Behavior: ExecutionBehaviorTimeout},
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if timeout.Status != ExecutionStatusFailed || timeout.FailureClass != ExecutionFailureTimeout || !timeout.CircuitOpen {
		t.Fatalf("timeout fake did not fail closed with circuit evidence: %#v", timeout)
	}
	if timeout.Attempts != 2 || timeout.RetryBudget != 1 || timeout.NetworkContacted {
		t.Fatalf("timeout fake did not respect bounded retry/no-network semantics: %#v", timeout)
	}
	if timeout.EgressManifestSHA256 == "" || timeout.ProviderID != "fake-openai-responses" {
		t.Fatalf("timeout fake lost retained route evidence: %#v", timeout)
	}

	rateLimited, err := service.SimulateExecution(context.Background(), ExecutionRequest{
		Route:    remoteDocumentationRequest("provider-rate-limit-job"),
		Scenario: ExecutionScenario{Behavior: ExecutionBehaviorRateLimited, RetryAfterMillis: 2500},
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if rateLimited.Status != ExecutionStatusFailed || rateLimited.FailureClass != ExecutionFailureRateLimited || !rateLimited.CircuitOpen {
		t.Fatalf("rate-limit fake did not fail closed with circuit evidence: %#v", rateLimited)
	}
	if rateLimited.Attempts != 2 || rateLimited.RetryAfterMillis != 2500 || rateLimited.NetworkContacted {
		t.Fatalf("rate-limit fake did not retain bounded retry metadata: %#v", rateLimited)
	}
}

func TestProviderExecutionFakeBatchPartialFailureRetainsPerItemEvidence(t *testing.T) {
	service, store := newRemoteExecutionService(t)
	report, err := service.SimulateBatchExecution(context.Background(), BatchExecutionRequest{Items: []ExecutionRequest{
		{
			Route:    remoteDocumentationRequest("provider-batch-success-job"),
			Scenario: ExecutionScenario{Behavior: ExecutionBehaviorSuccess},
		},
		{
			Route:    remoteDocumentationRequest("provider-batch-timeout-job"),
			Scenario: ExecutionScenario{Behavior: ExecutionBehaviorTimeout},
		},
	}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != ExecutionStatusPartialFailure || report.Succeeded != 1 || report.Failures != 1 || report.PartialFailures != 1 {
		t.Fatalf("batch fake did not retain partial-failure counters: %#v", report)
	}
	if report.BatchPolicy != "project_isolated" || report.ProjectID != "owner-repo" {
		t.Fatalf("batch fake did not enforce project isolation policy: %#v", report)
	}
	if len(report.Items) != 2 || report.Items[0].EgressManifestSHA256 == "" || report.Items[1].EgressManifestSHA256 == "" {
		t.Fatalf("batch fake lost per-item egress evidence: %#v", report)
	}
	if len(store.manifests) != 2 {
		t.Fatalf("batch fake retained %d manifests, want 2", len(store.manifests))
	}
	for _, item := range report.Items {
		if item.NetworkContacted {
			t.Fatalf("execution fake attempted network contact: %#v", item)
		}
	}
}

func TestProviderExecutionFakeRejectsCrossProjectBatchAndCapturesStreamInterruption(t *testing.T) {
	service, _ := newRemoteExecutionService(t)
	crossProject := remoteDocumentationRequest("provider-batch-other-project")
	crossProject.ProjectID = "other-repo"
	if _, err := service.SimulateBatchExecution(context.Background(), BatchExecutionRequest{Items: []ExecutionRequest{
		{Route: remoteDocumentationRequest("provider-batch-a"), Scenario: ExecutionScenario{Behavior: ExecutionBehaviorSuccess}},
		{Route: crossProject, Scenario: ExecutionScenario{Behavior: ExecutionBehaviorSuccess}},
	}}, "tester"); err == nil || !strings.Contains(err.Error(), "project-isolated") {
		t.Fatalf("cross-project batch was accepted: %v", err)
	}

	streamed, err := service.SimulateExecution(context.Background(), ExecutionRequest{
		Route: remoteDocumentationRequest("provider-stream-truncated-job"),
		Scenario: ExecutionScenario{
			Behavior:     ExecutionBehaviorStreamTruncated,
			StreamChunks: []string{`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n" + `data: {"finish_reason":"length"}` + "\n"},
		},
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if streamed.Status != ExecutionStatusFailed || streamed.FailureClass != ExecutionFailureStreamInterrupted || !streamed.Stream.Truncated {
		t.Fatalf("stream interruption fake did not retain truncation evidence: %#v", streamed)
	}
}

func newRemoteExecutionService(t *testing.T) (*Service, *memoryStore) {
	t.Helper()
	store := &memoryStore{}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 7, 24, 12, 10, 0, 0, time.UTC) }
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index := range store.providers {
		if store.providers[index].ID == "fake-openai-responses" {
			store.providers[index].Enabled = true
		}
	}
	for index := range store.routes {
		if store.routes[index].ID == "remote-documentation-ci-preview" {
			store.routes[index].Enabled = true
			store.routes[index].MaxCostUSD = 1
			store.routes[index].RetryBudget = 1
			store.routes[index].BatchPolicy = "project_isolated"
		}
	}
	if _, err := service.ProbeModel(context.Background(), "fake-remote-json", "tester"); err != nil {
		t.Fatal(err)
	}
	return service, store
}

func remoteDocumentationRequest(jobID string) RouteRequest {
	return RouteRequest{
		JobID: jobID, ProjectID: "owner-repo", Role: "documentation", Purpose: "documentation provider execution fixture",
		DataClasses: []string{"task_metadata", "documentation_public_source"}, ArtifactIDs: []string{"artifact-provider-docs"},
		EstimatedBytes: 2048, EstimatedTokens: 2000, RequiresStructuredOutput: true,
	}
}
