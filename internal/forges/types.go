package forges

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type FeatureStatus string

const (
	Supported    FeatureStatus = "supported"
	Unsupported  FeatureStatus = "unsupported"
	OperatorOnly FeatureStatus = "operator_only"
)

type Capability struct {
	Feature string        `json:"feature"`
	Status  FeatureStatus `json:"status"`
	Reason  string        `json:"reason,omitempty"`
}
type Profile struct {
	ProjectID               string            `json:"project_id"`
	Provider                string            `json:"provider"`
	Endpoint                string            `json:"endpoint"`
	EndpointAllowlist       []string          `json:"endpoint_allowlist"`
	Repository              string            `json:"repository"`
	CredentialReference     string            `json:"credential_reference,omitempty"`
	CredentialStatus        string            `json:"credential_status"`
	WebhookStatus           string            `json:"webhook_status"`
	SyncDirection           string            `json:"sync_direction"`
	PollingMinutes          int               `json:"polling_minutes"`
	BranchConvention        string            `json:"branch_convention"`
	ChangeRequestConvention string            `json:"change_request_convention"`
	LabelMapping            map[string]string `json:"label_mapping"`
	CIArtifactPolicy        string            `json:"ci_artifact_policy"`
	ReleasePolicy           string            `json:"release_policy"`
	SubmodulesEnabled       bool              `json:"submodules_enabled"`
	Enabled                 bool              `json:"enabled"`
	Revision                int64             `json:"revision"`
	UpdatedAt               time.Time         `json:"updated_at"`
}
type SaveProfileRequest struct {
	Profile          Profile
	ExpectedRevision int64
	ActorID          string
	Reason           string
	Reauthenticated  bool
}
type Object struct {
	ProjectID        string          `json:"project_id"`
	Provider         string          `json:"provider"`
	Kind             string          `json:"kind"`
	ExternalID       string          `json:"external_id"`
	Title            string          `json:"title,omitempty"`
	State            string          `json:"state,omitempty"`
	Ref              string          `json:"ref,omitempty"`
	SHA              string          `json:"sha,omitempty"`
	URL              string          `json:"url,omitempty"`
	ParentID         string          `json:"parent_id,omitempty"`
	ProviderMetadata json.RawMessage `json:"provider_metadata"`
	UpdatedAt        *time.Time      `json:"updated_at,omitempty"`
}
type Probe struct {
	ProjectID        string       `json:"project_id"`
	Provider         string       `json:"provider"`
	Ready            bool         `json:"ready"`
	CredentialStatus string       `json:"credential_status"`
	WebhookStatus    string       `json:"webhook_status"`
	Capabilities     []Capability `json:"capabilities"`
	Problems         []string     `json:"problems"`
	CheckedAt        time.Time    `json:"checked_at"`
}
type SyncRequest struct {
	ProjectID      string `json:"project_id"`
	Cursor         string `json:"cursor,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}
type SyncPage struct {
	ProjectID          string       `json:"project_id"`
	Provider           string       `json:"provider"`
	Cursor             string       `json:"cursor,omitempty"`
	NextCursor         string       `json:"next_cursor,omitempty"`
	Objects            []Object     `json:"objects"`
	Capabilities       []Capability `json:"capabilities"`
	RateLimitRemaining int          `json:"rate_limit_remaining"`
	RetryAfterSeconds  int          `json:"retry_after_seconds"`
	Partial            bool         `json:"partial"`
	Unsupported        []string     `json:"unsupported"`
}
type SyncRun struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Provider       string    `json:"provider"`
	InputCursor    string    `json:"input_cursor,omitempty"`
	OutputCursor   string    `json:"output_cursor,omitempty"`
	IdempotencyKey string    `json:"idempotency_key"`
	State          string    `json:"state"`
	Objects        int       `json:"objects"`
	Partial        bool      `json:"partial"`
	Unsupported    []string  `json:"unsupported"`
	Replay         bool      `json:"replay"`
	CreatedAt      time.Time `json:"created_at"`
}
