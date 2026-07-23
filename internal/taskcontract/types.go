package taskcontract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = 1

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Criterion struct {
	ID                 string `json:"id"`
	Statement          string `json:"statement"`
	VerificationMethod string `json:"verification_method"`
}

type Question struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Answer     string `json:"answer,omitempty"`
	AnsweredBy string `json:"answered_by,omitempty"`
}

type Contract struct {
	JobID                 string      `json:"job_id"`
	SchemaVersion         int         `json:"schema_version"`
	Version               int64       `json:"version"`
	Status                string      `json:"status"`
	SourceKind            string      `json:"source_kind"`
	SourceRef             string      `json:"source_ref,omitempty"`
	RequestedBehavior     string      `json:"requested_behavior"`
	ExplicitNonGoals      []string    `json:"explicit_non_goals"`
	AffectedUsers         []string    `json:"affected_users"`
	AcceptanceCriteria    []Criterion `json:"acceptance_criteria"`
	Constraints           []string    `json:"constraints"`
	LikelyComponents      []string    `json:"likely_components"`
	LikelyRisks           []string    `json:"likely_risks"`
	RequiredEvidence      []string    `json:"required_evidence"`
	RequiredDocumentation []string    `json:"required_documentation"`
	Assumptions           []string    `json:"assumptions"`
	Questions             []Question  `json:"questions"`
	CompletionChecklist   []Criterion `json:"completion_checklist"`
	ContractSHA256        string      `json:"contract_sha256"`
	ApprovedBy            string      `json:"approved_by,omitempty"`
	ApprovedAt            *time.Time  `json:"approved_at,omitempty"`
	CreatedAt             time.Time   `json:"created_at"`
	UpdatedAt             time.Time   `json:"updated_at"`
}

type Event struct {
	ID        string          `json:"id"`
	JobID     string          `json:"job_id"`
	Version   int64           `json:"version"`
	Action    string          `json:"action"`
	ActorID   string          `json:"actor_id"`
	ActorRole string          `json:"actor_role"`
	Reason    string          `json:"reason"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type UpsertRequest struct {
	JobID                 string
	ExpectedVersion       int64
	ActorID               string
	ActorRole             string
	Reason                string
	SourceKind            string
	SourceRef             string
	RequestedBehavior     string
	ExplicitNonGoals      []string
	AffectedUsers         []string
	AcceptanceCriteria    []Criterion
	Constraints           []string
	LikelyComponents      []string
	LikelyRisks           []string
	RequiredEvidence      []string
	RequiredDocumentation []string
	Assumptions           []string
	Questions             []Question
	CompletionChecklist   []Criterion
}

type ApprovalRequest struct {
	JobID           string
	ExpectedVersion int64
	ActorID         string
	ActorRole       string
	Reason          string
}

type Store interface {
	EnsureTaskContract(context.Context, UpsertRequest) (Contract, error)
	GetTaskContract(context.Context, string) (Contract, error)
	ListTaskContractEvents(context.Context, string, int) ([]Event, error)
	ApproveTaskContract(context.Context, ApprovalRequest) (Contract, error)
}

func DraftFromTask(jobID, task string, issueNumber *int64) (UpsertRequest, error) {
	task = strings.TrimSpace(task)
	if jobID == "" || task == "" || len(task) > 128<<10 {
		return UpsertRequest{}, errors.New("job id and bounded task text are required")
	}
	sourceKind := "free_form"
	sourceRef := ""
	if issueNumber != nil {
		sourceKind = "issue"
		sourceRef = strconv.FormatInt(*issueNumber, 10)
	}
	return UpsertRequest{
		JobID: jobID, SourceKind: sourceKind, SourceRef: sourceRef,
		RequestedBehavior: task,
		ExplicitNonGoals:  []string{"Do not broaden scope beyond the submitted task without a human contract edit."},
		AffectedUsers:     []string{"registered project maintainers"},
		AcceptanceCriteria: []Criterion{
			{ID: "AC-REQUESTED-BEHAVIOR", Statement: "The requested behavior is implemented exactly as approved in the task contract.", VerificationMethod: "reviewed_diff"},
			{ID: "AC-REGRESSION-GATES", Statement: "Targeted, full, policy, and final verification gates required by risk policy pass for the exact result commit.", VerificationMethod: "controller_verification"},
		},
		Constraints:           []string{"Preserve existing Increment 1 safety and repository authority boundaries."},
		LikelyComponents:      []string{"to_be_confirmed_by_context"},
		LikelyRisks:           []string{"ambiguity_in_task_contract"},
		RequiredEvidence:      []string{"accepted_task_contract", "risk_assessment", "exact_commit_verification"},
		RequiredDocumentation: []string{"documentation_impact_assessment"},
		Assumptions:           []string{"Low-risk assumptions require project policy; otherwise the job waits for human approval."},
		Questions:             []Question{{ID: "Q-SCOPE-1", Question: "Does the submitted task need any explicit non-goal, protected path, or publication constraint before implementation?"}},
		CompletionChecklist: []Criterion{
			{ID: "DONE-CONTRACT", Statement: "Task contract was approved before implementation.", VerificationMethod: "controller_audit"},
			{ID: "DONE-RISK", Statement: "Risk routing decision and any waiver are retained as evidence.", VerificationMethod: "risk_assessment"},
		},
	}, nil
}

func Normalize(request UpsertRequest, current *Contract) (Contract, error) {
	contract := Contract{
		JobID: request.JobID, SchemaVersion: SchemaVersion, Status: "draft", SourceKind: strings.TrimSpace(request.SourceKind),
		SourceRef: strings.TrimSpace(request.SourceRef), RequestedBehavior: strings.TrimSpace(request.RequestedBehavior),
		ExplicitNonGoals:      cleanStrings(request.ExplicitNonGoals, 32, 4000),
		AffectedUsers:         cleanStrings(request.AffectedUsers, 32, 2000),
		AcceptanceCriteria:    cleanCriteria(request.AcceptanceCriteria, 64),
		Constraints:           cleanStrings(request.Constraints, 64, 4000),
		LikelyComponents:      cleanStrings(request.LikelyComponents, 64, 512),
		LikelyRisks:           cleanStrings(request.LikelyRisks, 64, 512),
		RequiredEvidence:      cleanStrings(request.RequiredEvidence, 64, 512),
		RequiredDocumentation: cleanStrings(request.RequiredDocumentation, 64, 512),
		Assumptions:           cleanStrings(request.Assumptions, 64, 4000),
		Questions:             cleanQuestions(request.Questions),
		CompletionChecklist:   cleanCriteria(request.CompletionChecklist, 64),
	}
	if current != nil {
		contract.Version = current.Version + 1
	} else {
		contract.Version = 1
	}
	if err := contract.Validate(); err != nil {
		return Contract{}, err
	}
	contract.ContractSHA256 = contract.Hash()
	return contract, nil
}

func (c Contract) Validate() error {
	if !safeID.MatchString(c.JobID) || c.SchemaVersion != SchemaVersion || c.Version < 1 ||
		(c.Status != "draft" && c.Status != "approved") || (c.SourceKind != "free_form" && c.SourceKind != "issue" && c.SourceKind != "forge_object") ||
		c.RequestedBehavior == "" || len(c.RequestedBehavior) > 128<<10 || len(c.AcceptanceCriteria) == 0 ||
		len(c.CompletionChecklist) == 0 {
		return errors.New("task contract violates version-one bounds")
	}
	for _, values := range [][]string{c.ExplicitNonGoals, c.AffectedUsers, c.Constraints, c.LikelyComponents, c.LikelyRisks, c.RequiredEvidence, c.RequiredDocumentation, c.Assumptions} {
		for _, value := range values {
			if value == "" || strings.ContainsRune(value, 0) {
				return errors.New("task contract contains unsafe text")
			}
		}
	}
	if err := validateCriteria(c.AcceptanceCriteria); err != nil {
		return err
	}
	if err := validateCriteria(c.CompletionChecklist); err != nil {
		return err
	}
	seenQuestions := map[string]bool{}
	for _, question := range c.Questions {
		if !safeID.MatchString(question.ID) || strings.TrimSpace(question.Question) == "" || len(question.Question) > 4000 || len(question.Answer) > 8000 {
			return errors.New("task contract question is invalid")
		}
		if seenQuestions[question.ID] {
			return errors.New("task contract question IDs must be unique")
		}
		seenQuestions[question.ID] = true
	}
	return nil
}

func (c Contract) Hash() string {
	type hashContract Contract
	copy := hashContract(c)
	copy.ContractSHA256 = ""
	copy.ApprovedBy = ""
	copy.ApprovedAt = nil
	copy.CreatedAt = time.Time{}
	copy.UpdatedAt = time.Time{}
	payload, _ := json.Marshal(copy)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func ToAcceptanceJSON(criteria []Criterion) (json.RawMessage, string, error) {
	if err := validateCriteria(criteria); err != nil {
		return nil, "", err
	}
	payload, err := json.Marshal(criteria)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return json.RawMessage(payload), hex.EncodeToString(digest[:]), nil
}

func validateCriteria(criteria []Criterion) error {
	if len(criteria) == 0 || len(criteria) > 64 {
		return errors.New("criteria count is out of bounds")
	}
	seen := map[string]bool{}
	for _, item := range criteria {
		if !safeID.MatchString(item.ID) || strings.TrimSpace(item.Statement) == "" || strings.TrimSpace(item.VerificationMethod) == "" ||
			len(item.Statement)+len(item.VerificationMethod) > 16000 {
			return errors.New("criterion is invalid")
		}
		if seen[item.ID] {
			return errors.New("criterion IDs must be unique")
		}
		seen[item.ID] = true
	}
	return nil
}

func cleanStrings(values []string, maxItems, maxBytes int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxBytes || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == maxItems {
			break
		}
	}
	sort.Strings(result)
	return result
}

func cleanCriteria(values []Criterion, maxItems int) []Criterion {
	result := make([]Criterion, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Statement = strings.TrimSpace(value.Statement)
		value.VerificationMethod = strings.TrimSpace(value.VerificationMethod)
		if value.ID == "" || value.Statement == "" || value.VerificationMethod == "" || seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		result = append(result, value)
		if len(result) == maxItems {
			break
		}
	}
	return result
}

func cleanQuestions(values []Question) []Question {
	result := make([]Question, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Question = strings.TrimSpace(value.Question)
		value.Answer = strings.TrimSpace(value.Answer)
		value.AnsweredBy = strings.TrimSpace(value.AnsweredBy)
		if value.ID == "" || value.Question == "" || seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		result = append(result, value)
		if len(result) == 64 {
			break
		}
	}
	return result
}
