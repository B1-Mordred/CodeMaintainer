package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
)

func (s *Server) requireCapabilities(w http.ResponseWriter) (*capabilities.Service, bool) {
	if s.capabilities == nil {
		writeError(w, http.StatusServiceUnavailable, "capability_service_unavailable", "capability packs and Repo Doctor are not configured")
		return nil, false
	}
	return s.capabilities, true
}

func (s *Server) capabilityCatalog(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": capabilities.SchemaVersion, "items": service.Catalog()})
}
func (s *Server) capabilityManifest(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	manifest, trust, err := service.Manifest(r.PathValue("packID"), r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusNotFound, "capability_pack_not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest, "trust": trust})
}
func (s *Server) capabilityInstallations(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	items, err := service.Installations(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) capabilityEvents(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	items, err := service.Events(r.Context(), r.PathValue("packID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type capabilityTransitionRequest struct {
	TargetVersion    string `json:"target_version"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

func (s *Server) previewCapabilityTransition(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	var request capabilityTransitionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if action != "install" && action != "upgrade" && action != "rollback" {
		writeError(w, http.StatusBadRequest, "invalid_capability_action", "preview action must be install, upgrade, or rollback")
		return
	}
	result, err := service.PreviewTransition(r.Context(), r.PathValue("packID"), action, request.TargetVersion)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) transitionCapability(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	action := r.PathValue("action")
	switch action {
	case "install", "enable", "disable", "upgrade", "rollback", "pin", "unpin":
	default:
		writeError(w, http.StatusBadRequest, "invalid_capability_action", "the requested capability lifecycle action is not allowed")
		return
	}
	var request capabilityTransitionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	reauthenticated := s.auth == nil || recentlyReauthenticated(r)
	installation, event, err := service.Transition(r.Context(), capabilities.TransitionRequest{PackID: r.PathValue("packID"), Action: action, TargetVersion: request.TargetVersion, ExpectedRevision: request.ExpectedRevision, ActorID: actorID(r), Reason: request.Reason}, reauthenticated)
	if err != nil {
		if strings.Contains(err.Error(), "reauthentication") {
			writeError(w, http.StatusForbidden, "recent_reauthentication_required", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installation": installation, "event": event})
}

func (s *Server) projectCapabilityAssignments(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	items, err := service.Assignments(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type capabilityAssignmentConfigurationRequest struct {
	ExpectedRevision int64           `json:"expected_revision"`
	Reason           string          `json:"reason"`
	Config           json.RawMessage `json:"config"`
}

func (s *Server) previewCapabilityAssignmentConfiguration(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	var request capabilityAssignmentConfigurationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := service.PreviewAssignmentConfiguration(r.Context(), r.PathValue("projectID"), r.PathValue("packID"), request.ExpectedRevision, request.Config)
	if errors.Is(err, capabilities.ErrConfigurationConflict) {
		writeError(w, http.StatusConflict, "stale_capability_configuration", err.Error())
		return
	}
	if errors.Is(err, capabilities.ErrConfigurationNotFound) {
		writeError(w, http.StatusNotFound, "capability_assignment_not_found", err.Error())
		return
	}
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) updateCapabilityAssignmentConfiguration(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	var request capabilityAssignmentConfigurationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	assignment, err := service.UpdateAssignmentConfiguration(r.Context(), capabilities.AssignmentConfigurationRequest{
		ProjectID: r.PathValue("projectID"), PackID: r.PathValue("packID"), ExpectedRevision: request.ExpectedRevision,
		Config: request.Config, ActorID: actorID(r), Reason: request.Reason,
	})
	if errors.Is(err, capabilities.ErrConfigurationConflict) {
		writeError(w, http.StatusConflict, "stale_capability_configuration", err.Error())
		return
	}
	if errors.Is(err, capabilities.ErrConfigurationNotFound) {
		writeError(w, http.StatusNotFound, "capability_assignment_not_found", err.Error())
		return
	}
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, assignment)
}
func (s *Server) listRepoDoctorScans(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	items, err := service.Scans(r.Context(), r.PathValue("projectID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) getRepoDoctorScan(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	item, err := service.GetScan(r.Context(), r.PathValue("projectID"), r.PathValue("scanID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) runRepoDoctor(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	probe := make([]byte, 1)
	if count, err := r.Body.Read(probe); count > 0 || (err != nil && err != io.EOF) {
		writeError(w, http.StatusBadRequest, "repository_source_rejected", "Repo Doctor accepts no browser-supplied repository bytes or detector rules")
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
	files := make([]capabilities.SourceFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		files = append(files, capabilities.SourceFile{Path: file.Path, Content: file.Content})
	}
	scan, err := service.Scan(r.Context(), capabilities.ScanInput{ProjectID: projectID, Repository: project.Repository, Revision: snapshot.Revision, Files: files, ExcludedFiles: snapshot.Excluded}, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, scan)
}

type repoDoctorProposalRequest struct {
	ExpectedVersion int64           `json:"expected_version"`
	Reason          string          `json:"reason"`
	Config          json.RawMessage `json:"config,omitempty"`
}

func (s *Server) dryRunRepoDoctorProposal(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	var request repoDoctorProposalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	result, err := service.DryRun(r.Context(), r.PathValue("projectID"), r.PathValue("scanID"), r.PathValue("proposalID"), request.Config)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) reviewRepoDoctorProposal(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireCapabilities(w)
	if !ok {
		return
	}
	action := r.PathValue("action")
	if action != "accept" && action != "reject" {
		writeError(w, http.StatusBadRequest, "invalid_proposal_action", "proposal action must be accept or reject")
		return
	}
	var request repoDoctorProposalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	proposal, assignment, err := service.Review(r.Context(), capabilities.ReviewRequest{ProjectID: r.PathValue("projectID"), ScanID: r.PathValue("scanID"), ProposalID: r.PathValue("proposalID"), ExpectedVersion: request.ExpectedVersion, Accept: action == "accept", ActorID: actorID(r), Reason: request.Reason, Config: request.Config})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "assignment": assignment})
}
