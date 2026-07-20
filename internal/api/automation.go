package api

import (
	"net/http"

	"github.com/local-code-maintainer/appliance/internal/automation"
)

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSchedules(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) saveSchedule(w http.ResponseWriter, r *http.Request) {
	var request automation.ScheduleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	schedule, err := s.store.SaveSchedule(r.Context(), request, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (s *Server) listScheduleRuns(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListScheduleRuns(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listSkillProposals(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSkillProposals(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) reviewSkillProposal(w http.ResponseWriter, r *http.Request) {
	var request automation.SkillReviewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.ActorID = actorID(r)
	proposal, err := s.store.ReviewSkillProposal(r.Context(), r.PathValue("proposalID"), request)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (s *Server) listAutomationRequests(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListApprovalRequests(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNotifications(r.Context(), r.URL.Query().Get("state"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) acknowledgeNotification(w http.ResponseWriter, r *http.Request) {
	notification, err := s.store.AcknowledgeNotification(r.Context(), r.PathValue("notificationID"), actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, notification)
}
