package scheduler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ModeQualityLatency     = "quality_latency"
	ModeThroughputBatching = "throughput_batching"

	DecisionScheduled = "scheduled"
	DecisionDeferred  = "deferred"
	DecisionRejected  = "rejected"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Topology struct {
	HostID              string `json:"host_id"`
	PhysicalCores       int    `json:"physical_cores"`
	LogicalCores        int    `json:"logical_cores"`
	TotalMemoryBytes    int64  `json:"total_memory_bytes"`
	ReservedMemoryBytes int64  `json:"reserved_memory_bytes"`
	IOPressure          string `json:"io_pressure"`
	ThermalState        string `json:"thermal_state"`
}

type ResourceProfile struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	CPUCores    int    `json:"cpu_cores"`
	MemoryBytes int64  `json:"memory_bytes"`
	ModelID     string `json:"model_id,omitempty"`
	RunnerID    string `json:"runner_id,omitempty"`
	Network     string `json:"network"`
}

type QueueItem struct {
	JobID      string    `json:"job_id"`
	ProjectID  string    `json:"project_id"`
	State      string    `json:"state"`
	Priority   int       `json:"priority"`
	ProfileID  string    `json:"profile_id"`
	CreatedAt  time.Time `json:"created_at"`
	DeadlineAt time.Time `json:"deadline_at,omitempty"`
	ModelID    string    `json:"model_id,omitempty"`
	RunnerID   string    `json:"runner_id,omitempty"`
	Reason     string    `json:"reason"`
}

type SimulationRequest struct {
	Mode              string            `json:"mode"`
	Topology          Topology          `json:"topology"`
	Profiles          []ResourceProfile `json:"profiles"`
	Active            []QueueItem       `json:"active"`
	Queued            []QueueItem       `json:"queued"`
	MaintenanceWindow bool              `json:"maintenance_window"`
	FairnessWindow    int               `json:"fairness_window"`
}

type Decision struct {
	ID                string    `json:"id,omitempty"`
	Mode              string    `json:"mode"`
	SelectedJobID     string    `json:"selected_job_id,omitempty"`
	SelectedProjectID string    `json:"selected_project_id,omitempty"`
	SelectedProfileID string    `json:"selected_profile_id,omitempty"`
	Status            string    `json:"status"`
	Reason            string    `json:"reason"`
	RejectedJobIDs    []string  `json:"rejected_job_ids"`
	DeferredJobIDs    []string  `json:"deferred_job_ids"`
	CoResidenceSafe   bool      `json:"co_residence_safe"`
	FairnessApplied   bool      `json:"fairness_applied"`
	ModelBatchGroup   string    `json:"model_batch_group,omitempty"`
	ResourceSummary   string    `json:"resource_summary"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
}

type Store interface {
	RecordSchedulerDecision(context.Context, Decision) (Decision, error)
	ListSchedulerDecisions(context.Context, int) ([]Decision, error)
}

func DefaultTopology() Topology {
	return Topology{
		HostID: "local-dual-xeon-ci-profile", PhysicalCores: 44, LogicalCores: 88,
		TotalMemoryBytes: 128 << 30, ReservedMemoryBytes: 16 << 30,
		IOPressure: "normal", ThermalState: "unknown",
	}
}

func DefaultProfiles() []ResourceProfile {
	return []ResourceProfile{
		{ID: "controller_reserved", Kind: "controller", CPUCores: 2, MemoryBytes: 4 << 30, Network: "none"},
		{ID: "verification_offline", Kind: "verification", CPUCores: 8, MemoryBytes: 16 << 30, RunnerID: "verification", Network: "offline"},
		{ID: "implementation_inference", Kind: "implementation", CPUCores: 16, MemoryBytes: 48 << 30, ModelID: "implementation", RunnerID: "implementation", Network: "inference_only"},
		{ID: "qc_inference", Kind: "qc", CPUCores: 16, MemoryBytes: 48 << 30, ModelID: "qc", RunnerID: "qc", Network: "inference_only"},
		{ID: "documentation_inference", Kind: "documentation", CPUCores: 12, MemoryBytes: 32 << 30, ModelID: "documentation", RunnerID: "documentation", Network: "inference_only"},
		{ID: "test_designer_inference", Kind: "test_designer", CPUCores: 12, MemoryBytes: 32 << 30, ModelID: "test_designer", RunnerID: "test_designer", Network: "inference_only"},
	}
}

func Simulate(request SimulationRequest, now time.Time) (Decision, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if request.Mode == "" {
		request.Mode = ModeQualityLatency
	}
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	decision := Decision{
		Mode: request.Mode, Status: DecisionDeferred, CoResidenceSafe: true,
		RejectedJobIDs: []string{}, DeferredJobIDs: []string{},
		CreatedAt: now,
	}
	if !request.MaintenanceWindow {
		for _, item := range request.Queued {
			decision.DeferredJobIDs = append(decision.DeferredJobIDs, item.JobID)
		}
		decision.Reason = "outside configured maintenance window"
		decision.ResourceSummary = resourceSummary(request, nil)
		return decision, decision.Validate()
	}
	profiles := map[string]ResourceProfile{}
	for _, profile := range request.Profiles {
		profiles[profile.ID] = profile
	}
	activeMemory := request.Topology.ReservedMemoryBytes
	activeCPU := 0
	activeProjects := map[string]struct{}{}
	activeModels := map[string]struct{}{}
	for _, item := range request.Active {
		profile := profiles[item.ProfileID]
		activeMemory += profile.MemoryBytes
		activeCPU += profile.CPUCores
		activeProjects[item.ProjectID] = struct{}{}
		if profile.ModelID != "" {
			activeModels[profile.ModelID] = struct{}{}
		}
	}
	availableMemory := request.Topology.TotalMemoryBytes - activeMemory
	availableCPU := request.Topology.LogicalCores - activeCPU
	if availableMemory <= 0 || availableCPU <= 0 {
		decision.CoResidenceSafe = false
		for _, item := range request.Queued {
			decision.DeferredJobIDs = append(decision.DeferredJobIDs, item.JobID)
		}
		decision.Reason = "active workloads plus controller reservation exhaust host capacity"
		decision.ResourceSummary = resourceSummary(request, nil)
		return decision, decision.Validate()
	}
	candidates := append([]QueueItem(nil), request.Queued...)
	sortQueue(candidates, request.Mode, now)
	for _, item := range candidates {
		profile, ok := profiles[item.ProfileID]
		if !ok {
			decision.RejectedJobIDs = append(decision.RejectedJobIDs, item.JobID)
			continue
		}
		if _, active := activeProjects[item.ProjectID]; active && len(activeProjects) > 0 && request.FairnessWindow > 0 {
			decision.DeferredJobIDs = append(decision.DeferredJobIDs, item.JobID)
			decision.FairnessApplied = true
			continue
		}
		if profile.MemoryBytes > availableMemory || profile.CPUCores > availableCPU {
			decision.DeferredJobIDs = append(decision.DeferredJobIDs, item.JobID)
			continue
		}
		decision.Status = DecisionScheduled
		decision.SelectedJobID = item.JobID
		decision.SelectedProjectID = item.ProjectID
		decision.SelectedProfileID = item.ProfileID
		if request.Mode == ModeThroughputBatching && profile.ModelID != "" {
			decision.ModelBatchGroup = profile.ModelID
			if _, loaded := activeModels[profile.ModelID]; loaded {
				decision.Reason = "selected compatible queued job using already-loaded model profile"
			} else {
				decision.Reason = "selected highest-priority queued job and will establish model batch group"
			}
		} else {
			decision.Reason = "selected safest highest-priority queued job within CPU and memory limits"
		}
		decision.ResourceSummary = resourceSummary(request, &profile)
		return decision, decision.Validate()
	}
	decision.Reason = "no queued job fit resource, fairness, and profile constraints"
	decision.ResourceSummary = resourceSummary(request, nil)
	return decision, decision.Validate()
}

func (r SimulationRequest) Validate() error {
	if r.Mode == "" {
		r.Mode = ModeQualityLatency
	}
	if r.Mode != ModeQualityLatency && r.Mode != ModeThroughputBatching {
		return errors.New("scheduler mode is invalid")
	}
	if err := r.Topology.Validate(); err != nil {
		return err
	}
	if len(r.Profiles) == 0 || len(r.Profiles) > 64 || len(r.Active) > 128 || len(r.Queued) > 256 || r.FairnessWindow < 0 || r.FairnessWindow > 1000 {
		return errors.New("scheduler simulation input is invalid")
	}
	seenProfiles := map[string]struct{}{}
	for _, profile := range r.Profiles {
		if err := profile.Validate(); err != nil {
			return err
		}
		if _, ok := seenProfiles[profile.ID]; ok {
			return errors.New("scheduler profile IDs must be unique")
		}
		seenProfiles[profile.ID] = struct{}{}
	}
	seenJobs := map[string]struct{}{}
	for _, items := range [][]QueueItem{r.Active, r.Queued} {
		for _, item := range items {
			if err := item.Validate(); err != nil {
				return err
			}
			if _, ok := seenProfiles[item.ProfileID]; !ok {
				return errors.New("scheduler queue item references an unknown profile")
			}
			if _, ok := seenJobs[item.JobID]; ok {
				return errors.New("scheduler queue item job IDs must be unique")
			}
			seenJobs[item.JobID] = struct{}{}
		}
	}
	return nil
}

func (t Topology) Validate() error {
	if !safeID.MatchString(t.HostID) || t.PhysicalCores < 1 || t.PhysicalCores > 1024 ||
		t.LogicalCores < t.PhysicalCores || t.LogicalCores > 2048 ||
		t.TotalMemoryBytes < 1<<30 || t.TotalMemoryBytes > 16<<40 ||
		t.ReservedMemoryBytes < 0 || t.ReservedMemoryBytes >= t.TotalMemoryBytes ||
		(t.IOPressure != "normal" && t.IOPressure != "elevated" && t.IOPressure != "high") ||
		(t.ThermalState != "unknown" && t.ThermalState != "normal" && t.ThermalState != "hot") {
		return errors.New("scheduler topology is invalid")
	}
	return nil
}

func (p ResourceProfile) Validate() error {
	if !safeID.MatchString(p.ID) || !safeID.MatchString(p.Kind) || p.CPUCores < 1 || p.CPUCores > 1024 ||
		p.MemoryBytes < 1<<20 || p.MemoryBytes > 16<<40 ||
		(p.ModelID != "" && !safeID.MatchString(p.ModelID)) ||
		(p.RunnerID != "" && !safeID.MatchString(p.RunnerID)) ||
		(p.Network != "none" && p.Network != "offline" && p.Network != "inference_only" && p.Network != "dependency_egress") {
		return errors.New("scheduler resource profile is invalid")
	}
	return nil
}

func (i QueueItem) Validate() error {
	if !safeID.MatchString(i.JobID) || !safeID.MatchString(i.ProjectID) || strings.TrimSpace(i.State) == "" ||
		i.Priority < 0 || i.Priority > 1000 || !safeID.MatchString(i.ProfileID) ||
		(i.ModelID != "" && !safeID.MatchString(i.ModelID)) || (i.RunnerID != "" && !safeID.MatchString(i.RunnerID)) ||
		len(i.Reason) > 4000 {
		return errors.New("scheduler queue item is invalid")
	}
	return nil
}

func (d Decision) Validate() error {
	if d.Mode != ModeQualityLatency && d.Mode != ModeThroughputBatching {
		return errors.New("scheduler decision mode is invalid")
	}
	if d.Status != DecisionScheduled && d.Status != DecisionDeferred && d.Status != DecisionRejected {
		return errors.New("scheduler decision status is invalid")
	}
	if d.Status == DecisionScheduled && (!safeID.MatchString(d.SelectedJobID) || !safeID.MatchString(d.SelectedProjectID) || !safeID.MatchString(d.SelectedProfileID)) {
		return errors.New("scheduler scheduled decision lacks selected job evidence")
	}
	if strings.TrimSpace(d.Reason) == "" || strings.TrimSpace(d.ResourceSummary) == "" || len(d.RejectedJobIDs) > 256 || len(d.DeferredJobIDs) > 256 {
		return errors.New("scheduler decision lacks bounded evidence")
	}
	for _, values := range [][]string{d.RejectedJobIDs, d.DeferredJobIDs} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("scheduler decision contains invalid job ID evidence")
			}
		}
	}
	if d.ModelBatchGroup != "" && !safeID.MatchString(d.ModelBatchGroup) {
		return errors.New("scheduler decision model batch group is invalid")
	}
	return nil
}

func sortQueue(items []QueueItem, mode string, now time.Time) {
	sort.SliceStable(items, func(i, j int) bool {
		if mode == ModeThroughputBatching && items[i].ModelID != "" && items[i].ModelID == items[j].ModelID {
			return items[i].Priority > items[j].Priority
		}
		iUrgent, jUrgent := deadlineScore(items[i], now), deadlineScore(items[j], now)
		if iUrgent != jUrgent {
			return iUrgent > jUrgent
		}
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}

func deadlineScore(item QueueItem, now time.Time) int {
	if item.DeadlineAt.IsZero() {
		return 0
	}
	until := item.DeadlineAt.Sub(now)
	switch {
	case until <= 0:
		return 4
	case until <= 15*time.Minute:
		return 3
	case until <= time.Hour:
		return 2
	default:
		return 1
	}
}

func resourceSummary(request SimulationRequest, selected *ResourceProfile) string {
	activeMemory := request.Topology.ReservedMemoryBytes
	activeCPU := 0
	for _, item := range request.Active {
		for _, profile := range request.Profiles {
			if profile.ID == item.ProfileID {
				activeMemory += profile.MemoryBytes
				activeCPU += profile.CPUCores
				break
			}
		}
	}
	selectedText := "no selected workload"
	if selected != nil {
		selectedText = fmt.Sprintf("selected %s uses %d CPU and %d bytes memory", selected.ID, selected.CPUCores, selected.MemoryBytes)
	}
	return fmt.Sprintf("host %s has %d logical CPU and %d bytes memory; active plus reserved uses %d CPU and %d bytes memory; %s",
		request.Topology.HostID, request.Topology.LogicalCores, request.Topology.TotalMemoryBytes, activeCPU, activeMemory, selectedText)
}
