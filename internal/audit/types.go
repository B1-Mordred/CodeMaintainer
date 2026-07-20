package audit

import (
	"encoding/json"
	"time"
)

type Event struct {
	Sequence      int64           `json:"sequence"`
	ID            string          `json:"id"`
	ActorID       string          `json:"actor_id"`
	ActorRole     string          `json:"actor_role"`
	Action        string          `json:"action"`
	TargetType    string          `json:"target_type"`
	TargetID      string          `json:"target_id"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	RemoteAddress string          `json:"remote_address,omitempty"`
	Details       json.RawMessage `json:"details"`
	CreatedAt     time.Time       `json:"created_at"`
}

type AppendRequest struct {
	ActorID       string
	ActorRole     string
	Action        string
	TargetType    string
	TargetID      string
	CorrelationID string
	RemoteAddress string
	Details       json.RawMessage
}
