package api

import (
	"net/http"
	"time"

	resourcescheduler "github.com/B1-Mordred/CodeMaintainer/internal/scheduler"
)

func (s *Server) schedulerStatus(w http.ResponseWriter, r *http.Request) {
	decisions, err := s.store.ListSchedulerDecisions(r.Context(), queryInt(r, "limit", 20))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"topology":  resourcescheduler.DefaultTopology(),
		"profiles":  resourcescheduler.DefaultProfiles(),
		"modes":     []string{resourcescheduler.ModeQualityLatency, resourcescheduler.ModeThroughputBatching},
		"decisions": decisions,
	})
}

func (s *Server) simulateScheduler(w http.ResponseWriter, r *http.Request) {
	var request resourcescheduler.SimulationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Topology.HostID == "" {
		request.Topology = resourcescheduler.DefaultTopology()
	}
	if len(request.Profiles) == 0 {
		request.Profiles = resourcescheduler.DefaultProfiles()
	}
	decision, err := resourcescheduler.Simulate(request, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scheduler_simulation", err.Error())
		return
	}
	decision, err = s.store.RecordSchedulerDecision(r.Context(), decision)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"decision": decision})
}

func (s *Server) listSchedulerDecisions(w http.ResponseWriter, r *http.Request) {
	decisions, err := s.store.ListSchedulerDecisions(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": decisions})
}
