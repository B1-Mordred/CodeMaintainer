package api

import (
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
)

func (s *Server) listAgentContracts(w http.ResponseWriter, r *http.Request) {
	if err := agents.ValidateRegistry(); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contracts": agents.BuiltInContracts()})
}

func (s *Server) listJobAgentContractValidations(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListAgentContractValidations(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"validations": items})
}
