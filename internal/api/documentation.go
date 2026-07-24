package api

import (
	"net/http"

	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
)

func (s *Server) listJobDocumentationManifests(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListDocumentationManifests(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifests": items})
}

func (s *Server) getDocumentationPolicyProfile(w http.ResponseWriter, r *http.Request) {
	profile := documentation.BuiltinPolicyProfile()
	if err := profile.Validate(); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

func (s *Server) simulateDocumentationPolicy(w http.ResponseWriter, r *http.Request) {
	var request documentation.PolicySimulationInput
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := documentation.SimulatePolicy(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_documentation_policy_simulation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"simulation": result})
}
