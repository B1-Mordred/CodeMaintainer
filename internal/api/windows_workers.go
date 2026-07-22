package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
)

func (s *Server) requireWindowsWorkers(w http.ResponseWriter) (*windowsworker.Service, bool) {
	if s.windowsWorkers == nil {
		writeError(w, http.StatusServiceUnavailable, "windows_worker_service_unavailable", "Windows worker profiles and execution are not configured")
		return nil, false
	}
	return s.windowsWorkers, true
}

func (s *Server) listWindowsWorkerProfiles(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	items, err := service.Profiles(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": windowsworker.SchemaVersion, "approved_job_types": windowsworker.ApprovedJobTypes, "items": items})
}

func (s *Server) getWindowsWorkerProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	item, err := service.Profile(r.Context(), r.PathValue("profileID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type windowsWorkerProfileRequest struct {
	Name                   string                    `json:"name"`
	Mode                   string                    `json:"mode"`
	Endpoint               string                    `json:"endpoint"`
	EndpointAllowlist      []string                  `json:"endpoint_allowlist"`
	CredentialReference    string                    `json:"credential_reference,omitempty"`
	CredentialStatus       string                    `json:"credential_status"`
	Health                 string                    `json:"health"`
	Capacity               int                       `json:"capacity"`
	VMTemplateID           string                    `json:"vm_template_id"`
	Toolchains             map[string]string         `json:"toolchains"`
	AllowedJobTypes        []string                  `json:"allowed_job_types"`
	TimeoutSeconds         int                       `json:"timeout_seconds"`
	SimulatorProfileIDs    []string                  `json:"simulator_profile_ids"`
	ArtifactRetentionDays  int                       `json:"artifact_retention_days"`
	SigningPolicyReference string                    `json:"signing_policy_reference,omitempty"`
	ManualGates            windowsworker.ManualGates `json:"manual_gates"`
	Enabled                bool                      `json:"enabled"`
	ExpectedRevision       int64                     `json:"expected_revision"`
	Reason                 string                    `json:"reason"`
}

func (s *Server) saveWindowsWorkerProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	var request windowsWorkerProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	profile := windowsworker.Profile{
		ID: r.PathValue("profileID"), Name: request.Name, Mode: request.Mode, Endpoint: request.Endpoint,
		EndpointAllowlist: request.EndpointAllowlist, CredentialReference: request.CredentialReference,
		CredentialStatus: request.CredentialStatus, Health: request.Health, Capacity: request.Capacity,
		VMTemplateID: request.VMTemplateID, Toolchains: request.Toolchains, AllowedJobTypes: request.AllowedJobTypes,
		TimeoutSeconds: request.TimeoutSeconds, SimulatorProfileIDs: request.SimulatorProfileIDs,
		ArtifactRetentionDays: request.ArtifactRetentionDays, SigningPolicyReference: request.SigningPolicyReference,
		ManualGates: request.ManualGates, Enabled: request.Enabled,
	}
	item, err := service.SaveProfile(r.Context(), windowsworker.SaveProfileRequest{
		Profile: profile, ExpectedRevision: request.ExpectedRevision, ActorID: actorID(r), Reason: request.Reason,
		Reauthenticated: s.auth == nil || recentlyReauthenticated(r),
	})
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) {
			s.storageError(w, r, err)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_windows_worker_profile", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) probeWindowsWorkerProfile(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	item, err := service.Probe(r.Context(), r.PathValue("profileID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "windows_worker_probe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listWindowsWorkerRuns(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	items, err := service.Runs(r.Context(), r.PathValue("profileID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type windowsWorkerRunRequest struct {
	ProjectID      string                       `json:"project_id"`
	JobID          string                       `json:"job_id"`
	JobType        string                       `json:"job_type"`
	Input          windowsworker.ImmutableInput `json:"input"`
	IdempotencyKey string                       `json:"idempotency_key"`
	OperatorGated  bool                         `json:"operator_gated"`
}

func (s *Server) runWindowsWorkerJob(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireWindowsWorkers(w)
	if !ok {
		return
	}
	var request windowsWorkerRunRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.OperatorGated && s.auth != nil && !recentlyReauthenticated(r) {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "operator-gated Windows jobs require recent reauthentication")
		return
	}
	item, err := service.Run(r.Context(), windowsworker.RunRequest{
		ProfileID: r.PathValue("profileID"), ProjectID: request.ProjectID, JobID: request.JobID,
		JobType: request.JobType, Input: request.Input, IdempotencyKey: request.IdempotencyKey,
		OperatorGated: request.OperatorGated, ActorID: actorID(r), ActorRole: actorRole(r),
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrIdempotencyKey) {
			s.storageError(w, r, err)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "windows_worker_run_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}
