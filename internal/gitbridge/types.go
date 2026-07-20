package gitbridge

import (
	"errors"
	"regexp"
)

var (
	safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	commit = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

var (
	ErrInvalid       = errors.New("invalid Git bridge request")
	ErrNotFound      = errors.New("Git bridge resource not found")
	ErrConflict      = errors.New("Git bridge conflict")
	ErrUpstreamMoved = errors.New("upstream branch moved")
)

type Registration struct {
	ProjectID       string `json:"project_id"`
	Provider        string `json:"provider"`
	Repository      string `json:"repository"`
	DefaultBranch   string `json:"default_branch"`
	LocalRemoteName string `json:"local_remote_name,omitempty"`
}

type SyncResult struct {
	ProjectID string `json:"project_id"`
	BaseSHA   string `json:"base_sha"`
}

type WorktreeRequest struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
}

type WorktreeResult struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
	Branch    string `json:"branch"`
}

type CommitRequest struct {
	ProjectID    string `json:"project_id"`
	JobID        string `json:"job_id"`
	ExpectedHead string `json:"expected_head"`
	OperationID  string `json:"operation_id"`
}

type CommitResult struct {
	ResultSHA string `json:"result_sha"`
}

type DiffRequest struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
	ResultSHA string `json:"result_sha"`
}

type DiffResult struct {
	Patch string `json:"patch"`
}

type PublishRequest struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
	ResultSHA string `json:"result_sha"`
}

type Publication struct {
	Provider   string `json:"provider"`
	Branch     string `json:"branch"`
	ResultSHA  string `json:"result_sha"`
	Draft      bool   `json:"draft"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
	Number     int    `json:"number,omitempty"`
}
