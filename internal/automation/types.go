package automation

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid       = errors.New("invalid automation record")
	ErrNotFound      = errors.New("automation record not found")
	ErrConflict      = errors.New("automation record version conflict")
	ErrNoDueSchedule = errors.New("no schedule is due")
	identifier       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type Schedule struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Name              string    `json:"name"`
	TaskType          string    `json:"task_type"`
	Task              string    `json:"task"`
	IntervalSeconds   int       `json:"interval_seconds"`
	WindowStartMinute int       `json:"window_start_minute"`
	WindowEndMinute   int       `json:"window_end_minute"`
	MaxWallSeconds    int       `json:"max_wall_seconds"`
	MaxTokens         int       `json:"max_tokens"`
	Enabled           bool      `json:"enabled"`
	NextRunAt         time.Time `json:"next_run_at"`
	Version           int64     `json:"version"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ScheduleRequest struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Name              string    `json:"name"`
	TaskType          string    `json:"task_type"`
	Task              string    `json:"task"`
	IntervalSeconds   int       `json:"interval_seconds"`
	WindowStartMinute int       `json:"window_start_minute"`
	WindowEndMinute   int       `json:"window_end_minute"`
	MaxWallSeconds    int       `json:"max_wall_seconds"`
	MaxTokens         int       `json:"max_tokens"`
	Enabled           bool      `json:"enabled"`
	NextRunAt         time.Time `json:"next_run_at"`
	ExpectedVersion   int64     `json:"expected_version,omitempty"`
}

func (r ScheduleRequest) Validate() error {
	if (r.ID != "" && !identifier.MatchString(r.ID)) || !identifier.MatchString(r.ProjectID) || strings.TrimSpace(r.Name) == "" || len(r.Name) > 128 || strings.TrimSpace(r.Task) == "" || len(r.Task) > 64<<10 {
		return ErrInvalid
	}
	if r.TaskType != "maintenance" && r.TaskType != "sync" && r.TaskType != "audit" {
		return ErrInvalid
	}
	if r.IntervalSeconds < 60 || r.IntervalSeconds > 31*24*60*60 || r.WindowStartMinute < 0 || r.WindowStartMinute > 1439 || r.WindowEndMinute < 0 || r.WindowEndMinute > 1439 || r.MaxWallSeconds < 60 || r.MaxWallSeconds > 7*24*60*60 || r.MaxTokens < 1 || r.MaxTokens > 10_000_000 {
		return ErrInvalid
	}
	return nil
}

type ScheduleRun struct {
	Sequence   int64     `json:"sequence"`
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	JobID      string    `json:"job_id,omitempty"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	DueAt      time.Time `json:"due_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type SkillProposal struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Content      string    `json:"content"`
	ContentHash  string    `json:"content_hash"`
	Status       string    `json:"status"`
	Version      int64     `json:"version"`
	ProposedBy   string    `json:"proposed_by"`
	ReviewedBy   string    `json:"reviewed_by,omitempty"`
	ReviewReason string    `json:"review_reason,omitempty"`
	Activated    bool      `json:"activated"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SkillProposalRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	ProposedBy  string `json:"-"`
}

func (r SkillProposalRequest) Validate() error {
	if !identifier.MatchString(r.Name) || strings.TrimSpace(r.Description) == "" || len(r.Description) > 4096 || strings.TrimSpace(r.Content) == "" || len(r.Content) > 64<<10 || strings.TrimSpace(r.ProposedBy) == "" {
		return ErrInvalid
	}
	return nil
}

type SkillReviewRequest struct {
	Decision        string `json:"decision"`
	Rationale       string `json:"rationale"`
	ActorID         string `json:"-"`
	ExpectedVersion int64  `json:"expected_version"`
}

type ApprovalRequest struct {
	ID          string    `json:"id"`
	JobID       string    `json:"job_id"`
	Kind        string    `json:"kind"`
	RequestedBy string    `json:"requested_by"`
	Rationale   string    `json:"rationale"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store interface {
	SaveSchedule(context.Context, ScheduleRequest, string) (Schedule, error)
	GetSchedule(context.Context, string) (Schedule, error)
	ListSchedules(context.Context, int) ([]Schedule, error)
	DispatchDueSchedule(context.Context, string) (ScheduleRun, error)
	ListScheduleRuns(context.Context, int) ([]ScheduleRun, error)
	CreateSkillProposal(context.Context, SkillProposalRequest) (SkillProposal, error)
	ReviewSkillProposal(context.Context, string, SkillReviewRequest) (SkillProposal, error)
	ListSkillProposals(context.Context, int) ([]SkillProposal, error)
	CreateApprovalRequest(context.Context, string, string, string, string) (ApprovalRequest, error)
	ListApprovalRequests(context.Context, int) ([]ApprovalRequest, error)
}
