package config

import (
	"encoding/json"
	"time"
)

type StoredValue struct {
	Key        string          `json:"key"`
	Scope      ScopeRef        `json:"scope"`
	Value      json.RawMessage `json:"value,omitempty"`
	Configured bool            `json:"configured"`
	Secret     bool            `json:"secret"`
	Version    int64           `json:"version"`
	RevisionID string          `json:"revision_id"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type ScopeState struct {
	Scope      ScopeRef      `json:"scope"`
	Version    int64         `json:"version"`
	RevisionID string        `json:"revision_id"`
	Values     []StoredValue `json:"values"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type RevisionEntry struct {
	Key              string          `json:"key"`
	BeforeValue      json.RawMessage `json:"before_value,omitempty"`
	AfterValue       json.RawMessage `json:"after_value,omitempty"`
	BeforeConfigured bool            `json:"before_configured"`
	AfterConfigured  bool            `json:"after_configured"`
	Secret           bool            `json:"secret"`
}

type RegistryRevision struct {
	ID           string          `json:"id"`
	Sequence     int64           `json:"sequence"`
	Scope        ScopeRef        `json:"scope"`
	ScopeVersion int64           `json:"scope_version"`
	ActorID      string          `json:"actor_id"`
	ActorRole    string          `json:"actor_role"`
	Operation    string          `json:"operation"`
	Reason       string          `json:"reason"`
	RollbackOf   string          `json:"rollback_of,omitempty"`
	DraftID      string          `json:"draft_id,omitempty"`
	Entries      []RevisionEntry `json:"entries"`
	CreatedAt    time.Time       `json:"created_at"`
}

type ScopeChange struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value,omitempty"`
	Configured bool            `json:"configured"`
	Secret     bool            `json:"secret"`
}

type ApplyScopeRequest struct {
	Scope           ScopeRef
	ExpectedVersion int64
	ActorID         string
	ActorRole       string
	Operation       string
	Reason          string
	RollbackOf      string
	DraftID         string
	DraftVersion    int64
	Changes         []ScopeChange
}

type DraftState string

const (
	DraftOpen      DraftState = "draft"
	DraftReviewed  DraftState = "reviewed"
	DraftApplied   DraftState = "applied"
	DraftDiscarded DraftState = "discarded"
)

type DraftEntry struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value,omitempty"`
	Reset      bool            `json:"reset"`
	Secret     bool            `json:"secret"`
	Configured bool            `json:"configured"`
}

type Draft struct {
	ID                string        `json:"id"`
	Scope             ScopeRef      `json:"scope"`
	Operation         string        `json:"operation"`
	State             DraftState    `json:"state"`
	BaseScopeVersion  int64         `json:"base_scope_version"`
	Version           int64         `json:"version"`
	AuthorID          string        `json:"author_id"`
	ReviewerID        string        `json:"reviewer_id,omitempty"`
	Reason            string        `json:"reason,omitempty"`
	AppliedRevisionID string        `json:"applied_revision_id,omitempty"`
	Entries           []DraftEntry  `json:"entries"`
	UnknownEntries    []ImportValue `json:"unknown_entries,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type CreateDraftRequest struct {
	Scope            ScopeRef
	Operation        string
	BaseScopeVersion int64
	AuthorID         string
	Reason           string
	Entries          []DraftEntry
	UnknownEntries   []ImportValue
}

type UpdateDraftRequest struct {
	ID              string
	ExpectedVersion int64
	ActorID         string
	Reason          string
	Entries         []DraftEntry
}

type TransitionDraftRequest struct {
	ID              string
	ExpectedVersion int64
	ActorID         string
	Target          DraftState
	Reason          string
}

type CheckResult struct {
	ID           string          `json:"id"`
	Sequence     int64           `json:"sequence"`
	DraftID      string          `json:"draft_id"`
	DraftVersion int64           `json:"draft_version"`
	Kind         string          `json:"kind"`
	Handler      string          `json:"handler"`
	Status       string          `json:"status"`
	Result       json.RawMessage `json:"result"`
	CreatedAt    time.Time       `json:"created_at"`
	ExpiresAt    *time.Time      `json:"expires_at,omitempty"`
}

type JobSnapshot struct {
	JobID         string          `json:"job_id"`
	SchemaVersion int             `json:"schema_version"`
	RegistryHash  string          `json:"registry_hash"`
	SHA256        string          `json:"sha256"`
	Document      json.RawMessage `json:"document"`
	CreatedAt     time.Time       `json:"created_at"`
}
