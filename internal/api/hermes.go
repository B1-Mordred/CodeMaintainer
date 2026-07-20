package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Server) hermesSubmitJob(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProjectID   string `json:"project_id"`
		Task        string `json:"task"`
		IssueNumber *int64 `json:"issue_number,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	project, err := s.store.GetProject(r.Context(), request.ProjectID)
	if err != nil || !project.Enabled || strings.TrimSpace(request.Task) == "" {
		writeError(w, http.StatusUnprocessableEntity, "project_not_registered", "Hermes may submit only to an enabled registered project")
		return
	}
	details, _ := json.Marshal(map[string]string{"source": "hermes"})
	job, err := s.store.CreateJob(r.Context(), storage.CreateJobParams{
		ProjectID: project.ID, Repository: project.Repository, Task: strings.TrimSpace(request.Task),
		IssueNumber: request.IssueNumber, ActorID: actorID(r), Details: details,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) hermesListJobs(w http.ResponseWriter, r *http.Request) {
	s.listJobs(w, r)
}

func (s *Server) hermesJobStatus(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) hermesCancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	job, err = s.store.TransitionJob(r.Context(), job.ID, jobs.TransitionRequest{
		To: jobs.StateCancelled, ActorID: actorID(r), Reason: "Hermes requested cancellation",
		ExpectedVersion: job.Version, Details: json.RawMessage(`{"source":"hermes"}`),
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) hermesJobReport(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	phases, err := s.store.ListPhaseRecords(r.Context(), job.ID, 100)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	findings, err := s.store.ListFindings(r.Context(), job.ID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	artifacts, err := s.store.ListJobArtifacts(r.Context(), job.ID, 200)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "phases": phases, "findings": findings, "artifacts": artifacts})
}

func (s *Server) hermesRequestReview(w http.ResponseWriter, r *http.Request) {
	s.hermesCreateApprovalRequest(w, r, "review")
}

func (s *Server) hermesRequestPublicationApproval(w http.ResponseWriter, r *http.Request) {
	s.hermesCreateApprovalRequest(w, r, "publication_approval")
}

func (s *Server) hermesCreateApprovalRequest(w http.ResponseWriter, r *http.Request, kind string) {
	var request struct {
		Rationale string `json:"rationale"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	created, err := s.store.CreateApprovalRequest(r.Context(), r.PathValue("jobID"), kind, actorID(r), request.Rationale)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (s *Server) hermesProjectMemory(w http.ResponseWriter, r *http.Request) {
	s.listProjectMemory(w, r)
}

func (s *Server) hermesListSchedules(w http.ResponseWriter, r *http.Request) {
	s.listSchedules(w, r)
}

func (s *Server) hermesCreateSkillProposal(w http.ResponseWriter, r *http.Request) {
	var request automation.SkillProposalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.ProposedBy = actorID(r)
	proposal, err := s.store.CreateSkillProposal(r.Context(), request)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, proposal)
}
