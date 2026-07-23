package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/risk"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
)

type taskContractUpdateRequest struct {
	ExpectedVersion       int64                    `json:"expected_version"`
	Reason                string                   `json:"reason"`
	SourceKind            string                   `json:"source_kind"`
	SourceRef             string                   `json:"source_ref,omitempty"`
	RequestedBehavior     string                   `json:"requested_behavior"`
	ExplicitNonGoals      []string                 `json:"explicit_non_goals"`
	AffectedUsers         []string                 `json:"affected_users"`
	AcceptanceCriteria    []taskcontract.Criterion `json:"acceptance_criteria"`
	Constraints           []string                 `json:"constraints"`
	LikelyComponents      []string                 `json:"likely_components"`
	LikelyRisks           []string                 `json:"likely_risks"`
	RequiredEvidence      []string                 `json:"required_evidence"`
	RequiredDocumentation []string                 `json:"required_documentation"`
	Assumptions           []string                 `json:"assumptions"`
	Questions             []taskcontract.Question  `json:"questions"`
	CompletionChecklist   []taskcontract.Criterion `json:"completion_checklist"`
}

type approveTaskContractRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type riskWaiverRequest struct {
	AssessmentID string     `json:"assessment_id"`
	ToLevel      risk.Level `json:"to_level"`
	Reason       string     `json:"reason"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

func (s *Server) getTaskContract(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	contract, err := s.store.GetTaskContract(r.Context(), jobID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	events, err := s.store.ListTaskContractEvents(r.Context(), jobID, 100)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract": contract, "events": events})
}

func (s *Server) updateTaskContract(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	var request taskContractUpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	_, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	contract, err := s.store.EnsureTaskContract(r.Context(), taskcontract.UpsertRequest{
		JobID: jobID, ExpectedVersion: request.ExpectedVersion, ActorID: actorID(r), ActorRole: actorRole(r),
		Reason: strings.TrimSpace(request.Reason), SourceKind: request.SourceKind, SourceRef: request.SourceRef,
		RequestedBehavior: request.RequestedBehavior, ExplicitNonGoals: request.ExplicitNonGoals,
		AffectedUsers: request.AffectedUsers, AcceptanceCriteria: request.AcceptanceCriteria,
		Constraints: request.Constraints, LikelyComponents: request.LikelyComponents, LikelyRisks: request.LikelyRisks,
		RequiredEvidence: request.RequiredEvidence, RequiredDocumentation: request.RequiredDocumentation,
		Assumptions: request.Assumptions, Questions: request.Questions, CompletionChecklist: request.CompletionChecklist,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	assessment, err := risk.Assess(contract, nil, "")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_risk_input", err.Error())
		return
	}
	assessment, err = s.store.SaveRiskAssessment(r.Context(), assessment)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract": contract, "risk_assessment": assessment})
}

func (s *Server) approveTaskContract(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	var request approveTaskContractRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	contract, err := s.store.ApproveTaskContract(r.Context(), taskcontract.ApprovalRequest{
		JobID: jobID, ExpectedVersion: request.ExpectedVersion, ActorID: actorID(r), ActorRole: actorRole(r),
		Reason: strings.TrimSpace(request.Reason),
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	assessment, riskErr := s.store.GetLatestRiskAssessment(r.Context(), jobID)
	response := map[string]any{"contract": contract}
	if riskErr == nil {
		response["risk_assessment"] = assessment
	} else if errors.Is(riskErr, storage.ErrNotFound) {
		assessment, assessErr := risk.Assess(contract, nil, "")
		if assessErr == nil {
			assessment, assessErr = s.store.SaveRiskAssessment(r.Context(), assessment)
		}
		if assessErr == nil {
			response["risk_assessment"] = assessment
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getJobRisk(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	assessments, err := s.store.ListRiskAssessments(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	waivers, err := s.store.ListRiskWaivers(r.Context(), jobID, 100)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assessments": assessments, "waivers": waivers})
}

func (s *Server) createRiskWaiver(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	var request riskWaiverRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	reauthenticated := s.auth == nil || recentlyReauthenticated(r)
	if !reauthenticated {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "risk lowering requires reauthentication within five minutes")
		return
	}
	waiver, assessment, err := s.store.CreateRiskWaiver(r.Context(), risk.WaiverRequest{
		JobID: jobID, AssessmentID: request.AssessmentID, ToLevel: request.ToLevel, Reason: strings.TrimSpace(request.Reason),
		ActorID: actorID(r), ActorRole: actorRole(r), Reauthenticated: reauthenticated, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"waiver_id": waiver.ID})
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "risk.waiver.api", TargetType: "job", TargetID: jobID, Details: payload})
	writeJSON(w, http.StatusCreated, map[string]any{"waiver": waiver, "risk_assessment": assessment})
}
