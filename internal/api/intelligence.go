package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
)

type intelligenceProjectConfiguration struct {
	IndexingEnabled bool
	RetentionDays   int
	CacheQuotaBytes int64
}

func (s *Server) projectIntelligenceConfiguration(ctx context.Context, projectID string) (intelligenceProjectConfiguration, error) {
	result := intelligenceProjectConfiguration{IndexingEnabled: true, RetentionDays: 30, CacheQuotaBytes: 512 << 20}
	if s.configRegistry == nil {
		return result, nil
	}
	effective, err := s.configRegistry.Effective(ctx, []appconfig.ScopeRef{{Kind: appconfig.ScopeSystem}, {Kind: appconfig.ScopeProject, ID: projectID}})
	if err != nil {
		return result, err
	}
	if value, ok := effective.Values["intelligence.indexing_enabled"]; ok {
		if err := json.Unmarshal(value.Value, &result.IndexingEnabled); err != nil {
			return result, err
		}
	}
	if value, ok := effective.Values["intelligence.index_retention_days"]; ok {
		if err := json.Unmarshal(value.Value, &result.RetentionDays); err != nil {
			return result, err
		}
	}
	if value, ok := effective.Values["intelligence.cache_quota_bytes"]; ok {
		if err := json.Unmarshal(value.Value, &result.CacheQuotaBytes); err != nil {
			return result, err
		}
	}
	return result, nil
}

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
	configuration, err := s.projectIntelligenceConfiguration(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	status.IndexingEnabled = configuration.IndexingEnabled
	status.RetentionDays = configuration.RetentionDays
	status.CacheQuotaBytes = configuration.CacheQuotaBytes
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
	configuration, err := s.projectIntelligenceConfiguration(r.Context(), projectID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !configuration.IndexingEnabled {
		writeError(w, http.StatusConflict, "indexing_paused", "project indexing is paused by effective configuration")
		return
	}
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
	if len(files) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no_indexable_files", "the bounded trusted snapshot contained no indexable source files")
		return
	}
	run, err := service.Index(r.Context(), intelligence.IndexRequest{ProjectID: projectID, Repository: project.Repository, Revision: snapshot.Revision, ParserID: "controller-syntax-v1", CacheRetentionDays: configuration.RetentionDays, CacheQuotaBytes: configuration.CacheQuotaBytes, Files: files})
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

func (s *Server) compareContextManifests(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if intelligence.ValidateIdentity(request.LeftID) != nil || intelligence.ValidateIdentity(request.RightID) != nil || request.LeftID == request.RightID {
		writeError(w, http.StatusBadRequest, "invalid_manifest_comparison", "two distinct bounded manifest identities are required")
		return
	}
	result, err := service.CompareContextManifests(r.Context(), r.PathValue("projectID"), request.LeftID, request.RightID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func (s *Server) intelligenceReauthenticated(r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	principal, exists := principalFromRequest(r)
	return exists && principal.RecentlyReauthenticated(time.Now().UTC())
}

func (s *Server) listBaselineSupersessions(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	items, err := service.BaselineSupersessions(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) supersedeBaseline(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		DifferentialID string `json:"differential_id"`
		Reason         string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 || intelligence.ValidateIdentity(request.DifferentialID) != nil {
		writeError(w, http.StatusBadRequest, "invalid_baseline_update", "a bounded differential identity and audited reason are required")
		return
	}
	result, err := service.SupersedeBaseline(r.Context(), r.PathValue("projectID"), r.PathValue("baselineID"), request.DifferentialID, actorID(r), request.Reason, s.intelligenceReauthenticated(r))
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
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

func (s *Server) listDifferentialCorrections(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	items, err := service.DifferentialCorrections(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) correctDifferential(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		ObservationKind     string `json:"observation_kind"`
		ObservationKey      string `json:"observation_key"`
		AfterClassification string `json:"after_classification"`
		Reason              string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := service.CorrectDifferential(r.Context(), intelligence.DifferentialCorrection{ProjectID: r.PathValue("projectID"), DifferentialID: r.PathValue("differentialID"), ObservationKind: request.ObservationKind, ObservationKey: request.ObservationKey, AfterClassification: request.AfterClassification, ActorID: actorID(r), Reason: request.Reason}, s.intelligenceReauthenticated(r))
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_differential_correction", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
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

func (s *Server) listTestImpactOverrides(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	items, err := service.TestImpactOverrides(r.Context(), r.PathValue("projectID"), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) overrideTestImpact(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		TestID    string `json:"test_id"`
		Selected  bool   `json:"selected"`
		Reason    string `json:"reason"`
		ExpiresAt string `json:"expires_at,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	var expires *time.Time
	if request.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, request.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_override_expiry", "override expiry must be RFC 3339")
			return
		}
		expires = &parsed
	}
	result, err := service.OverrideTestImpact(r.Context(), intelligence.TestImpactOverride{ProjectID: r.PathValue("projectID"), ImpactID: r.PathValue("impactID"), TestID: request.TestID, Selected: request.Selected, ActorID: actorID(r), Reason: request.Reason, ExpiresAt: expires}, s.intelligenceReauthenticated(r))
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_test_impact_override", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
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

func (s *Server) verifyProjectCaches(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		Kind string `json:"kind,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Kind != "" && intelligence.ValidateIdentity(request.Kind) != nil {
		writeError(w, http.StatusBadRequest, "invalid_cache_kind", "cache kind must be a bounded identity")
		return
	}
	report, err := service.VerifyCaches(r.Context(), r.PathValue("projectID"), request.Kind)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) simulateProjectCache(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireIntelligence(w)
	if !ok {
		return
	}
	var request struct {
		TrustDomain    string `json:"trust_domain"`
		Kind           string `json:"kind"`
		EstimatedBytes int64  `json:"estimated_bytes"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if intelligence.ValidateIdentity(request.TrustDomain) != nil || intelligence.ValidateIdentity(request.Kind) != nil || request.EstimatedBytes < 0 {
		writeError(w, http.StatusBadRequest, "invalid_cache_simulation", "bounded trust domain, cache kind, and non-negative estimated bytes are required")
		return
	}
	configuration, err := s.projectIntelligenceConfiguration(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	result, err := service.SimulateCache(r.Context(), r.PathValue("projectID"), request.TrustDomain, request.Kind, request.EstimatedBytes, configuration.CacheQuotaBytes)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
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
