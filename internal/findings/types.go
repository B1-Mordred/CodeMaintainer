package findings

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusOpen        Status = "open"
	StatusFixed       Status = "fixed"
	StatusVerified    Status = "verified"
	StatusDisputed    Status = "disputed"
	StatusAccepted    Status = "accepted"
	StatusRejected    Status = "rejected"
	StatusHumanWaived Status = "human_waived"
	StatusClosed      Status = "closed"
)

type Record struct {
	JobID              string          `json:"job_id"`
	ID                 string          `json:"id"`
	Severity           string          `json:"severity"`
	Category           string          `json:"category"`
	Claim              string          `json:"claim"`
	Location           json.RawMessage `json:"location"`
	RequiredResolution string          `json:"required_resolution"`
	VerificationMethod string          `json:"verification_method"`
	Status             Status          `json:"status"`
	FirstSeenCycle     int             `json:"first_seen_cycle"`
	LastSeenCycle      int             `json:"last_seen_cycle"`
	Version            int64           `json:"version"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type TransitionRequest struct {
	To              Status
	ActorID         string
	ActorRole       string
	Rationale       string
	Reauthenticated bool
	ExpectedVersion int64
}

func CanTransition(from, to Status) bool {
	allowed := map[Status]map[Status]bool{
		StatusOpen:        {StatusFixed: true, StatusDisputed: true, StatusHumanWaived: true},
		StatusFixed:       {StatusVerified: true, StatusOpen: true},
		StatusVerified:    {StatusClosed: true, StatusOpen: true},
		StatusDisputed:    {StatusAccepted: true, StatusRejected: true},
		StatusAccepted:    {StatusClosed: true},
		StatusRejected:    {StatusOpen: true},
		StatusHumanWaived: {StatusClosed: true},
	}
	return allowed[from][to]
}
