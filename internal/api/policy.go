package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/policy"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Server) listPolicyBundles(w http.ResponseWriter, r *http.Request) {
	items, err := policy.NewService(s.store).ListBundles(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundles": items})
}

func (s *Server) listPolicyActivations(w http.ResponseWriter, r *http.Request) {
	items, err := policy.NewService(s.store).ListActivations(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activations": items})
}

func (s *Server) activatePolicyBundle(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action               string `json:"action"`
		Reason               string `json:"reason"`
		StagedRolloutPercent int    `json:"staged_rollout_percent"`
	}
	if err := decodeJSONLimit(w, r, &request, 16*1024); err != nil {
		return
	}
	reauthenticated := s.auth == nil || recentlyReauthenticated(r)
	action := request.Action
	if strings.TrimSpace(action) == "" {
		action = "activate"
	}
	activation, err := policy.NewService(s.store).Activate(r.Context(), policy.ActivationRequest{
		BundleID: r.PathValue("bundleID"), Action: action,
		ActorID: actorID(r), ActorRole: actorRole(r), Reason: request.Reason,
		StagedRolloutPercent: request.StagedRolloutPercent, Reauthenticated: reauthenticated,
	})
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"activation": activation})
}

func (s *Server) simulatePolicy(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BundleID      string          `json:"bundle_id"`
		DecisionPoint string          `json:"decision_point"`
		Input         json.RawMessage `json:"input"`
	}
	if err := decodeJSONLimit(w, r, &request, 512*1024); err != nil {
		return
	}
	result, err := policy.NewService(s.store).Simulate(r.Context(), policy.SimulationRequest{
		BundleID: request.BundleID, DecisionPoint: request.DecisionPoint, Input: request.Input, ActorID: actorID(r),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "policy_simulation_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"simulation": result})
}

func (s *Server) listPolicySimulations(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPolicySimulations(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"simulations": items})
}

func (s *Server) listJobPolicyDecisions(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListPolicyDecisions(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"decisions": []policy.Decision{}})
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": items})
}
