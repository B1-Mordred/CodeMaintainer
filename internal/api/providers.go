package api

import (
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
)

func (s *Server) providerStatus(w http.ResponseWriter, r *http.Request) {
	status, err := providers.NewService(s.store).Status(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) simulateProviderRoute(w http.ResponseWriter, r *http.Request) {
	var request providers.RouteRequest
	if err := decodeJSONLimit(w, r, &request, 128*1024); err != nil {
		return
	}
	decision, err := providers.NewService(s.store).SimulateRoute(r.Context(), request, actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "provider_route_invalid", err.Error())
		return
	}
	status := http.StatusCreated
	if decision.Status == providers.DecisionDenied {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"decision": decision})
}

func (s *Server) listProviderEgressManifests(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListEgressManifests(r.Context(), r.URL.Query().Get("project_id"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifests": items})
}

func (s *Server) probeProviderModel(w http.ResponseWriter, r *http.Request) {
	probe, err := providers.NewService(s.store).ProbeModel(r.Context(), r.PathValue("modelID"), actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "provider_probe_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"probe": probe})
}

func (s *Server) listProviderCapabilityProbes(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListCapabilityProbes(r.Context(), r.URL.Query().Get("model_profile_id"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"probes": items})
}
