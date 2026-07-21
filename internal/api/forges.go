package api

import (
	"net/http"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/forges"
)

func (s *Server) requireForges(w http.ResponseWriter) (*forges.Service, bool) {
	if s.forges == nil {
		writeError(w, http.StatusServiceUnavailable, "forge_service_unavailable", "forge profiles and normalized synchronization are not configured")
		return nil, false
	}
	return s.forges, true
}

func (s *Server) listForgeProfiles(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	items, err := service.Profiles(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": forges.SchemaVersion, "items": items})
}

func (s *Server) getForgeProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	item, err := service.Profile(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type forgeProfileRequest struct {
	Provider                string            `json:"provider"`
	Endpoint                string            `json:"endpoint"`
	EndpointAllowlist       []string          `json:"endpoint_allowlist"`
	Repository              string            `json:"repository"`
	CredentialReference     string            `json:"credential_reference,omitempty"`
	CredentialStatus        string            `json:"credential_status"`
	WebhookStatus           string            `json:"webhook_status"`
	SyncDirection           string            `json:"sync_direction"`
	PollingMinutes          int               `json:"polling_minutes"`
	BranchConvention        string            `json:"branch_convention"`
	ChangeRequestConvention string            `json:"change_request_convention"`
	LabelMapping            map[string]string `json:"label_mapping"`
	CIArtifactPolicy        string            `json:"ci_artifact_policy"`
	ReleasePolicy           string            `json:"release_policy"`
	SubmodulesEnabled       bool              `json:"submodules_enabled"`
	Enabled                 bool              `json:"enabled"`
	ExpectedRevision        int64             `json:"expected_revision"`
	Reason                  string            `json:"reason"`
}

func (s *Server) saveForgeProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	var request forgeProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	profile := forges.Profile{
		ProjectID: r.PathValue("projectID"), Provider: request.Provider, Endpoint: request.Endpoint,
		EndpointAllowlist: request.EndpointAllowlist, Repository: request.Repository,
		CredentialReference: request.CredentialReference, CredentialStatus: request.CredentialStatus,
		WebhookStatus: request.WebhookStatus, SyncDirection: request.SyncDirection,
		PollingMinutes: request.PollingMinutes, BranchConvention: request.BranchConvention,
		ChangeRequestConvention: request.ChangeRequestConvention, LabelMapping: request.LabelMapping,
		CIArtifactPolicy: request.CIArtifactPolicy, ReleasePolicy: request.ReleasePolicy,
		SubmodulesEnabled: request.SubmodulesEnabled, Enabled: request.Enabled,
	}
	item, err := service.SaveProfile(r.Context(), forges.SaveProfileRequest{
		Profile: profile, ExpectedRevision: request.ExpectedRevision, ActorID: actorID(r), Reason: request.Reason,
		Reauthenticated: s.auth == nil || recentlyReauthenticated(r),
	})
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) probeForgeProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	item, err := service.Probe(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "forge_probe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type forgeSyncRequest struct {
	Cursor         string `json:"cursor,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) syncForgeProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	var request forgeSyncRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	item, err := service.Sync(r.Context(), r.PathValue("projectID"), request.Cursor, request.IdempotencyKey, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listForgeSyncRuns(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	items, err := service.Runs(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listForgeObjects(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireForges(w)
	if !ok {
		return
	}
	items, err := service.Objects(r.Context(), r.PathValue("projectID"), strings.TrimSpace(r.URL.Query().Get("kind")))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
