package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/intelligence"
)

func (s *Server) requireIntelligence(w http.ResponseWriter) (*intelligence.Service, bool) {
	if s.intelligence == nil {
		writeError(w, http.StatusServiceUnavailable, "intelligence_unavailable", "code intelligence is not configured")
		return nil, false
	}
	return s.intelligence, true
}

func (s *Server) intelligenceStatus(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	status, err := service.Status(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type intelligenceQueryRequest struct {
	Revision string `json:"revision,omitempty"`
	Term     string `json:"term"`
	Limit    int    `json:"limit,omitempty"`
}

func (s *Server) queryIntelligence(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request intelligenceQueryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := service.Query(r.Context(), intelligence.Query{ProjectID: r.PathValue("projectID"), Revision: request.Revision, Term: request.Term, Limit: request.Limit})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) refreshIntelligence(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	if s.gitOperator == nil {
		writeError(w, http.StatusServiceUnavailable, "repository_source_unavailable", "the trusted Git snapshot source is unavailable")
		return
	}
	projectID := r.PathValue("projectID")
	project, err := s.store.GetProject(r.Context(), projectID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	synced, err := s.gitOperator.Sync(r.Context(), projectID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	snapshot, err := s.gitOperator.Snapshot(r.Context(), projectID, synced.BaseSHA)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	files := make([]intelligence.SourceFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		digest := sha256.Sum256(file.Content)
		files = append(files, intelligence.SourceFile{Path: file.Path, BlobSHA256: hex.EncodeToString(digest[:]), Content: file.Content})
	}
	run, err := service.Index(r.Context(), intelligence.IndexRequest{ProjectID: projectID, Repository: project.Repository, Revision: snapshot.Revision, ParserID: "controller-syntax-v1", Files: files})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "source_excluded": snapshot.Excluded})
}

func (s *Server) rebuildIntelligence(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a bounded audited rebuild reason is required")
		return
	}
	if s.auth != nil {
		principal, exists := principalFromRequest(r)
		if !exists || !principal.RecentlyReauthenticated(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", "index rebuild requires recent reauthentication")
			return
		}
	}
	if err := service.Rebuild(r.Context(), r.PathValue("projectID"), actorID(r), request.Reason); err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared", "next_action": "refresh", "reason": request.Reason})
}

func (s *Server) getContextManifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireIntelligence(w); !ok {
		return
	}
	manifest, err := s.store.GetContextManifest(r.Context(), r.PathValue("projectID"), r.PathValue("manifestID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *Server) listContextManifests(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireIntelligence(w); !ok {
		return
	}
	items, err := s.store.ListContextManifests(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listProjectBaselines(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireIntelligence(w); !ok {
		return
	}
	items, err := s.store.ListBaselines(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listProjectDifferentials(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireIntelligence(w); !ok {
		return
	}
	items, err := s.store.ListDifferentials(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listProjectTestImpacts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireIntelligence(w); !ok {
		return
	}
	items, err := s.store.ListTestImpacts(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listProjectCaches(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	items, err := service.CacheEntries(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type cachePurgeRequest struct {
	Kind   string `json:"kind,omitempty"`
	Reason string `json:"reason"`
}

func (s *Server) purgeProjectCaches(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request cachePurgeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a bounded audited purge reason is required")
		return
	}
	reauthenticated := s.auth == nil
	if principal, exists := principalFromRequest(r); exists {
		reauthenticated = principal.RecentlyReauthenticated(time.Now().UTC())
	}
	count, err := service.PurgeCache(r.Context(), r.PathValue("projectID"), request.Kind, actorID(r), request.Reason, reauthenticated)
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": count, "kind": request.Kind, "reason": request.Reason})
}
