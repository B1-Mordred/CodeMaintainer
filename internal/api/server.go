package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
	"github.com/local-code-maintainer/appliance/internal/automation"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	"github.com/local-code-maintainer/appliance/internal/ui"
)

const maxRequestBody = 1 << 20

type Server struct {
	store        storage.Store
	artifacts    ArtifactReader
	logger       *slog.Logger
	profile      string
	started      time.Time
	handler      http.Handler
	auth         *maintainerauth.Service
	memoryIndex  memory.Index
	secureCookie bool
	hermesToken  []byte
	githubEvents GitHubWebhookValidator
}

type ArtifactReader interface {
	Open(context.Context, string, string) (storage.ArtifactRecord, io.ReadCloser, error)
}

type GitHubWebhookValidator interface {
	ValidateWebhook(context.Context, gitbridge.WebhookValidationRequest) (gitbridge.PullRequestEvent, error)
}

type Option func(*Server)

func WithArtifactReader(reader ArtifactReader) Option {
	return func(server *Server) { server.artifacts = reader }
}

func WithAuthentication(service *maintainerauth.Service, secureCookie bool) Option {
	return func(server *Server) {
		server.auth = service
		server.secureCookie = secureCookie
	}
}

func WithMemoryIndex(index memory.Index) Option {
	return func(server *Server) { server.memoryIndex = index }
}

func WithHermesToken(token []byte) Option {
	return func(server *Server) { server.hermesToken = append([]byte(nil), token...) }
}

func WithGitHubWebhookValidator(validator GitHubWebhookValidator) Option {
	return func(server *Server) { server.githubEvents = validator }
}

func NewServer(store storage.Store, logger *slog.Logger, profile string, options ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: store, logger: logger, profile: profile, started: time.Now().UTC()}
	for _, option := range options {
		option(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", s.authBootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("GET /api/v1/auth/session", s.authSession)
	mux.HandleFunc("POST /api/v1/auth/reauthenticate", s.authReauthenticate)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("GET /api/v1/system/status", s.systemStatus)
	mux.HandleFunc("POST /api/v1/github/webhooks", s.githubWebhook)
	mux.HandleFunc("GET /api/v1/workflow/states", s.workflowStates)
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("POST /api/v1/projects", s.upsertProject)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory", s.listProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory", s.createProjectMemory)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/retrievals", s.listProjectMemoryRetrievals)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/export", s.exportProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/actions/restore", s.restoreProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/actions/reindex", s.reindexProjectMemory)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/{memoryID}", s.getProjectMemory)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/{memoryID}/events", s.listProjectMemoryEvents)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/{memoryID}/actions/promote", s.promoteProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/{memoryID}/actions/correct", s.correctProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/{memoryID}/actions/invalidate", s.invalidateProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/{memoryID}/actions/delete", s.deleteProjectMemory)
	mux.HandleFunc("GET /api/v1/schedules", s.listSchedules)
	mux.HandleFunc("POST /api/v1/schedules", s.saveSchedule)
	mux.HandleFunc("GET /api/v1/schedule-runs", s.listScheduleRuns)
	mux.HandleFunc("GET /api/v1/skill-proposals", s.listSkillProposals)
	mux.HandleFunc("POST /api/v1/skill-proposals/{proposalID}/actions/review", s.reviewSkillProposal)
	mux.HandleFunc("GET /api/v1/automation-requests", s.listAutomationRequests)
	mux.HandleFunc("POST /api/v1/hermes/tools/jobs/submit", s.hermesSubmitJob)
	mux.HandleFunc("GET /api/v1/hermes/tools/jobs", s.hermesListJobs)
	mux.HandleFunc("GET /api/v1/hermes/tools/jobs/{jobID}", s.hermesJobStatus)
	mux.HandleFunc("POST /api/v1/hermes/tools/jobs/{jobID}/cancel", s.hermesCancelJob)
	mux.HandleFunc("GET /api/v1/hermes/tools/jobs/{jobID}/report", s.hermesJobReport)
	mux.HandleFunc("POST /api/v1/hermes/tools/jobs/{jobID}/request-review", s.hermesRequestReview)
	mux.HandleFunc("POST /api/v1/hermes/tools/jobs/{jobID}/request-publication-approval", s.hermesRequestPublicationApproval)
	mux.HandleFunc("GET /api/v1/hermes/tools/projects/{projectID}/memory", s.hermesProjectMemory)
	mux.HandleFunc("GET /api/v1/hermes/tools/schedules", s.hermesListSchedules)
	mux.HandleFunc("POST /api/v1/hermes/tools/skill-proposals", s.hermesCreateSkillProposal)
	mux.HandleFunc("GET /api/v1/jobs", s.listJobs)
	mux.HandleFunc("POST /api/v1/jobs", s.createJob)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}", s.getJob)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}/events", s.jobEvents)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}/artifacts", s.listJobArtifacts)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}/artifacts/{artifactID}", s.downloadJobArtifact)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/cancel", s.cancelJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/retry", s.retryJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/approve-publication", s.approvePublication)
	mux.HandleFunc("GET /api/v1/config", s.getConfig)
	mux.HandleFunc("POST /api/v1/config/validate", s.validateConfig)
	mux.HandleFunc("GET /api/v1/config/revisions", s.listConfigRevisions)
	mux.HandleFunc("POST /api/v1/config/revisions", s.createConfigRevision)
	mux.HandleFunc("POST /api/v1/config/revisions/{revisionID}/rollback", s.rollbackConfigRevision)
	mux.HandleFunc("GET /api/v1/audit", s.listAudit)
	mux.Handle("GET /", s.staticHandler())
	s.handler = s.middleware(s.hermesAuthenticationMiddleware(s.authenticationMiddleware(mux)))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
		s.logger.InfoContext(r.Context(), "http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.ListJobs(r.Context(), 1, 0); err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "durable storage is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) systemStatus(w http.ResponseWriter, _ *http.Request) {
	memoryStatus := "sqlite"
	if s.memoryIndex != nil {
		memoryStatus = "openviking"
		if s.profile == "mock" {
			memoryStatus = "fake-index"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "healthy", "profile": s.profile,
		"uptime_seconds": int64(time.Since(s.started).Seconds()),
		"components": map[string]string{
			"controller": "healthy", "storage": "healthy",
			"runner": "fake", "model": "unloaded", "memory": memoryStatus, "git": "fake",
		},
	})
}

func (s *Server) workflowStates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs.AllStates()})
}

func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.githubEvents == nil {
		writeError(w, http.StatusServiceUnavailable, "github_webhooks_disabled", "GitHub webhook validation is disabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	payload, err := io.ReadAll(r.Body)
	if err != nil || len(payload) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_webhook", "the bounded webhook payload is invalid")
		return
	}
	event, err := s.githubEvents.ValidateWebhook(r.Context(), gitbridge.WebhookValidationRequest{
		DeliveryID: r.Header.Get("X-GitHub-Delivery"), Event: r.Header.Get("X-GitHub-Event"),
		Signature256: r.Header.Get("X-Hub-Signature-256"), Payload: base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_webhook_signature", "the GitHub webhook could not be authenticated")
		return
	}
	result, err := s.store.ApplyGitHubPullRequestEvent(r.Context(), event)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListProjects(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) upsertProject(w http.ResponseWriter, r *http.Request) {
	var request projects.UpsertRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	project, err := s.store.UpsertProject(r.Context(), request, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	items, err := s.store.ListJobs(r.Context(), limit, offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var request jobs.CreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Repository = strings.TrimSpace(request.Repository)
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.Task = strings.TrimSpace(request.Task)
	if request.ProjectID == "" || !validRepository(request.Repository) || request.Task == "" {
		writeError(w, http.StatusBadRequest, "invalid_job", "project_id, owner/repository, and task are required")
		return
	}
	project, err := s.store.GetProject(r.Context(), request.ProjectID)
	if err != nil || !project.Enabled || project.Repository != request.Repository {
		writeError(w, http.StatusUnprocessableEntity, "project_not_registered", "job project must be enabled and match its registered repository")
		return
	}
	details, _ := json.Marshal(map[string]string{"source": "api"})
	job, err := s.store.CreateJob(r.Context(), storage.CreateJobParams{
		ProjectID: request.ProjectID, Repository: request.Repository, Task: request.Task,
		IssueNumber: request.IssueNumber, ActorID: actorID(r), Details: details,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
	writeJSON(w, http.StatusCreated, job)
}

func validRepository(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(value) > 200 {
		return false
	}
	for _, part := range parts {
		if part == "." || part == ".." {
			return false
		}
		for _, runeValue := range part {
			if !((runeValue >= 'a' && runeValue <= 'z') || (runeValue >= 'A' && runeValue <= 'Z') ||
				(runeValue >= '0' && runeValue <= '9') || strings.ContainsRune("._-", runeValue)) {
				return false
			}
		}
	}
	return true
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	transitions, err := s.store.ListTransitions(r.Context(), job.ID, 0, 500)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	findings, err := s.store.ListFindings(r.Context(), job.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	approvals, err := s.store.ListApprovals(r.Context(), job.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	phases, err := s.store.ListPhaseRecords(r.Context(), job.ID, 500)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job": job, "transitions": transitions, "findings": findings, "approvals": approvals, "phases": phases,
	})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateCancelled, "operator cancelled job")
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateQueued, "operator retried job")
}

func (s *Server) approvePublication(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Rationale       string `json:"rationale"`
		Reauthenticated bool   `json:"reauthenticated,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	current, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	reauthenticated := request.Reauthenticated
	if principal, ok := principalFromRequest(r); ok {
		reauthenticated = principal.RecentlyReauthenticated(time.Now().UTC())
	}
	job, approval, err := s.store.ApprovePublication(r.Context(), current.ID, storage.PublicationApprovalRequest{
		ActorID: actorID(r), ActorRole: actorRole(r), Rationale: request.Rationale,
		Reauthenticated: reauthenticated, ExpectedVersion: current.Version,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "approval": approval})
}

func (s *Server) transitionAction(w http.ResponseWriter, r *http.Request, to jobs.State, reason string) {
	current, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	job, err := s.store.TransitionJob(r.Context(), current.ID, jobs.TransitionRequest{
		To: to, ActorID: actorID(r), Reason: reason, ExpectedVersion: current.Version,
		Details: json.RawMessage(`{"source":"api"}`),
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.GetJob(r.Context(), r.PathValue("jobID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	after := int64(queryInt(r, "after", 0))
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		if value, err := strconv.ParseInt(header, 10, 64); err == nil && value > after {
			after = value
		}
	}
	poll := time.NewTicker(time.Second)
	keepalive := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer keepalive.Stop()
	for {
		items, err := s.store.ListTransitions(r.Context(), r.PathValue("jobID"), after, 100)
		if err != nil {
			return
		}
		for _, item := range items {
			payload, _ := json.Marshal(item)
			fmt.Fprintf(w, "id: %d\nevent: transition\ndata: %s\n\n", item.Sequence, payload)
			after = item.Sequence
		}
		if len(items) > 0 {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) listJobArtifacts(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.store.ListJobArtifacts(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) downloadJobArtifact(w http.ResponseWriter, r *http.Request) {
	if s.artifacts == nil {
		writeError(w, http.StatusServiceUnavailable, "artifact_store_unavailable", "artifact content is unavailable")
		return
	}
	record, reader, err := s.artifacts.Open(r.Context(), r.PathValue("jobID"), r.PathValue("artifactID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(record.Bytes, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+record.ID+`"`)
	w.Header().Set("X-Artifact-SHA256", record.ObjectSHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, reader, record.Bytes)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	revision, err := s.store.CurrentConfig(r.Context())
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	var document appconfig.System
	if err := json.Unmarshal(revision.After, &document); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": revision, "document": document})
}

type configDocumentRequest struct {
	Document appconfig.System `json:"document"`
	Reason   string           `json:"reason,omitempty"`
}

type rollbackConfigRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) validateConfig(w http.ResponseWriter, r *http.Request) {
	var request configDocumentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	_, current, err := s.currentSystemConfig(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	errors := appconfig.ValidateChange(current, request.Document)
	if errors == nil {
		errors = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": len(errors) == 0, "errors": errors})
}

func (s *Server) createConfigRevision(w http.ResponseWriter, r *http.Request) {
	var request configDocumentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a reason between 1 and 1000 characters is required")
		return
	}
	currentRevision, current, err := s.currentSystemConfig(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	validationErrors := appconfig.ValidateChange(current, request.Document)
	if len(validationErrors) != 0 {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"error": map[string]any{"code": "invalid_configuration", "message": "configuration validation failed", "details": validationErrors},
		})
		return
	}
	after, err := json.Marshal(request.Document)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	diff, err := appconfig.Diff(currentRevision.After, after)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if string(diff) == "[]" {
		writeError(w, http.StatusConflict, "no_change", "configuration is unchanged")
		return
	}
	validationResult, _ := json.Marshal(map[string]any{"valid": true, "errors": []string{}})
	revision, err := s.store.CreateConfigRevision(r.Context(), appconfig.Revision{
		ActorID: actorID(r), SchemaVersion: appconfig.SchemaVersion,
		Before: currentRevision.After, After: after, Diff: diff,
		ValidationResult: validationResult, Reason: request.Reason,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/config/revisions/"+revision.ID)
	writeJSON(w, http.StatusCreated, revision)
}

func (s *Server) rollbackConfigRevision(w http.ResponseWriter, r *http.Request) {
	var request rollbackConfigRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_reason", "a reason between 1 and 1000 characters is required")
		return
	}
	target, err := s.store.GetConfigRevision(r.Context(), r.PathValue("revisionID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	currentRevision, current, err := s.currentSystemConfig(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var targetDocument appconfig.System
	if err := json.Unmarshal(target.After, &targetDocument); err != nil {
		s.internalError(w, r, fmt.Errorf("decode rollback target: %w", err))
		return
	}
	validationErrors := appconfig.ValidateChange(current, targetDocument)
	if len(validationErrors) != 0 {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"error": map[string]any{"code": "invalid_rollback", "message": "rollback target is incompatible", "details": validationErrors},
		})
		return
	}
	diff, err := appconfig.Diff(currentRevision.After, target.After)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if string(diff) == "[]" {
		writeError(w, http.StatusConflict, "no_change", "configuration already matches the selected revision")
		return
	}
	validationResult, _ := json.Marshal(map[string]any{"valid": true, "errors": []string{}})
	revision, err := s.store.CreateConfigRevision(r.Context(), appconfig.Revision{
		ActorID: actorID(r), SchemaVersion: appconfig.SchemaVersion,
		Before: currentRevision.After, After: target.After, Diff: diff,
		ValidationResult: validationResult, RollbackOf: target.ID, Reason: request.Reason,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, revision)
}

func (s *Server) currentSystemConfig(ctx context.Context) (appconfig.Revision, appconfig.System, error) {
	revision, err := s.store.CurrentConfig(ctx)
	if err != nil {
		return appconfig.Revision{}, appconfig.System{}, err
	}
	var document appconfig.System
	if err := json.Unmarshal(revision.After, &document); err != nil {
		return appconfig.Revision{}, appconfig.System{}, fmt.Errorf("decode current configuration: %w", err)
	}
	return revision, document, nil
}

func (s *Server) listConfigRevisions(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListConfigRevisions(r.Context(), queryInt(r, "limit", 50))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAudit(r.Context(), int64(queryInt(r, "after", 0)), queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) staticHandler() http.Handler {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func actorID(r *http.Request) string {
	if actor, ok := serviceActorFromRequest(r); ok {
		return actor
	}
	if principal, ok := principalFromRequest(r); ok {
		return principal.User.ID
	}
	if value := strings.TrimSpace(r.Header.Get("X-Maintainer-Actor")); value != "" && len(value) <= 128 {
		return value
	}
	return "local-operator"
}

func actorRole(r *http.Request) string {
	if principal, ok := principalFromRequest(r); ok {
		return string(principal.User.Role)
	}
	value := strings.TrimSpace(r.Header.Get("X-Maintainer-Role"))
	if value == "viewer" || value == "operator" || value == "reviewer" || value == "administrator" {
		return value
	}
	return "operator"
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONLimit(w, r, destination, maxRequestBody)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content_type", "Content-Type must be application/json")
		return errors.New("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be one valid JSON object")
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain only one JSON object")
		return errors.New("multiple JSON values")
	}
	return nil
}

func (s *Server) storageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, memory.ErrNotFound), errors.Is(err, automation.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, storage.ErrConflict), errors.Is(err, memory.ErrConflict), errors.Is(err, automation.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "resource changed; refresh and retry")
	case errors.Is(err, storage.ErrInvalid), errors.Is(err, memory.ErrInvalid), errors.Is(err, memory.ErrScope), errors.Is(err, automation.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
	case errors.Is(err, storage.ErrBudgetExceeded):
		writeError(w, http.StatusUnprocessableEntity, "budget_exceeded", "the job token or wall-time budget is exhausted")
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) { writeJSONStatus(w, status, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// ShutdownContext is shared by process entrypoints and tests that need a
// bounded graceful-shutdown context.
func ShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Second)
}
