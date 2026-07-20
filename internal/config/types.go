package config

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type System struct {
	SchemaVersion int                `json:"schema_version"`
	Deployment    Deployment         `json:"deployment"`
	Workflow      WorkflowPolicy     `json:"workflow"`
	QC            QCPolicy           `json:"qc"`
	Protected     ProtectedPathRule  `json:"protected_paths"`
	Notifications NotificationPolicy `json:"notifications"`
}

type Deployment struct {
	ListenAddress string `json:"listen_address"`
	DataRoot      string `json:"data_root"`
	Profile       string `json:"profile"`
}

type WorkflowPolicy struct {
	MaxReviewCycles int   `json:"max_review_cycles"`
	MaxWallSeconds  int64 `json:"max_wall_seconds"`
	MaxLogBytes     int64 `json:"max_log_bytes"`
}

type QCPolicy struct {
	BlockOn                    []string `json:"block_on"`
	RequireEvidenceForBlocking bool     `json:"require_evidence_for_blocking"`
	RequireVerificationMethod  bool     `json:"require_verification_method"`
	HumanWaiverEnabled         bool     `json:"human_waiver_enabled"`
	WaiverRationaleRequired    bool     `json:"waiver_rationale_required"`
}

type ProtectedPathRule struct {
	Patterns []string `json:"patterns"`
}

type NotificationPolicy struct {
	LocalInboxEnabled bool `json:"local_inbox_enabled"`
}

type Revision struct {
	ID               string          `json:"id"`
	Sequence         int64           `json:"sequence"`
	ActorID          string          `json:"actor_id"`
	SchemaVersion    int             `json:"schema_version"`
	Before           json.RawMessage `json:"before"`
	After            json.RawMessage `json:"after"`
	Diff             json.RawMessage `json:"diff"`
	ValidationResult json.RawMessage `json:"validation_result"`
	RollbackOf       string          `json:"rollback_of,omitempty"`
	Reason           string          `json:"reason"`
	CreatedAt        time.Time       `json:"created_at"`
}

func Default(dataRoot string) System {
	return System{
		SchemaVersion: SchemaVersion,
		Deployment: Deployment{
			ListenAddress: "127.0.0.1:8080",
			DataRoot:      dataRoot,
			Profile:       "mock",
		},
		Workflow: WorkflowPolicy{
			MaxReviewCycles: 2,
			MaxWallSeconds:  14_400,
			MaxLogBytes:     8 << 20,
		},
		QC: QCPolicy{
			BlockOn:                    []string{"blocker", "must_fix"},
			RequireEvidenceForBlocking: true,
			RequireVerificationMethod:  true,
			HumanWaiverEnabled:         true,
			WaiverRationaleRequired:    true,
		},
		Protected: ProtectedPathRule{Patterns: []string{
			".github/workflows/**",
			"CODEOWNERS",
			".gitmodules",
		}},
		Notifications: NotificationPolicy{LocalInboxEnabled: true},
	}
}

func Validate(value System) []string {
	var errors []string
	if value.SchemaVersion != SchemaVersion {
		errors = append(errors, "unsupported schema_version")
	}
	if value.Deployment.ListenAddress == "" {
		errors = append(errors, "deployment.listen_address is required")
	}
	if value.Deployment.DataRoot == "" {
		errors = append(errors, "deployment.data_root is required")
	}
	if value.Workflow.MaxReviewCycles < 1 || value.Workflow.MaxReviewCycles > 10 {
		errors = append(errors, "workflow.max_review_cycles must be between 1 and 10")
	}
	if value.Workflow.MaxWallSeconds < 60 {
		errors = append(errors, "workflow.max_wall_seconds must be at least 60")
	}
	if value.Workflow.MaxLogBytes < 1024 || value.Workflow.MaxLogBytes > 1<<30 {
		errors = append(errors, "workflow.max_log_bytes must be between 1024 and 1073741824")
	}
	if len(value.QC.BlockOn) == 0 {
		errors = append(errors, "qc.block_on must not be empty")
	}
	return errors
}

// ValidateChange applies mutation rules that cannot be expressed by validating
// one document in isolation. Bootstrap-controlled deployment paths and listen
// addresses are intentionally immutable through the browser/API.
func ValidateChange(before, after System) []string {
	errors := Validate(after)
	if before.Deployment.ListenAddress != after.Deployment.ListenAddress {
		errors = append(errors, "deployment.listen_address is bootstrap-controlled and cannot be changed through the API")
	}
	if before.Deployment.DataRoot != after.Deployment.DataRoot {
		errors = append(errors, "deployment.data_root is bootstrap-controlled and cannot be changed through the API")
	}
	if before.Deployment.Profile != after.Deployment.Profile {
		errors = append(errors, "deployment.profile is bootstrap-controlled and cannot be changed through the API")
	}
	return errors
}
