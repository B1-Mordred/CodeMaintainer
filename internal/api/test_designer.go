package api

import (
	"encoding/json"
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

func (s *Server) listJobTestDesignerReports(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListTestDesignerReports(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	dispositions, err := s.store.ListTestDesignerDispositions(r.Context(), jobID, "", queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": items, "dispositions": dispositions})
}

func (s *Server) disposeTestDesignerProposal(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Disposition string `json:"disposition"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_test_designer_disposition", "disposition request must be valid JSON")
		return
	}
	role := actorRole(r)
	if s.auth == nil && role == "operator" {
		role = "reviewer"
	}
	record, err := s.store.SaveTestDesignerDisposition(r.Context(), testdesigner.Disposition{
		ReportID: r.PathValue("reportID"), ProposalID: r.PathValue("proposalID"),
		Disposition: request.Disposition, Reason: request.Reason, ActorID: actorID(r), ActorRole: role,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	reports, err := s.store.ListTestDesignerReports(r.Context(), record.JobID, 100)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	pending := testdesigner.PendingDispositionCount(reports, nil)
	var job any
	if pending == 0 {
		current, currentErr := s.store.GetJob(r.Context(), record.JobID)
		if currentErr != nil {
			s.storageError(w, r, currentErr)
			return
		}
		if current.State == jobs.StateAwaitingTestDesignDisposition {
			advanced, transitionErr := s.store.TransitionJob(r.Context(), record.JobID, jobs.TransitionRequest{
				To: jobs.StateGoldenRehearsalReview, ActorID: actorID(r),
				Reason:          "all required Test Designer proposals have explicit dispositions",
				ExpectedVersion: current.Version,
				Details:         json.RawMessage(`{"source":"test_designer_disposition"}`),
			})
			if transitionErr != nil {
				s.storageError(w, r, transitionErr)
				return
			}
			job = advanced
		} else {
			job = current
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"disposition": record, "pending_dispositions": pending, "job": job})
}
