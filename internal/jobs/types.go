package jobs

import (
	"encoding/json"
	"time"
)

type Job struct {
	ID                     string          `json:"id"`
	ProjectID              string          `json:"project_id"`
	Repository             string          `json:"repository"`
	Task                   string          `json:"task"`
	IssueNumber            *int64          `json:"issue_number,omitempty"`
	State                  State           `json:"state"`
	BaseSHA                string          `json:"base_sha,omitempty"`
	ResultSHA              string          `json:"result_sha,omitempty"`
	AcceptanceCriteria     json.RawMessage `json:"acceptance_criteria"`
	AcceptanceCriteriaHash string          `json:"acceptance_criteria_hash,omitempty"`
	ReviewCycle            int             `json:"review_cycle"`
	MaxWallSeconds         int             `json:"max_wall_seconds"`
	DeadlineAt             time.Time       `json:"deadline_at"`
	MaxTokens              int             `json:"max_tokens"`
	ReservedTokens         int             `json:"reserved_tokens"`
	Version                int64           `json:"version"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type CreateRequest struct {
	ProjectID   string `json:"project_id"`
	Repository  string `json:"repository"`
	Task        string `json:"task"`
	IssueNumber *int64 `json:"issue_number,omitempty"`
}

type Transition struct {
	Sequence  int64           `json:"sequence"`
	JobID     string          `json:"job_id"`
	From      *State          `json:"from,omitempty"`
	To        State           `json:"to"`
	ActorID   string          `json:"actor_id"`
	Reason    string          `json:"reason"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type TransitionRequest struct {
	To              State
	ActorID         string
	Reason          string
	ExpectedVersion int64
	Details         json.RawMessage
}
