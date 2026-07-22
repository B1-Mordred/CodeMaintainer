package capabilities

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

// Manifest is controller-owned declarative metadata. It deliberately has no
// image, mount, network, host path, executable, environment, or free-form
// runner argument field.
type Manifest struct {
	SchemaVersion    int                   `json:"schema_version"`
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Version          string                `json:"version"`
	ChecksumSHA256   string                `json:"checksum_sha256"`
	Description      string                `json:"description"`
	Languages        []string              `json:"languages"`
	Compatibility    Compatibility         `json:"compatibility"`
	Prerequisites    []Prerequisite        `json:"prerequisites"`
	DetectionRules   []DetectionRule       `json:"detection_rules"`
	RunnerProfileIDs []string              `json:"runner_profile_ids"`
	OperationClasses []string              `json:"operation_classes"`
	ParserIDs        []string              `json:"parser_ids"`
	PolicyFragments  []string              `json:"policy_fragments"`
	ContextSelectors []string              `json:"context_selectors"`
	RiskRules        []string              `json:"risk_rules"`
	Documentation    []string              `json:"documentation_rules"`
	WorkflowChanges  []WorkflowChange      `json:"workflow_changes"`
	UISchema         []UIField             `json:"ui_schema"`
	Rehearsals       []RehearsalDefinition `json:"rehearsals"`
}

type Compatibility struct {
	ControllerConstraint string   `json:"controller_constraint"`
	Platforms            []string `json:"platforms"`
}

type Prerequisite struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
	Help     string `json:"help"`
}

type DetectionRule struct {
	ID          string   `json:"id"`
	AnyPaths    []string `json:"any_paths"`
	AllPaths    []string `json:"all_paths,omitempty"`
	Confidence  int      `json:"confidence"`
	Explanation string   `json:"explanation"`
}

type WorkflowChange struct {
	Stage       string `json:"stage"`
	OperationID string `json:"operation_id"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type UIField struct {
	Key       string          `json:"key"`
	Label     string          `json:"label"`
	Kind      string          `json:"kind"`
	Default   json.RawMessage `json:"default"`
	Allowed   []string        `json:"allowed,omitempty"`
	Minimum   *float64        `json:"minimum,omitempty"`
	Maximum   *float64        `json:"maximum,omitempty"`
	MaxLength int             `json:"max_length,omitempty"`
	Format    string          `json:"format,omitempty"`
	Help      string          `json:"help"`
	Advanced  bool            `json:"advanced,omitempty"`
}

// RehearsalDefinition is a reusable golden/rehearsal primitive. Artifact
// approval and policy gates are intentionally added by Milestone 5.
type RehearsalDefinition struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	OperationID     string   `json:"operation_id"`
	ArtifactKinds   []string `json:"artifact_kinds"`
	ComparisonClass string   `json:"comparison_class"`
	ApprovalPolicy  string   `json:"approval_policy"`
}

type TrustReport struct {
	PackID        string   `json:"pack_id"`
	Version       string   `json:"version"`
	Expected      string   `json:"expected_checksum_sha256"`
	Computed      string   `json:"computed_checksum_sha256"`
	ChecksumValid bool     `json:"checksum_valid"`
	AuthoritySafe bool     `json:"authority_safe"`
	Compatible    bool     `json:"compatible"`
	Issues        []string `json:"issues"`
}

type Installation struct {
	PackID      string    `json:"pack_id"`
	PackVersion string    `json:"pack_version"`
	Checksum    string    `json:"checksum_sha256"`
	State       string    `json:"state"`
	Pinned      bool      `json:"pinned"`
	Revision    int64     `json:"revision"`
	Previous    string    `json:"previous_version,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LifecycleEvent struct {
	ID          string    `json:"id"`
	PackID      string    `json:"pack_id"`
	FromVersion string    `json:"from_version,omitempty"`
	ToVersion   string    `json:"to_version,omitempty"`
	Action      string    `json:"action"`
	ActorID     string    `json:"actor_id"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
}

type Assignment struct {
	ProjectID   string          `json:"project_id"`
	PackID      string          `json:"pack_id"`
	PackVersion string          `json:"pack_version"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"`
	Revision    int64           `json:"revision"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type AssignmentConfigurationRequest struct {
	ProjectID        string
	PackID           string
	ExpectedRevision int64
	Config           json.RawMessage
	ActorID          string
	Reason           string
}

type Evidence struct {
	Path        string `json:"path"`
	Observation string `json:"observation"`
	SHA256      string `json:"sha256"`
}

type Finding struct {
	Category   string     `json:"category"`
	Value      string     `json:"value"`
	Confidence int        `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
}

type Proposal struct {
	ID         string          `json:"id"`
	ScanID     string          `json:"scan_id"`
	ProjectID  string          `json:"project_id"`
	Kind       string          `json:"kind"`
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
	Confidence int             `json:"confidence"`
	Evidence   []Evidence      `json:"evidence"`
	State      string          `json:"state"`
	Version    int64           `json:"version"`
	Reason     string          `json:"reason,omitempty"`
	ReviewedBy string          `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time      `json:"reviewed_at,omitempty"`
}

type Scan struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Repository    string     `json:"repository"`
	Revision      string     `json:"revision"`
	State         string     `json:"state"`
	Findings      []Finding  `json:"findings"`
	Proposals     []Proposal `json:"proposals"`
	Drift         []string   `json:"drift"`
	FilesObserved int        `json:"files_observed"`
	ExcludedFiles int        `json:"excluded_files"`
	CreatedAt     time.Time  `json:"created_at"`
}

type SourceFile struct {
	Path    string
	Content []byte
}

type ScanInput struct {
	ProjectID     string
	Repository    string
	Revision      string
	Files         []SourceFile
	ExcludedFiles int
}

type ReviewRequest struct {
	ProjectID       string
	ScanID          string
	ProposalID      string
	ExpectedVersion int64
	Accept          bool
	ActorID         string
	Reason          string
	Config          json.RawMessage
}
