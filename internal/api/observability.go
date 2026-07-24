package api

import (
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/observability"
)

func (s *Server) observabilityStatus(w http.ResponseWriter, r *http.Request) {
	status, err := observability.NewService(s.store).Status(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) listObservabilityEvents(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListObservabilityEvents(r.Context(), r.URL.Query().Get("component"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) recordObservabilityEvent(w http.ResponseWriter, r *http.Request) {
	var request observability.RecordEventRequest
	if err := decodeJSONLimit(w, r, &request, 128*1024); err != nil {
		return
	}
	event, err := observability.NewService(s.store).RecordEvent(r.Context(), request, actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "observability_event_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": event})
}

func (s *Server) listSupportBundles(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSupportBundles(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createSupportBundle(w http.ResponseWriter, r *http.Request) {
	var request observability.CreateSupportBundleRequest
	if err := decodeJSONLimit(w, r, &request, 128*1024); err != nil {
		return
	}
	bundle, err := observability.NewService(s.store).CreateSupportBundle(r.Context(), request, actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "support_bundle_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bundle": bundle})
}
