package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
)

func (s *Server) requireConfigRegistry(w http.ResponseWriter) (*appconfig.RegistryService, bool) {
	if s.configRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "configuration_registry_unavailable", "the configuration registry is not initialized")
		return nil, false
	}
	return s.configRegistry, true
}

func (s *Server) configDescriptors(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	advanced := r.URL.Query().Get("advanced") != "false"
	items := make([]appconfig.Descriptor, 0)
	for _, descriptor := range service.Descriptors() {
		if !advanced && descriptor.UI.Advanced {
			continue
		}
		searchable := strings.ToLower(descriptor.Key + " " + descriptor.Namespace + " " + descriptor.UI.Label + " " + descriptor.UI.Help + " " + descriptor.UI.Group)
		if query != "" && !strings.Contains(searchable, query) {
			continue
		}
		items = append(items, descriptor)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": appconfig.RegistrySchemaVersion, "items": items})
}

func configScopeFromRequest(r *http.Request) (appconfig.ScopeRef, error) {
	scope := appconfig.ScopeRef{Kind: appconfig.ScopeKind(strings.TrimSpace(r.URL.Query().Get("scope_kind"))), ID: strings.TrimSpace(r.URL.Query().Get("scope_id"))}
	if err := scope.Validate(); err != nil || scope.Kind == appconfig.ScopeBuiltIn {
		return appconfig.ScopeRef{}, errors.New("scope_kind and the required scope_id must identify one mutable configuration scope")
	}
	return scope, nil
}

func (s *Server) configScopeValues(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	scope, err := configScopeFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_configuration_scope", err.Error())
		return
	}
	state, err := service.Scope(r.Context(), scope)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("ETag", configETag("scope", state.Version))
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) configEffective(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	var request struct {
		Scopes []appconfig.ScopeRef `json:"scopes"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	effective, err := service.Effective(r.Context(), request.Scopes)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_effective_configuration", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

func (s *Server) exportRegistryConfig(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	scope, err := configScopeFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_configuration_scope", err.Error())
		return
	}
	document, err := service.ExportScope(r.Context(), scope)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("ETag", configETag("scope", document.ScopeVersion))
	writeJSON(w, http.StatusOK, document)
}

type registryImportRequest struct {
	Mode     string                      `json:"mode"`
	Target   appconfig.ScopeRef          `json:"target"`
	Reason   string                      `json:"reason,omitempty"`
	Document appconfig.DeclarativeConfig `json:"document"`
}

func (s *Server) previewRegistryImport(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	version, ok := requireConfigETag(w, r, "scope")
	if !ok {
		return
	}
	var request registryImportRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	preview, err := service.PreviewImportContext(r.Context(), request.Document, request.Mode, request.Target, version)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) importRegistryConfig(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	version, ok := requireConfigETag(w, r, "scope")
	if !ok {
		return
	}
	var request registryImportRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a reason between 1 and 1000 characters is required")
		return
	}
	draft, preview, err := service.ImportDraft(r.Context(), request.Document, request.Mode,
		request.Target, version, actorID(r), request.Reason)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if !preview.Valid {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"preview": preview})
		return
	}
	w.Header().Set("Location", "/api/v1/config/drafts/"+draft.ID)
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusCreated, map[string]any{"draft": draft, "preview": preview})
}

func (s *Server) configPrerequisites(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	items := service.Prerequisites(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type registryDraftRequest struct {
	Scope   appconfig.ScopeRef     `json:"scope"`
	Reason  string                 `json:"reason"`
	Entries []appconfig.DraftEntry `json:"entries"`
}

type registryReasonRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) listRegistryDrafts(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	scope, err := configScopeFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_configuration_scope", err.Error())
		return
	}
	items, err := service.Drafts(r.Context(), scope, queryInt(r, "limit", 50))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	var request registryDraftRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	baseVersion, ok := requireConfigETag(w, r, "scope")
	if !ok {
		return
	}
	draft, report, err := service.CreateDraft(r.Context(), appconfig.CreateDraftRequest{
		Scope: request.Scope, BaseScopeVersion: baseVersion, AuthorID: actorID(r),
		Reason: request.Reason, Entries: request.Entries,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if !report.Valid {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"validation": report})
		return
	}
	w.Header().Set("Location", "/api/v1/config/drafts/"+draft.ID)
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusCreated, map[string]any{"draft": draft, "validation": report})
}

func (s *Server) getRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	draft, err := service.Draft(r.Context(), r.PathValue("draftID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) updateRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	var request registryDraftRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	version, ok := requireConfigETag(w, r, "draft")
	if !ok {
		return
	}
	draft, report, err := service.UpdateDraft(r.Context(), appconfig.UpdateDraftRequest{
		ID: r.PathValue("draftID"), ExpectedVersion: version, ActorID: actorID(r),
		Reason: request.Reason, Entries: request.Entries,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if !report.Valid {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"validation": report})
		return
	}
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "validation": report})
}

func (s *Server) validateRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	report, check, err := service.ValidateDraft(r.Context(), r.PathValue("draftID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"validation": report, "check": check})
}

func (s *Server) dryRunRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	checks, err := service.DryRunDraft(r.Context(), r.PathValue("draftID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": checks})
}

func (s *Server) reviewRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, version, request, ok := s.registryActionRequest(w, r)
	if !ok {
		return
	}
	draft, report, err := service.ReviewDraft(r.Context(), appconfig.TransitionDraftRequest{
		ID: r.PathValue("draftID"), ExpectedVersion: version, ActorID: actorID(r),
		Target: appconfig.DraftReviewed, Reason: request.Reason,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	if !report.Valid {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"validation": report})
		return
	}
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "validation": report})
}

func (s *Server) applyRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, version, request, ok := s.registryActionRequest(w, r)
	if !ok {
		return
	}
	reauthenticated := false
	if principal, exists := principalFromRequest(r); exists {
		reauthenticated = principal.RecentlyReauthenticated(time.Now().UTC())
	}
	revision, state, report, err := service.ApplyDraft(r.Context(), r.PathValue("draftID"), version,
		actorID(r), actorRole(r), request.Reason, reauthenticated)
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		writeError(w, http.StatusConflict, "configuration_draft_conflict", err.Error())
		return
	}
	if !report.Valid {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"validation": report})
		return
	}
	w.Header().Set("ETag", configETag("scope", state.Version))
	writeJSON(w, http.StatusCreated, map[string]any{"revision": revision, "scope": state, "validation": report})
}

func (s *Server) discardRegistryDraft(w http.ResponseWriter, r *http.Request) {
	service, version, request, ok := s.registryActionRequest(w, r)
	if !ok {
		return
	}
	draft, err := service.DiscardDraft(r.Context(), appconfig.TransitionDraftRequest{
		ID: r.PathValue("draftID"), ExpectedVersion: version, ActorID: actorID(r),
		Target: appconfig.DraftDiscarded, Reason: request.Reason,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("ETag", configETag("draft", draft.Version))
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) registryActionRequest(w http.ResponseWriter, r *http.Request) (*appconfig.RegistryService, int64, registryReasonRequest, bool) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return nil, 0, registryReasonRequest{}, false
	}
	version, ok := requireConfigETag(w, r, "draft")
	if !ok {
		return nil, 0, registryReasonRequest{}, false
	}
	var request registryReasonRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return nil, 0, registryReasonRequest{}, false
	}
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a reason between 1 and 1000 characters is required")
		return nil, 0, registryReasonRequest{}, false
	}
	return service, version, request, true
}

func (s *Server) listRegistryDraftChecks(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	items, err := service.Checks(r.Context(), r.PathValue("draftID"), queryInt(r, "limit", 50))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listRegistryRevisions(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	scope, err := configScopeFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_configuration_scope", err.Error())
		return
	}
	items, err := service.Revisions(r.Context(), scope, queryInt(r, "limit", 50))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) rollbackRegistryRevision(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireConfigRegistry(w)
	if !ok {
		return
	}
	if s.auth != nil {
		principal, exists := principalFromRequest(r)
		if !exists || !principal.RecentlyReauthenticated(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", "configuration rollback requires reauthentication within five minutes")
			return
		}
	}
	version, ok := requireConfigETag(w, r, "scope")
	if !ok {
		return
	}
	var request registryReasonRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	revision, state, err := service.Rollback(r.Context(), r.PathValue("revisionID"), version,
		actorID(r), actorRole(r), request.Reason, s.auth == nil || recentlyReauthenticated(r))
	if err != nil {
		writeError(w, http.StatusConflict, "configuration_rollback_conflict", err.Error())
		return
	}
	w.Header().Set("ETag", configETag("scope", state.Version))
	writeJSON(w, http.StatusCreated, map[string]any{"revision": revision, "scope": state})
}

func recentlyReauthenticated(r *http.Request) bool {
	principal, ok := principalFromRequest(r)
	return ok && principal.RecentlyReauthenticated(time.Now().UTC())
}

func configETag(kind string, version int64) string {
	return fmt.Sprintf(`"config-%s-%d"`, kind, version)
}

func requireConfigETag(w http.ResponseWriter, r *http.Request, kind string) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", "If-Match must contain the current configuration version ETag")
		return 0, false
	}
	prefix := `"config-` + kind + `-`
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, `"`) {
		writeError(w, http.StatusBadRequest, "invalid_if_match", "If-Match is not a configuration version ETag")
		return 0, false
	}
	version, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, prefix), `"`), 10, 64)
	if err != nil || version < 0 {
		writeError(w, http.StatusBadRequest, "invalid_if_match", "If-Match is not a configuration version ETag")
		return 0, false
	}
	return version, true
}
