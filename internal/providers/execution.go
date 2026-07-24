package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	MaxExecutionBatchItems = 16

	ExecutionBehaviorSuccess         = "success"
	ExecutionBehaviorTimeout         = "timeout"
	ExecutionBehaviorRateLimited     = "rate_limited"
	ExecutionBehaviorStreamTruncated = "stream_truncated"

	ExecutionStatusSucceeded      = "succeeded"
	ExecutionStatusFailed         = "failed"
	ExecutionStatusDenied         = "denied"
	ExecutionStatusPartialFailure = "partial_failure"

	ExecutionFailureTimeout           = "provider_timeout"
	ExecutionFailureRateLimited       = "provider_rate_limited"
	ExecutionFailureStreamInterrupted = "provider_stream_interrupted"
	ExecutionFailureBatchPartial      = "provider_batch_partial_failure"
	ExecutionFailureRouteDenied       = "provider_route_denied"
)

type ExecutionScenario struct {
	Behavior         string   `json:"behavior"`
	RetryAfterMillis int      `json:"retry_after_millis,omitempty"`
	StreamChunks     []string `json:"stream_chunks,omitempty"`
}

type ExecutionRequest struct {
	Route    RouteRequest      `json:"route"`
	Scenario ExecutionScenario `json:"scenario"`
}

type ExecutionReport struct {
	Status               string       `json:"status"`
	FailureClass         string       `json:"failure_class,omitempty"`
	Reason               string       `json:"reason"`
	Attempts             int          `json:"attempts"`
	RetryBudget          int          `json:"retry_budget"`
	RetryAfterMillis     int          `json:"retry_after_millis,omitempty"`
	CircuitOpen          bool         `json:"circuit_open"`
	NetworkContacted     bool         `json:"network_contacted"`
	RouteID              string       `json:"route_id"`
	ProviderID           string       `json:"provider_id"`
	EndpointID           string       `json:"endpoint_id"`
	ModelProfileID       string       `json:"model_profile_id"`
	EgressManifestSHA256 string       `json:"egress_manifest_sha256"`
	Stream               StreamReport `json:"stream,omitempty"`
}

type BatchExecutionRequest struct {
	Items []ExecutionRequest `json:"items"`
}

type BatchExecutionReport struct {
	Status          string            `json:"status"`
	ProjectID       string            `json:"project_id"`
	BatchPolicy     string            `json:"batch_policy"`
	Items           []ExecutionReport `json:"items"`
	Succeeded       int               `json:"succeeded"`
	Failures        int               `json:"failures"`
	Denied          int               `json:"denied"`
	PartialFailures int               `json:"partial_failures"`
	Reason          string            `json:"reason"`
}

func (s *Service) SimulateExecution(ctx context.Context, request ExecutionRequest, actor string) (ExecutionReport, error) {
	if s == nil || s.store == nil {
		return ExecutionReport{}, errors.New("provider store is required")
	}
	if strings.TrimSpace(actor) == "" {
		return ExecutionReport{}, errors.New("provider execution actor is required")
	}
	if err := request.Validate(); err != nil {
		return ExecutionReport{}, err
	}
	decision, err := s.SimulateRoute(ctx, request.Route, actor)
	if err != nil {
		return ExecutionReport{}, err
	}
	return executeDecision(decision, request.Scenario, 1)
}

func (s *Service) SimulateBatchExecution(ctx context.Context, request BatchExecutionRequest, actor string) (BatchExecutionReport, error) {
	if s == nil || s.store == nil {
		return BatchExecutionReport{}, errors.New("provider store is required")
	}
	if strings.TrimSpace(actor) == "" {
		return BatchExecutionReport{}, errors.New("provider execution actor is required")
	}
	if len(request.Items) == 0 || len(request.Items) > MaxExecutionBatchItems {
		return BatchExecutionReport{}, errors.New("provider execution batch size is invalid")
	}
	projectID := request.Items[0].Route.ProjectID
	if !safeID.MatchString(projectID) {
		return BatchExecutionReport{}, errors.New("provider execution batch project is invalid")
	}
	report := BatchExecutionReport{Status: ExecutionStatusSucceeded, ProjectID: projectID, Items: make([]ExecutionReport, 0, len(request.Items))}
	for index, item := range request.Items {
		if item.Route.ProjectID != projectID {
			return BatchExecutionReport{}, errors.New("provider execution batch must be project-isolated")
		}
		if err := item.Validate(); err != nil {
			return BatchExecutionReport{}, err
		}
		decision, err := s.SimulateRoute(ctx, item.Route, actor)
		if err != nil {
			return BatchExecutionReport{}, fmt.Errorf("route batch item %d: %w", index, err)
		}
		if decision.Status == DecisionAllowed {
			if decision.Route.BatchPolicy != "project_isolated" {
				itemReport := deniedExecutionReport(decision, "route does not allow provider batch execution")
				itemReport.FailureClass = ExecutionFailureBatchPartial
				report.Items = append(report.Items, itemReport)
				report.Failures++
				report.PartialFailures++
				continue
			}
			report.BatchPolicy = decision.Route.BatchPolicy
		}
		itemReport, err := executeDecision(decision, item.Scenario, len(request.Items))
		if err != nil {
			return BatchExecutionReport{}, fmt.Errorf("execute batch item %d: %w", index, err)
		}
		report.Items = append(report.Items, itemReport)
		switch itemReport.Status {
		case ExecutionStatusSucceeded:
			report.Succeeded++
		case ExecutionStatusDenied:
			report.Denied++
			report.Failures++
			report.PartialFailures++
		default:
			report.Failures++
			report.PartialFailures++
		}
	}
	switch {
	case report.Failures == 0:
		report.Status = ExecutionStatusSucceeded
		report.Reason = "deterministic provider batch fake succeeded"
	case report.Succeeded > 0:
		report.Status = ExecutionStatusPartialFailure
		report.Reason = "deterministic provider batch fake retained partial-failure evidence"
	default:
		report.Status = ExecutionStatusFailed
		report.Reason = "deterministic provider batch fake failed closed"
	}
	if report.BatchPolicy == "" {
		report.BatchPolicy = "none"
	}
	return report, nil
}

func (r ExecutionRequest) Validate() error {
	if err := r.Route.Validate(); err != nil {
		return err
	}
	return r.Scenario.Validate()
}

func (s ExecutionScenario) Validate() error {
	switch s.behavior() {
	case ExecutionBehaviorSuccess, ExecutionBehaviorTimeout, ExecutionBehaviorRateLimited, ExecutionBehaviorStreamTruncated:
	default:
		return errors.New("provider execution scenario behavior is invalid")
	}
	if s.RetryAfterMillis < 0 || s.RetryAfterMillis > 3_600_000 {
		return errors.New("provider execution retry-after is invalid")
	}
	if len(s.StreamChunks) > MaxStreamChunks {
		return errors.New("provider execution stream chunk count is invalid")
	}
	for _, chunk := range s.StreamChunks {
		if len(chunk) == 0 || len(chunk) > MaxStreamChunkBytes {
			return errors.New("provider execution stream chunk is empty or oversized")
		}
	}
	return nil
}

func (s ExecutionScenario) behavior() string {
	if s.Behavior == "" {
		return ExecutionBehaviorSuccess
	}
	return s.Behavior
}

func executeDecision(decision RouteDecision, scenario ExecutionScenario, batchSize int) (ExecutionReport, error) {
	if decision.Status != DecisionAllowed {
		return deniedExecutionReport(decision, decision.Reason), nil
	}
	report := ExecutionReport{
		Status:               ExecutionStatusSucceeded,
		Reason:               "deterministic provider execution fake succeeded",
		Attempts:             1,
		RetryBudget:          decision.Route.RetryBudget,
		NetworkContacted:     false,
		RouteID:              decision.Route.ID,
		ProviderID:           decision.Provider.ID,
		EndpointID:           decision.Endpoint.ID,
		ModelProfileID:       decision.Model.ID,
		EgressManifestSHA256: decision.EgressManifest.ManifestSHA256,
	}
	if batchSize > 1 && decision.Route.BatchPolicy != "project_isolated" {
		report.Status = ExecutionStatusFailed
		report.FailureClass = ExecutionFailureBatchPartial
		report.Reason = "route does not allow provider batch execution"
		report.CircuitOpen = true
		return report, nil
	}
	switch scenario.behavior() {
	case ExecutionBehaviorSuccess:
		return report, nil
	case ExecutionBehaviorTimeout:
		report.Status = ExecutionStatusFailed
		report.FailureClass = ExecutionFailureTimeout
		report.Attempts = boundedAttempts(decision.Route.RetryBudget)
		report.CircuitOpen = true
		report.Reason = fmt.Sprintf("provider fake timed out after %d attempt(s) within endpoint timeout %dms", report.Attempts, decision.Endpoint.TimeoutMillis)
		return report, nil
	case ExecutionBehaviorRateLimited:
		report.Status = ExecutionStatusFailed
		report.FailureClass = ExecutionFailureRateLimited
		report.Attempts = boundedAttempts(decision.Route.RetryBudget)
		report.RetryAfterMillis = scenario.RetryAfterMillis
		if report.RetryAfterMillis == 0 {
			report.RetryAfterMillis = decision.Endpoint.TimeoutMillis
		}
		report.CircuitOpen = true
		report.Reason = fmt.Sprintf("provider fake exhausted retry budget after rate-limit retry-after %dms", report.RetryAfterMillis)
		return report, nil
	case ExecutionBehaviorStreamTruncated:
		chunks := streamChunksForScenario(scenario)
		byteChunks := make([][]byte, 0, len(chunks))
		for _, chunk := range chunks {
			byteChunks = append(byteChunks, []byte(chunk))
		}
		stream, err := ParseStreamEvents(decision.Provider.InterfaceFamily, byteChunks)
		report.Stream = stream
		if err != nil {
			report.Status = ExecutionStatusFailed
			report.FailureClass = ExecutionFailureStreamInterrupted
			report.CircuitOpen = true
			report.Reason = err.Error()
			return report, nil
		}
		report.Reason = "deterministic provider streaming fake completed"
		return report, nil
	default:
		return ExecutionReport{}, errors.New("provider execution scenario behavior is invalid")
	}
}

func deniedExecutionReport(decision RouteDecision, reason string) ExecutionReport {
	return ExecutionReport{
		Status:               ExecutionStatusDenied,
		FailureClass:         ExecutionFailureRouteDenied,
		Reason:               reason,
		Attempts:             0,
		RetryBudget:          0,
		CircuitOpen:          true,
		NetworkContacted:     false,
		RouteID:              decision.EgressManifest.RouteID,
		ProviderID:           decision.EgressManifest.ProviderID,
		EndpointID:           decision.EgressManifest.EndpointID,
		ModelProfileID:       decision.EgressManifest.ModelProfileID,
		EgressManifestSHA256: decision.EgressManifest.ManifestSHA256,
	}
}

func boundedAttempts(retryBudget int) int {
	if retryBudget < 0 {
		return 1
	}
	return retryBudget + 1
}

func streamChunksForScenario(scenario ExecutionScenario) []string {
	if len(scenario.StreamChunks) != 0 {
		return scenario.StreamChunks
	}
	return []string{`data: {"candidates":[{"finishReason":"MAX_TOKENS"}]}` + "\n"}
}
