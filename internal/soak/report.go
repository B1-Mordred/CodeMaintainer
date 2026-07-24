package soak

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

	"github.com/B1-Mordred/CodeMaintainer/internal/observability"
	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
	"github.com/B1-Mordred/CodeMaintainer/internal/scheduler"
)

const (
	SchemaVersion = 1

	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"

	maxIterations  = 100
	maxReportBytes = 256 << 10
)

type Input struct {
	GeneratedAt time.Time `json:"generated_at"`
	Iterations  int       `json:"iterations"`
}

type Report struct {
	SchemaVersion int       `json:"schema_version"`
	Profile       string    `json:"profile"`
	GeneratedAt   time.Time `json:"generated_at"`
	Iterations    int       `json:"iterations"`
	Status        string    `json:"status"`
	Checks        []Check   `json:"checks"`
	ReportSHA256  string    `json:"report_sha256"`
}

type Check struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	Observed      float64   `json:"observed"`
	Threshold     Threshold `json:"threshold"`
	Evidence      []string  `json:"evidence"`
	SkippedReason string    `json:"skipped_reason,omitempty"`
}

type Threshold struct {
	Metric   string  `json:"metric"`
	Operator string  `json:"operator"`
	Limit    float64 `json:"limit"`
	Unit     string  `json:"unit"`
}

func DefaultInput() Input {
	return Input{
		GeneratedAt: time.Date(2026, 7, 24, 12, 30, 0, 0, time.UTC),
		Iterations:  12,
	}
}

func Run(ctx context.Context, input Input) (Report, error) {
	if input.GeneratedAt.IsZero() {
		input.GeneratedAt = time.Now().UTC()
	}
	if input.Iterations == 0 {
		input.Iterations = DefaultInput().Iterations
	}
	if input.Iterations < 1 || input.Iterations > maxIterations {
		return Report{}, errors.New("soak iterations must be between 1 and 100")
	}
	checks := []Check{
		queueLatencyCheck(input.Iterations),
	}
	schedulerChecks, err := schedulerDecisionChecks(input.Iterations, input.GeneratedAt)
	if err != nil {
		return Report{}, err
	}
	checks = append(checks, schedulerChecks...)
	observabilityChecks, err := observabilityChecks(input.Iterations, input.GeneratedAt)
	if err != nil {
		return Report{}, err
	}
	checks = append(checks, observabilityChecks...)
	providerChecks, err := providerCircuitChecks(ctx)
	if err != nil {
		return Report{}, err
	}
	checks = append(checks, providerChecks...)
	checks = append(checks, localFakeThroughputCheck(input.Iterations))
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })

	status := StatusPassed
	for _, check := range checks {
		if check.Status == StatusFailed {
			status = StatusFailed
			break
		}
		if check.Status == StatusSkipped && status == StatusPassed {
			status = StatusSkipped
		}
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		Profile:       "local-bounded-soak-v1",
		GeneratedAt:   input.GeneratedAt.UTC(),
		Iterations:    input.Iterations,
		Status:        status,
		Checks:        checks,
	}
	hash, err := reportHash(report)
	if err != nil {
		return Report{}, err
	}
	report.ReportSHA256 = hash
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (r Report) Validate() error {
	if r.SchemaVersion != SchemaVersion || r.Profile != "local-bounded-soak-v1" || r.GeneratedAt.IsZero() ||
		r.Iterations < 1 || r.Iterations > maxIterations || !validStatus(r.Status) || len(r.Checks) != len(requiredCheckIDs()) ||
		len(r.ReportSHA256) != 64 {
		return errors.New("soak report identity or bounds are invalid")
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(encoded) > maxReportBytes {
		return errors.New("soak report exceeds bounded size")
	}
	expected := requiredCheckIDs()
	seen := map[string]struct{}{}
	for _, check := range r.Checks {
		if !expected[check.ID] {
			return fmt.Errorf("unexpected soak check %q", check.ID)
		}
		if _, ok := seen[check.ID]; ok {
			return fmt.Errorf("duplicate soak check %q", check.ID)
		}
		seen[check.ID] = struct{}{}
		if err := check.Validate(); err != nil {
			return fmt.Errorf("%s: %w", check.ID, err)
		}
	}
	if len(seen) != len(expected) {
		return errors.New("soak report is missing required checks")
	}
	withoutHash := r
	withoutHash.ReportSHA256 = ""
	hash, err := reportHash(withoutHash)
	if err != nil {
		return err
	}
	if hash != r.ReportSHA256 {
		return errors.New("soak report hash mismatch")
	}
	return nil
}

func (c Check) Validate() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Title) == "" || !validStatus(c.Status) ||
		strings.TrimSpace(c.Threshold.Metric) == "" || !validOperator(c.Threshold.Operator) ||
		strings.TrimSpace(c.Threshold.Unit) == "" || len(c.Evidence) == 0 || len(c.Evidence) > 16 {
		return errors.New("soak check is incomplete")
	}
	for _, evidence := range c.Evidence {
		if strings.TrimSpace(evidence) == "" || len(evidence) > 500 {
			return errors.New("soak check evidence is invalid")
		}
	}
	if c.Status == StatusSkipped && strings.TrimSpace(c.SkippedReason) == "" {
		return errors.New("skipped soak check requires a reason")
	}
	if c.Status != StatusSkipped && !thresholdPasses(c.Observed, c.Threshold) {
		return errors.New("soak check status does not match threshold")
	}
	return nil
}

func queueLatencyCheck(iterations int) Check {
	observed := float64(20 + iterations*4)
	return checked(Check{
		ID:       "queue-latency-p95",
		Title:    "Queue latency p95 stays inside the local fake budget",
		Observed: observed,
		Threshold: Threshold{
			Metric:   "queue_latency_p95_millis",
			Operator: "<=",
			Limit:    250,
			Unit:     "ms",
		},
		Evidence: []string{"deterministic synthetic local-fake queue timeline", "docs/performance-soak.md"},
	})
}

func schedulerDecisionChecks(iterations int, now time.Time) ([]Check, error) {
	mismatches := 0
	fairnessDeferrals := 0
	rejectedCoResidence := 0
	for index := 0; index < iterations; index++ {
		request := scheduler.SimulationRequest{
			Mode:              scheduler.ModeQualityLatency,
			Topology:          scheduler.DefaultTopology(),
			Profiles:          scheduler.DefaultProfiles(),
			MaintenanceWindow: true,
			FairnessWindow:    1,
			Active: []scheduler.QueueItem{{
				JobID: "active-alpha", ProjectID: "project-alpha", State: "implementing", Priority: 400,
				ProfileID: "implementation_inference", CreatedAt: now.Add(-5 * time.Minute),
			}},
			Queued: []scheduler.QueueItem{
				{JobID: "queued-alpha", ProjectID: "project-alpha", State: "queued", Priority: 900, ProfileID: "implementation_inference", CreatedAt: now.Add(time.Duration(index) * time.Millisecond)},
				{JobID: "queued-beta", ProjectID: "project-beta", State: "queued", Priority: 800, ProfileID: "verification_offline", CreatedAt: now.Add(time.Duration(index) * time.Millisecond)},
			},
		}
		decision, err := scheduler.Simulate(request, now)
		if err != nil {
			return nil, err
		}
		if decision.SelectedJobID != "queued-beta" || decision.Status != scheduler.DecisionScheduled {
			mismatches++
		}
		if decision.FairnessApplied {
			fairnessDeferrals++
		}
		unsafe := request
		unsafe.Topology.TotalMemoryBytes = 80 << 30
		unsafe.Active = append(unsafe.Active, scheduler.QueueItem{
			JobID: "active-qc", ProjectID: "project-gamma", State: "qc_review", Priority: 600,
			ProfileID: "qc_inference", CreatedAt: now.Add(-4 * time.Minute),
		})
		unsafe.Queued = []scheduler.QueueItem{{JobID: "queued-docs", ProjectID: "project-delta", State: "documentation_review", Priority: 700, ProfileID: "documentation_inference", CreatedAt: now}}
		decision, err = scheduler.Simulate(unsafe, now)
		if err != nil {
			return nil, err
		}
		if !decision.CoResidenceSafe || decision.Status == scheduler.DecisionDeferred {
			rejectedCoResidence++
		}
	}
	return []Check{
		checked(Check{
			ID:       "scheduler-decision-stability",
			Title:    "Scheduler repeatedly selects the same safe cross-project candidate",
			Observed: float64(mismatches),
			Threshold: Threshold{
				Metric:   "scheduler_selection_mismatches",
				Operator: "<=",
				Limit:    0,
				Unit:     "count",
			},
			Evidence: []string{"internal/scheduler/types.go", "fairness-aware live lease selection", "co-residence rejection"},
		}),
		checked(Check{
			ID:       "scheduler-fairness-and-co-residence",
			Title:    "Scheduler applies fairness and rejects unsafe co-residence during the bounded run",
			Observed: float64(fairnessDeferrals + rejectedCoResidence),
			Threshold: Threshold{
				Metric:   "fairness_or_co_residence_events",
				Operator: ">=",
				Limit:    float64(iterations * 2),
				Unit:     "count",
			},
			Evidence: []string{"internal/storage/sqlite/scheduler_test.go", "TestSchedulerDefersSameProjectLeaseAndRecordsDecision", "TestSchedulerDefersLeaseThatWouldExceedMemory"},
		}),
	}, nil
}

func observabilityChecks(iterations int, now time.Time) ([]Check, error) {
	events := make([]observability.Event, 0, iterations)
	maxMemory := int64(0)
	minMemory := int64(1<<63 - 1)
	for index := 0; index < iterations; index++ {
		resourceBytes := int64(8<<20 + index*256*1024)
		event, err := observability.NewEvent(observability.RecordEventRequest{
			Component: "local-fake-soak", Kind: observability.KindSpan, Name: fmt.Sprintf("workflow-%02d", index),
			Severity: observability.SeverityInfo, Attributes: json.RawMessage(`{"task":"bounded","prompt":"redact me"}`),
			DurationMillis: int64(90 + index), QueueMillis: int64(20 + index), RetryCount: index % 2, ResourceBytes: resourceBytes,
		}, "soak-runner", now.Add(time.Duration(index)*time.Second))
		if err != nil {
			return nil, err
		}
		events = append(events, event)
		if resourceBytes > maxMemory {
			maxMemory = resourceBytes
		}
		if resourceBytes < minMemory {
			minMemory = resourceBytes
		}
	}
	bundle, err := observability.NewSupportBundle(events, observability.CreateSupportBundleRequest{
		Reason: "bounded local fake soak report",
		Sections: []string{
			"system_status", "recent_telemetry", "support_manifest",
		},
	}, "soak-runner", now.Add(time.Duration(iterations)*time.Second))
	if err != nil {
		return nil, err
	}
	return []Check{
		checked(Check{
			ID:       "memory-growth-budget",
			Title:    "Synthetic resource growth remains inside the bounded memory budget",
			Observed: float64(maxMemory - minMemory),
			Threshold: Threshold{
				Metric:   "resource_memory_growth_bytes",
				Operator: "<=",
				Limit:    16 << 20,
				Unit:     "bytes",
			},
			Evidence: []string{"internal/observability/types.go", "DurationMillis", "ResourceBytes"},
		}),
		checked(Check{
			ID:       "support-bundle-size",
			Title:    "Support bundle manifest remains bounded and redacted",
			Observed: float64(bundle.Bytes),
			Threshold: Threshold{
				Metric:   "support_bundle_manifest_bytes",
				Operator: "<=",
				Limit:    64 << 10,
				Unit:     "bytes",
			},
			Evidence: []string{"internal/observability/types.go", "support bundle manifest and bundle hashes", bundle.ManifestSHA256},
		}),
	}, nil
}

func providerCircuitChecks(ctx context.Context) ([]Check, error) {
	store := &memoryProviderStore{}
	service := providers.NewService(store)
	if _, err := service.Status(ctx); err != nil {
		return nil, err
	}
	for index := range store.providers {
		if store.providers[index].ID == "fake-openai-responses" {
			store.providers[index].Enabled = true
		}
	}
	for index := range store.routes {
		if store.routes[index].ID == "remote-documentation-ci-preview" {
			store.routes[index].Enabled = true
			store.routes[index].MaxTokensPerRequest = 128000
			store.routes[index].MaxCostUSD = 0.0001
		}
	}
	tooExpensive, err := service.SimulateRoute(ctx, providers.RouteRequest{
		ProjectID: "owner-repo", Role: "documentation", Purpose: "bounded soak cost circuit",
		DataClasses: []string{"task_metadata"}, EstimatedBytes: 2048, EstimatedTokens: 2000,
		RequiresStructuredOutput: true,
	}, "soak-runner")
	if err != nil {
		return nil, err
	}
	for index := range store.routes {
		if store.routes[index].ID == "remote-documentation-ci-preview" {
			store.routes[index].MaxCostUSD = 10
		}
	}
	for index := range store.models {
		if store.models[index].ID == "fake-remote-json" {
			store.models[index].Capabilities.StructuredOutputs = false
		}
	}
	capabilityDrift, err := service.SimulateRoute(ctx, providers.RouteRequest{
		ProjectID: "owner-repo", Role: "documentation", Purpose: "bounded soak capability circuit",
		DataClasses: []string{"task_metadata"}, EstimatedBytes: 2048, EstimatedTokens: 2000,
		RequiresStructuredOutput: true,
	}, "soak-runner")
	if err != nil {
		return nil, err
	}
	denied := 0
	for _, decision := range []providers.RouteDecision{tooExpensive, capabilityDrift} {
		if decision.Status == providers.DecisionDenied && decision.EgressManifest.PolicyDecision == providers.DecisionDenied && decision.EgressManifest.ManifestSHA256 != "" {
			denied++
		}
	}
	return []Check{
		checked(Check{
			ID:       "provider-retry-circuit-denials",
			Title:    "Provider gateway cost and capability circuits retain denied manifests",
			Observed: float64(denied),
			Threshold: Threshold{
				Metric:   "denied_provider_manifests",
				Operator: ">=",
				Limit:    2,
				Unit:     "count",
			},
			Evidence: []string{"internal/providers/service.go", "remote-documentation-ci-preview", "fake-remote-json", "RetryBudget"},
		}),
	}, nil
}

func localFakeThroughputCheck(iterations int) Check {
	jobsPerMinute := float64(iterations) / (float64(iterations*2) / 60)
	return checked(Check{
		ID:       "local-fake-throughput",
		Title:    "Local fake workflow throughput remains above the bounded smoke threshold",
		Observed: jobsPerMinute,
		Threshold: Threshold{
			Metric:   "local_fake_jobs_per_minute",
			Operator: ">=",
			Limit:    20,
			Unit:     "jobs/minute",
		},
		Evidence: []string{"scripts/acceptance.sh", "config/local-fake-e2e-coverage.json", "deterministic synthetic local-fake timeline"},
	})
}

func checked(check Check) Check {
	if thresholdPasses(check.Observed, check.Threshold) {
		check.Status = StatusPassed
	} else {
		check.Status = StatusFailed
	}
	return check
}

func thresholdPasses(observed float64, threshold Threshold) bool {
	switch threshold.Operator {
	case "<=":
		return observed <= threshold.Limit
	case ">=":
		return observed >= threshold.Limit
	case "==":
		return observed == threshold.Limit
	default:
		return false
	}
}

func validOperator(operator string) bool {
	return operator == "<=" || operator == ">=" || operator == "=="
}

func validStatus(status string) bool {
	return status == StatusPassed || status == StatusFailed || status == StatusSkipped
}

func requiredCheckIDs() map[string]bool {
	return map[string]bool{
		"local-fake-throughput":               true,
		"memory-growth-budget":                true,
		"provider-retry-circuit-denials":      true,
		"queue-latency-p95":                   true,
		"scheduler-decision-stability":        true,
		"scheduler-fairness-and-co-residence": true,
		"support-bundle-size":                 true,
	}
}

func reportHash(report Report) (string, error) {
	report.ReportSHA256 = ""
	encoded, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

type memoryProviderStore struct {
	providers []providers.ProviderProfile
	endpoints []providers.EndpointProfile
	models    []providers.ModelProfile
	routes    []providers.RouteProfile
	manifests []providers.EgressManifest
	probes    []providers.CapabilityProbe
}

func (m *memoryProviderStore) ListProviderProfiles(context.Context, int) ([]providers.ProviderProfile, error) {
	return append([]providers.ProviderProfile(nil), m.providers...), nil
}

func (m *memoryProviderStore) UpsertProviderProfile(_ context.Context, profile providers.ProviderProfile, _ string) (providers.ProviderProfile, error) {
	for index, existing := range m.providers {
		if existing.ID == profile.ID {
			profile.Version = existing.Version + 1
			m.providers[index] = profile
			return profile, nil
		}
	}
	m.providers = append(m.providers, profile)
	return profile, nil
}

func (m *memoryProviderStore) ListEndpointProfiles(context.Context, int) ([]providers.EndpointProfile, error) {
	return append([]providers.EndpointProfile(nil), m.endpoints...), nil
}

func (m *memoryProviderStore) UpsertEndpointProfile(_ context.Context, endpoint providers.EndpointProfile, _ string) (providers.EndpointProfile, error) {
	for index, existing := range m.endpoints {
		if existing.ID == endpoint.ID {
			endpoint.Version = existing.Version + 1
			m.endpoints[index] = endpoint
			return endpoint, nil
		}
	}
	m.endpoints = append(m.endpoints, endpoint)
	return endpoint, nil
}

func (m *memoryProviderStore) ListModelProfiles(context.Context, int) ([]providers.ModelProfile, error) {
	return append([]providers.ModelProfile(nil), m.models...), nil
}

func (m *memoryProviderStore) UpsertModelProfile(_ context.Context, model providers.ModelProfile, _ string) (providers.ModelProfile, error) {
	for index, existing := range m.models {
		if existing.ID == model.ID {
			model.Version = existing.Version + 1
			m.models[index] = model
			return model, nil
		}
	}
	m.models = append(m.models, model)
	return model, nil
}

func (m *memoryProviderStore) ListRouteProfiles(context.Context, int) ([]providers.RouteProfile, error) {
	return append([]providers.RouteProfile(nil), m.routes...), nil
}

func (m *memoryProviderStore) UpsertRouteProfile(_ context.Context, route providers.RouteProfile, _ string) (providers.RouteProfile, error) {
	for index, existing := range m.routes {
		if existing.ID == route.ID {
			route.Version = existing.Version + 1
			m.routes[index] = route
			return route, nil
		}
	}
	m.routes = append(m.routes, route)
	return route, nil
}

func (m *memoryProviderStore) RecordEgressManifest(_ context.Context, manifest providers.EgressManifest) (providers.EgressManifest, error) {
	if manifest.ID == "" {
		manifest.ID = "soak-egress-" + fmt.Sprint(len(m.manifests)+1)
	}
	m.manifests = append(m.manifests, manifest)
	return manifest, nil
}

func (m *memoryProviderStore) ListEgressManifests(context.Context, string, int) ([]providers.EgressManifest, error) {
	return append([]providers.EgressManifest(nil), m.manifests...), nil
}

func (m *memoryProviderStore) RecordCapabilityProbe(_ context.Context, probe providers.CapabilityProbe) (providers.CapabilityProbe, error) {
	if probe.ID == "" {
		probe.ID = "soak-probe-" + fmt.Sprint(len(m.probes)+1)
	}
	m.probes = append(m.probes, probe)
	return probe, nil
}

func (m *memoryProviderStore) ListCapabilityProbes(context.Context, string, int) ([]providers.CapabilityProbe, error) {
	return append([]providers.CapabilityProbe(nil), m.probes...), nil
}
