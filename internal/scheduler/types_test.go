package scheduler

import (
	"testing"
	"time"
)

func TestSimulateSelectsUrgentJobWithinResourceLimits(t *testing.T) {
	now := time.Date(2026, 7, 24, 2, 45, 0, 0, time.UTC)
	decision, err := Simulate(SimulationRequest{
		Mode: ModeQualityLatency, Topology: DefaultTopology(), Profiles: DefaultProfiles(), MaintenanceWindow: true,
		Queued: []QueueItem{
			{JobID: "job_low", ProjectID: "project_a", State: "queued", Priority: 10, ProfileID: "verification_offline", CreatedAt: now.Add(-time.Hour), DeadlineAt: now.Add(2 * time.Hour)},
			{JobID: "job_urgent", ProjectID: "project_b", State: "queued", Priority: 5, ProfileID: "documentation_inference", CreatedAt: now, DeadlineAt: now.Add(10 * time.Minute)},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != DecisionScheduled || decision.SelectedJobID != "job_urgent" || !decision.CoResidenceSafe {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestSimulateDefersUnsafeCoResidence(t *testing.T) {
	now := time.Date(2026, 7, 24, 2, 45, 0, 0, time.UTC)
	topology := DefaultTopology()
	topology.TotalMemoryBytes = 64 << 30
	decision, err := Simulate(SimulationRequest{
		Topology: topology, Profiles: DefaultProfiles(), MaintenanceWindow: true,
		Active: []QueueItem{{JobID: "job_active", ProjectID: "project_a", State: "implementing", Priority: 100, ProfileID: "implementation_inference", CreatedAt: now}},
		Queued: []QueueItem{{JobID: "job_qc", ProjectID: "project_b", State: "queued", Priority: 100, ProfileID: "qc_inference", CreatedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != DecisionDeferred || len(decision.DeferredJobIDs) != 1 || decision.DeferredJobIDs[0] != "job_qc" {
		t.Fatalf("unsafe co-residence decision = %#v", decision)
	}
}

func TestSimulateAppliesFairnessAndThroughputBatching(t *testing.T) {
	now := time.Date(2026, 7, 24, 2, 45, 0, 0, time.UTC)
	decision, err := Simulate(SimulationRequest{
		Mode: ModeThroughputBatching, Topology: DefaultTopology(), Profiles: DefaultProfiles(), MaintenanceWindow: true, FairnessWindow: 1,
		Active: []QueueItem{{JobID: "job_active", ProjectID: "project_a", State: "implementing", Priority: 100, ProfileID: "implementation_inference", CreatedAt: now, ModelID: "implementation"}},
		Queued: []QueueItem{
			{JobID: "job_same_project", ProjectID: "project_a", State: "queued", Priority: 1000, ProfileID: "implementation_inference", CreatedAt: now, ModelID: "implementation"},
			{JobID: "job_other_project", ProjectID: "project_b", State: "queued", Priority: 100, ProfileID: "implementation_inference", CreatedAt: now, ModelID: "implementation"},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != DecisionScheduled || decision.SelectedJobID != "job_other_project" || !decision.FairnessApplied || decision.ModelBatchGroup != "implementation" {
		t.Fatalf("fairness throughput decision = %#v", decision)
	}
}

func TestSimulateRejectsUnsafeInput(t *testing.T) {
	if _, err := Simulate(SimulationRequest{Topology: DefaultTopology(), Profiles: DefaultProfiles(), MaintenanceWindow: true, Queued: []QueueItem{{JobID: "../bad", ProjectID: "project", State: "queued", ProfileID: "verification_offline"}}}, time.Now()); err == nil {
		t.Fatal("unsafe job ID accepted")
	}
}

func TestWorkflowStatesMapToBoundedProfiles(t *testing.T) {
	for state, profile := range map[string]string{
		"queued":                   "controller_workflow",
		"preparing_dependencies":   "dependency_preparation",
		"verifying_full":           "verification_offline",
		"implementing":             "implementation_inference",
		"test_design_review":       "test_designer_inference",
		"documentation_review":     "documentation_inference",
		"qc_review":                "qc_inference",
		"awaiting_task_approval":   "controller_workflow",
		"awaiting_golden_approval": "controller_workflow",
		"publishing_branch":        "controller_workflow",
		"unexpected_future_state":  "controller_workflow",
	} {
		if got := ProfileIDForState(state); got != profile {
			t.Fatalf("ProfileIDForState(%q) = %q, want %q", state, got, profile)
		}
		if profile != "controller_workflow" && RunnerIDForProfile(profile) == "" {
			t.Fatalf("profile %q lacks runner ID", profile)
		}
	}
	if ModelIDForProfile("implementation_inference") != "implementation" || ModelIDForProfile("verification_offline") != "" {
		t.Fatal("model profile mapping is incorrect")
	}
	now := time.Date(2026, 7, 24, 3, 0, 0, 0, time.UTC)
	if PriorityForDeadline(now, now.Add(time.Minute), now.Add(-time.Hour)) <= PriorityForDeadline(now, now.Add(12*time.Hour), now.Add(-time.Hour)) {
		t.Fatal("urgent deadline did not receive higher priority")
	}
}
