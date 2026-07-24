package api

import (
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/evaluation"
)

func (s *Server) listEvaluationDatasets(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListEvaluationDatasets(r.Context(), r.URL.Query().Get("project_id"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createEvaluationDataset(w http.ResponseWriter, r *http.Request) {
	var request evaluation.CreateDatasetRequest
	if err := decodeJSONLimit(w, r, &request, 128*1024); err != nil {
		return
	}
	dataset, err := evaluation.NewService(s.store).CreateDataset(r.Context(), request, actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "evaluation_dataset_invalid", err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/evaluations/datasets/"+dataset.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"dataset": dataset})
}

func (s *Server) listEvaluationRuns(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListEvaluationRuns(r.Context(), r.URL.Query().Get("dataset_id"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) launchEvaluationRun(w http.ResponseWriter, r *http.Request) {
	var request evaluation.LaunchRunRequest
	if err := decodeJSONLimit(w, r, &request, 128*1024); err != nil {
		return
	}
	run, err := evaluation.NewService(s.store).LaunchRun(r.Context(), request, actorID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "evaluation_run_invalid", err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/evaluations/runs/"+run.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"run": run})
}
