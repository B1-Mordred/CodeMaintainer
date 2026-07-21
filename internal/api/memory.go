package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/memory"
)

const memoryRetrievalBudget = 4096

func (s *Server) projectMemoryScope(r *http.Request) (memory.ProjectScope, error) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if err != nil {
		return memory.ProjectScope{}, err
	}
	parts := strings.Split(project.Repository, "/")
	if !project.Enabled || len(parts) != 2 {
		return memory.ProjectScope{}, memory.ErrScope
	}
	scope := memory.ProjectScope{Owner: parts[0], Repository: parts[1]}
	if !scope.Valid() {
		return memory.ProjectScope{}, memory.ErrScope
	}
	return scope, nil
}

func (s *Server) listProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		items, err := s.store.ListMemory(r.Context(), scope, memory.Status(r.URL.Query().Get("status")), queryInt(r, "limit", 100))
		if err != nil {
			s.storageError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "namespace": scope.Namespace()})
		return
	}

	limit := queryInt(r, "limit", 100)
	var candidates []memory.Record
	trajectoryData := map[string]any{"filter": "project-before-ranking", "ranker": "bounded-lexical-v1"}
	if s.memoryIndex == nil {
		candidates, err = s.store.Search(r.Context(), scope, query, limit)
	} else {
		var matches []memory.IndexMatch
		matches, err = s.memoryIndex.Find(r.Context(), scope, query, limit)
		if err == nil {
			scores := make(map[string]float64, len(matches))
			for _, match := range matches {
				record, recordErr := s.store.GetMemory(r.Context(), scope, match.RecordID)
				if recordErr != nil || record.Status != memory.StatusCanonical || (record.ExpiresAt != nil && !record.ExpiresAt.After(time.Now().UTC())) {
					continue
				}
				candidates = append(candidates, record)
				scores[record.ID] = match.Score
			}
			trajectoryData = map[string]any{"filter": "project-before-ranking", "ranker": "openviking-v0.3.21", "scores": scores}
		}
	}
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	candidateIDs := make([]string, 0, len(candidates))
	selectedIDs := make([]string, 0, len(candidates))
	selected := make([]memory.Record, 0, len(candidates))
	allocated := 0
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.ID)
		tokens := (len(candidate.Content) + 3) / 4
		if tokens == 0 || allocated+tokens > memoryRetrievalBudget {
			continue
		}
		selected = append(selected, candidate)
		selectedIDs = append(selectedIDs, candidate.ID)
		allocated += tokens
	}
	trajectory, _ := json.Marshal(trajectoryData)
	trace, err := s.store.RecordRetrieval(r.Context(), memory.RetrievalTrace{
		Scope: scope, Query: query, CandidateIDs: candidateIDs, SelectedIDs: selectedIDs,
		BudgetTokens: memoryRetrievalBudget, AllocatedTokens: allocated, Trajectory: trajectory,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": selected, "namespace": scope.Namespace(), "retrieval": trace})
}

func (s *Server) smokeProjectMemoryIndex(w http.ResponseWriter, r *http.Request) {
	if s.memoryIndex == nil {
		writeError(w, http.StatusServiceUnavailable, "memory_index_disabled", "the replaceable memory index is disabled")
		return
	}
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := memory.SmokeIndex(ctx, s.memoryIndex, scope)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) reindexProjectMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryIndex == nil {
		writeError(w, http.StatusServiceUnavailable, "memory_index_unavailable", "the optional memory index is not configured")
		return
	}
	var request struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Mode != "vectors_only" && request.Mode != "semantic_and_vectors" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_reindex_mode", "mode must be vectors_only or semantic_and_vectors")
		return
	}
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	queued, err := s.store.EnqueueProjectMemoryRebuild(r.Context(), scope, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if err := s.memoryIndex.Reindex(r.Context(), scope, request.Mode); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "records_queued": queued, "namespace": scope.Namespace(), "mode": request.Mode})
}

func (s *Server) createProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	var request struct {
		Content          string     `json:"content"`
		Kind             string     `json:"kind"`
		SourceURI        string     `json:"source_uri,omitempty"`
		BaseCommit       string     `json:"base_commit,omitempty"`
		MergedCommit     string     `json:"merged_commit,omitempty"`
		AffectedPaths    []string   `json:"affected_paths,omitempty"`
		InvalidationRule string     `json:"invalidation_rule,omitempty"`
		ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	record, err := s.store.PutCandidate(r.Context(), scope, memory.Record{
		Content: request.Content, Kind: request.Kind, SourceURI: request.SourceURI,
		BaseCommit: request.BaseCommit, MergedCommit: request.MergedCommit,
		AffectedPaths: request.AffectedPaths, InvalidationRule: request.InvalidationRule, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("Location", r.URL.Path+"/"+record.ID)
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) getProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	record, err := s.store.GetMemory(r.Context(), scope, r.PathValue("memoryID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) listProjectMemoryEvents(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListMemoryEvents(r.Context(), scope, r.PathValue("memoryID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listProjectMemoryRetrievals(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListRetrievals(r.Context(), scope, queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) exportProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	bundle, err := s.store.ExportProjectMemory(r.Context(), scope)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="project-memory.json"`)
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) restoreProjectMemory(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DryRun bool                `json:"dry_run"`
		Bundle memory.ExportBundle `json:"bundle"`
	}
	if err := decodeJSONLimit(w, r, &request, 8<<20); err != nil {
		return
	}
	if !request.DryRun {
		if principal, ok := principalFromRequest(r); ok && !principal.RecentlyReauthenticated(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", "memory restore requires recent reauthentication")
			return
		}
	}
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	report, err := s.store.RestoreProjectMemory(r.Context(), scope, request.Bundle, request.DryRun, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	status := http.StatusCreated
	if request.DryRun {
		status = http.StatusOK
	}
	writeJSON(w, status, report)
}

func (s *Server) promoteProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	var request struct {
		Rationale       string `json:"rationale"`
		Basis           string `json:"basis"`
		MergedCommit    string `json:"merged_commit,omitempty"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	record, err := s.store.PromoteMemory(r.Context(), scope, r.PathValue("memoryID"), memory.PromotionRequest{
		ActorID: actorID(r), Rationale: request.Rationale, Basis: request.Basis,
		MergedCommit: request.MergedCommit, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) correctProjectMemory(w http.ResponseWriter, r *http.Request) {
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	var request struct {
		Content          string   `json:"content"`
		AffectedPaths    []string `json:"affected_paths,omitempty"`
		InvalidationRule string   `json:"invalidation_rule,omitempty"`
		Rationale        string   `json:"rationale"`
		ExpectedVersion  int64    `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	record, err := s.store.CorrectMemory(r.Context(), scope, r.PathValue("memoryID"), memory.CorrectionRequest{
		Content: request.Content, AffectedPaths: request.AffectedPaths, InvalidationRule: request.InvalidationRule,
		ActorID: actorID(r), Rationale: request.Rationale, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) invalidateProjectMemory(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Rationale       string `json:"rationale"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.Rationale) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_memory_action", "rationale is required")
		return
	}
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	record, err := s.store.InvalidateMemory(r.Context(), scope, r.PathValue("memoryID"), memory.InvalidationRequest{
		ActorID: actorID(r), Rationale: request.Rationale, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) deleteProjectMemory(w http.ResponseWriter, r *http.Request) {
	if principal, ok := principalFromRequest(r); ok && !principal.RecentlyReauthenticated(time.Now().UTC()) {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "memory deletion requires recent reauthentication")
		return
	}
	var request struct {
		Rationale       string `json:"rationale"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.Rationale) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_memory_action", "rationale is required")
		return
	}
	scope, err := s.projectMemoryScope(r)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if err := s.store.DeleteMemory(r.Context(), scope, r.PathValue("memoryID"), memory.DeletionRequest{
		ActorID: actorID(r), Rationale: request.Rationale, ExpectedVersion: request.ExpectedVersion,
	}); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
