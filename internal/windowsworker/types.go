package windowsworker

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

const (
	JobDotNet             = "dotnet_restore_build_test"
	JobPowerShell         = "powershell_pester"
	JobServiceLifecycle   = "windows_service_lifecycle"
	JobInstallerLifecycle = "inno_installer_lifecycle"
	JobHamiltonDiscovery  = "hamilton_discovery"
	JobVPNWorkflow        = "vpn_workflow"
	JobReleaseConsistency = "release_consistency"
	JobInstallerEvidence  = "installer_iq_evidence"
	JobEquipmentSimulator = "equipment_simulation"
	JobSigningRequest     = "signing_request"
)

var ApprovedJobTypes = []string{
	JobDotNet, JobPowerShell, JobServiceLifecycle, JobInstallerLifecycle,
	JobHamiltonDiscovery, JobVPNWorkflow, JobReleaseConsistency,
	JobInstallerEvidence, JobEquipmentSimulator, JobSigningRequest,
}

func DefaultSimulatorProfile() Profile {
	return Profile{
		ID: "windows-simulator", Name: "Windows worker simulator", Mode: "simulator",
		Endpoint: "simulator://windows-worker", EndpointAllowlist: []string{"simulator://windows-worker"},
		CredentialStatus: "not_required", Health: "simulated", Capacity: 2,
		VMTemplateID: "windows-2022-sim-v1",
		Toolchains: map[string]string{
			"dotnet": "8.0.302", "inno": "6.3.3", "pester": "5.6.1", "powershell": "7.4.4",
		},
		AllowedJobTypes:       append([]string(nil), ApprovedJobTypes...),
		TimeoutSeconds:        1800,
		SimulatorProfileIDs:   []string{"hamilton-sim-v1", "instrument-sim-v1"},
		ArtifactRetentionDays: 30,
		Enabled:               true,
	}
}

type ManualGates struct {
	PhysicalHardware bool `json:"physical_hardware"`
	CodeSigning      bool `json:"code_signing"`
	ProductionVPN    bool `json:"production_vpn"`
}

type Profile struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Mode                   string            `json:"mode"`
	Endpoint               string            `json:"endpoint"`
	EndpointAllowlist      []string          `json:"endpoint_allowlist"`
	CredentialReference    string            `json:"credential_reference,omitempty"`
	CredentialStatus       string            `json:"credential_status"`
	Health                 string            `json:"health"`
	Capacity               int               `json:"capacity"`
	VMTemplateID           string            `json:"vm_template_id"`
	Toolchains             map[string]string `json:"toolchains"`
	AllowedJobTypes        []string          `json:"allowed_job_types"`
	TimeoutSeconds         int               `json:"timeout_seconds"`
	SimulatorProfileIDs    []string          `json:"simulator_profile_ids"`
	ArtifactRetentionDays  int               `json:"artifact_retention_days"`
	SigningPolicyReference string            `json:"signing_policy_reference,omitempty"`
	ManualGates            ManualGates       `json:"manual_gates"`
	Enabled                bool              `json:"enabled"`
	Revision               int64             `json:"revision"`
	UpdatedAt              time.Time         `json:"updated_at"`
}

type SaveProfileRequest struct {
	Profile          Profile
	ExpectedRevision int64
	ActorID          string
	Reason           string
	Reauthenticated  bool
}

// ImmutableInput is a closed, non-executable worker input contract. Every
// field is interpreted by a controller-registered job type; there is no
// command, script, image, mount, path, network, environment, or argument list.
type ImmutableInput struct {
	RepositorySHA              string `json:"repository_sha"`
	CapabilityPackChecksum     string `json:"capability_pack_checksum"`
	ToolchainInventoryChecksum string `json:"toolchain_inventory_checksum"`
	SourceArtifactID           string `json:"source_artifact_id"`
	PreviousInstallerArtifact  string `json:"previous_installer_artifact_id,omitempty"`
	ReleaseVersion             string `json:"release_version,omitempty"`
	ExpectedServiceName        string `json:"expected_service_name,omitempty"`
	HamiltonProfileID          string `json:"hamilton_profile_id,omitempty"`
	SimulatorProfileID         string `json:"simulator_profile_id,omitempty"`
}

type RunRequest struct {
	ProfileID      string         `json:"profile_id"`
	ProjectID      string         `json:"project_id"`
	JobID          string         `json:"job_id"`
	JobType        string         `json:"job_type"`
	Input          ImmutableInput `json:"input"`
	IdempotencyKey string         `json:"idempotency_key"`
	OperatorGated  bool           `json:"operator_gated"`
	ActorID        string         `json:"-"`
	ActorRole      string         `json:"-"`
}

type Check struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Summary  string `json:"summary"`
	Duration int    `json:"duration_ms"`
}

type Artifact struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	MediaType string          `json:"media_type"`
	SHA256    string          `json:"sha256"`
	Bytes     int64           `json:"bytes"`
	Metadata  json.RawMessage `json:"metadata"`
}

type Result struct {
	RunID          string     `json:"run_id"`
	ProfileID      string     `json:"profile_id"`
	ProjectID      string     `json:"project_id"`
	JobID          string     `json:"job_id"`
	JobType        string     `json:"job_type"`
	InputSHA256    string     `json:"input_sha256"`
	State          string     `json:"state"`
	Checks         []Check    `json:"checks"`
	Artifacts      []Artifact `json:"artifacts"`
	IdempotencyKey string     `json:"idempotency_key"`
	Replay         bool       `json:"replay"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    time.Time  `json:"completed_at"`
}

type Probe struct {
	ProfileID    string            `json:"profile_id"`
	Ready        bool              `json:"ready"`
	Mode         string            `json:"mode"`
	Health       string            `json:"health"`
	Capacity     int               `json:"capacity"`
	VMTemplateID string            `json:"vm_template_id"`
	Toolchains   map[string]string `json:"toolchains"`
	Problems     []string          `json:"problems"`
	CheckedAt    time.Time         `json:"checked_at"`
}
