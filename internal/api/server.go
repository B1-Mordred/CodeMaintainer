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
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/backup"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/findings"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/models"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	"github.com/local-code-maintainer/appliance/internal/ui"
)

const maxRequestBody = 1 << 20

type Server struct {
	store          storage.Store
	artifacts      ArtifactReader
	logger         *slog.Logger
	profile        string
	started        time.Time
	version        string
	handler        http.Handler
	auth           *maintainerauth.Service
	memoryIndex    memory.Index
	secureCookie   bool
	hermesToken    []byte
	githubEvents   GitHubWebhookValidator
	gitOperator    GitOperator
	modelManager   models.Manager
	backups        BackupService
	configRegistry *appconfig.RegistryService
	intelligence   *intelligence.Service
}

type ArtifactReader interface {
	Open(context.Context, string, string) (storage.ArtifactRecord, io.ReadCloser, error)
}

type GitHubWebhookValidator interface {
	ValidateWebhook(context.Context, gitbridge.WebhookValidationRequest) (gitbridge.PullRequestEvent, error)
}

type GitOperator interface {
	Register(context.Context, gitbridge.Registration) error
	Sync(context.Context, string) (gitbridge.SyncResult, error)
	RepositoryDiagnostics(context.Context, string) (gitbridge.RepositoryDiagnostics, error)
	Snapshot(context.Context, string, string) (gitbridge.RepositorySnapshot, error)
}

type BackupService interface {
	Create(context.Context) (backup.Record, error)
	List(context.Context) ([]backup.Record, error)
	Validate(context.Context, string) (backup.Report, error)
	StageRestore(context.Context, string) (backup.RestoreResult, error)
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

func WithGitOperator(operator GitOperator) Option {
	return func(server *Server) { server.gitOperator = operator }
}

func WithModelManager(manager models.Manager) Option {
	return func(server *Server) { server.modelManager = manager }
}

func WithBackupService(service BackupService) Option {
	return func(server *Server) { server.backups = service }
}

func WithConfigRegistry(service *appconfig.RegistryService) Option {
	return func(server *Server) { server.configRegistry = service }
}

func WithIntelligence(service *intelligence.Service) Option {
	return func(server *Server) { server.intelligence = service }
}

func WithVersion(version string) Option { return func(server *Server) { server.version = version } }

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
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /api/v1/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", s.authBootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("GET /api/v1/auth/session", s.authSession)
	mux.HandleFunc("POST /api/v1/auth/reauthenticate", s.authReauthenticate)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("GET /api/v1/admin/users", s.listUsers)
	mux.HandleFunc("POST /api/v1/admin/users", s.createUser)
	mux.HandleFunc("PUT /api/v1/admin/users/{userID}", s.updateUser)
	mux.HandleFunc("GET /api/v1/admin/backups", s.listBackups)
	mux.HandleFunc("POST /api/v1/admin/backups", s.createBackup)
	mux.HandleFunc("POST /api/v1/admin/backups/{backupID}/actions/restore", s.restoreBackup)
	mux.HandleFunc("GET /api/v1/admin/update/preflight", s.updatePreflight)
	mux.HandleFunc("GET /api/v1/system/status", s.systemStatus)
	mux.HandleFunc("POST /api/v1/github/webhooks", s.githubWebhook)
	mux.HandleFunc("GET /api/v1/workflow/states", s.workflowStates)
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("POST /api/v1/projects", s.upsertProject)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}", s.disableProject)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/actions/sync", s.syncProject)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/diagnostics", s.projectDiagnostics)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/intelligence/status", s.intelligenceStatus)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/intelligence/query", s.queryIntelligence)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/intelligence/actions/refresh", s.refreshIntelligence)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/intelligence/actions/rebuild", s.rebuildIntelligence)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/context-manifests/{manifestID}", s.getContextManifest)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/context-manifests", s.listContextManifests)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/context-manifests/actions/compare", s.compareContextManifests)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/baselines", s.listProjectBaselines)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/baseline-supersessions", s.listBaselineSupersessions)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/baselines/{baselineID}/actions/supersede", s.supersedeBaseline)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/differentials", s.listProjectDifferentials)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/differential-corrections", s.listDifferentialCorrections)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/differentials/{differentialID}/actions/correct", s.correctDifferential)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/test-impacts", s.listProjectTestImpacts)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/test-impact-overrides", s.listTestImpactOverrides)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/test-impacts/{impactID}/actions/override", s.overrideTestImpact)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/caches", s.listProjectCaches)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/caches/actions/verify", s.verifyProjectCaches)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/caches/actions/simulate", s.simulateProjectCache)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/caches/actions/warm", s.refreshIntelligence)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/caches/actions/purge", s.purgeProjectCaches)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory", s.listProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory", s.createProjectMemory)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/retrievals", s.listProjectMemoryRetrievals)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/memory/export", s.exportProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/actions/restore", s.restoreProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/actions/reindex", s.reindexProjectMemory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/memory/actions/smoke-index", s.smokeProjectMemoryIndex)
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
	mux.HandleFunc("GET /api/v1/notifications", s.listNotifications)
	mux.HandleFunc("POST /api/v1/notifications/{notificationID}/actions/read", s.acknowledgeNotification)
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
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/verify", s.requestVerification)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/review", s.requestReview)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/findings/{findingID}/actions/{action}", s.findingAction)
	mux.HandleFunc("GET /api/v1/models", s.listModels)
	mux.HandleFunc("GET /api/v1/models/status", s.modelStatus)
	mux.HandleFunc("POST /api/v1/models/{profileID}/actions/benchmark", s.benchmarkModel)
	mux.HandleFunc("POST /api/v1/models/{profileID}/actions/load", s.loadModel)
	mux.HandleFunc("POST /api/v1/models/actions/unload", s.unloadModel)
	mux.HandleFunc("GET /api/v1/config", s.getConfig)
	mux.HandleFunc("POST /api/v1/config/validate", s.validateConfig)
	mux.HandleFunc("GET /api/v1/config/revisions", s.listConfigRevisions)
	mux.HandleFunc("POST /api/v1/config/revisions", s.createConfigRevision)
	mux.HandleFunc("POST /api/v1/config/revisions/{revisionID}/rollback", s.rollbackConfigRevision)
	mux.HandleFunc("GET /api/v1/config/descriptors", s.configDescriptors)
	mux.HandleFunc("GET /api/v1/config/values", s.configScopeValues)
	mux.HandleFunc("POST /api/v1/config/effective", s.configEffective)
	mux.HandleFunc("GET /api/v1/config/export", s.exportRegistryConfig)
	mux.HandleFunc("POST /api/v1/config/import/preview", s.previewRegistryImport)
	mux.HandleFunc("POST /api/v1/config/import", s.importRegistryConfig)
	mux.HandleFunc("GET /api/v1/config/prerequisites", s.configPrerequisites)
	mux.HandleFunc("GET /api/v1/config/drafts", s.listRegistryDrafts)
	mux.HandleFunc("POST /api/v1/config/drafts", s.createRegistryDraft)
	mux.HandleFunc("GET /api/v1/config/drafts/{draftID}", s.getRegistryDraft)
	mux.HandleFunc("PUT /api/v1/config/drafts/{draftID}", s.updateRegistryDraft)
	mux.HandleFunc("GET /api/v1/config/drafts/{draftID}/checks", s.listRegistryDraftChecks)
	mux.HandleFunc("POST /api/v1/config/drafts/{draftID}/actions/validate", s.validateRegistryDraft)
	mux.HandleFunc("POST /api/v1/config/drafts/{draftID}/actions/dry-run", s.dryRunRegistryDraft)
	mux.HandleFunc("POST /api/v1/config/drafts/{draftID}/actions/review", s.reviewRegistryDraft)
	mux.HandleFunc("POST /api/v1/config/drafts/{draftID}/actions/apply", s.applyRegistryDraft)
	mux.HandleFunc("POST /api/v1/config/drafts/{draftID}/actions/discard", s.discardRegistryDraft)
	mux.HandleFunc("GET /api/v1/config/registry-revisions", s.listRegistryRevisions)
	mux.HandleFunc("POST /api/v1/config/registry-revisions/{revisionID}/actions/rollback", s.rollbackRegistryRevision)
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

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListJobs(r.Context(), 200, 0)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "durable storage is unavailable")
		return
	}
	counts := map[jobs.State]int{}
	phaseCounts := map[jobs.State]int{}
	findingCounts := map[string]int{}
	for _, job := range items {
		counts[job.State]++
		if phases, phaseErr := s.store.ListPhaseRecords(r.Context(), job.ID, 500); phaseErr == nil {
			for _, phase := range phases {
				phaseCounts[phase.PhaseState]++
			}
		}
		if records, findingErr := s.store.ListFindings(r.Context(), job.ID); findingErr == nil {
			for _, record := range records {
				findingCounts[record.Severity+":"+string(record.Status)]++
			}
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP maintainer_uptime_seconds Controller process uptime.\n# TYPE maintainer_uptime_seconds gauge\nmaintainer_uptime_seconds %d\n", int64(time.Since(s.started).Seconds()))
	fmt.Fprintln(w, "# HELP maintainer_jobs Retained jobs by durable state.\n# TYPE maintainer_jobs gauge")
	for _, state := range jobs.AllStates() {
		fmt.Fprintf(w, "maintainer_jobs{state=%q} %d\n", state, counts[state])
	}
	fmt.Fprintln(w, "# HELP maintainer_phase_completions Retained phase completions by workflow phase.\n# TYPE maintainer_phase_completions gauge")
	for _, state := range jobs.AllStates() {
		fmt.Fprintf(w, "maintainer_phase_completions{phase=%q} %d\n", state, phaseCounts[state])
	}
	fmt.Fprintln(w, "# HELP maintainer_qc_findings Retained QC findings by severity and lifecycle status.\n# TYPE maintainer_qc_findings gauge")
	for key, count := range findingCounts {
		parts := strings.SplitN(key, ":", 2)
		fmt.Fprintf(w, "maintainer_qc_findings{severity=%q,status=%q} %d\n", parts[0], parts[1], count)
	}
	if s.modelManager != nil {
		if model, modelErr := s.modelManager.Status(r.Context()); modelErr == nil {
			loaded := 0
			if model.State == "loaded" {
				loaded = 1
			}
			fmt.Fprintln(w, "# HELP maintainer_model_loaded Whether an allow-listed model is resident.\n# TYPE maintainer_model_loaded gauge")
			fmt.Fprintf(w, "maintainer_model_loaded{profile=%q} %d\n", model.ProfileID, loaded)
			fmt.Fprintln(w, "# HELP maintainer_model_tokens_per_second Last reported inference throughput.\n# TYPE maintainer_model_tokens_per_second gauge")
			fmt.Fprintf(w, "maintainer_model_tokens_per_second{kind=\"prompt\"} %g\nmaintainer_model_tokens_per_second{kind=\"decode\"} %g\n", model.PromptTokensSecond, model.DecodeTokensSecond)
		}
	}
}

func (s *Server) systemStatus(w http.ResponseWriter, r *http.Request) {
	memoryStatus := "sqlite"
	if s.memoryIndex != nil {
		memoryStatus = "openviking"
		if s.profile == "mock" {
			memoryStatus = "fake-index"
		}
	}
	modelStatus := "unavailable"
	if s.modelManager != nil {
		if current, err := s.modelManager.Status(r.Context()); err == nil {
			modelStatus = current.State
		}
	}
	var disk syscall.Statfs_t
	_ = syscall.Statfs("/", &disk)
	var system syscall.Sysinfo_t
	_ = syscall.Sysinfo(&system)
	memoryUnit := uint64(system.Unit)
	if memoryUnit == 0 {
		memoryUnit = 1
	}
	physicalCores, smtEnabled, numaNodes := hostTopology()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "healthy", "profile": s.profile, "version": s.version,
		"uptime_seconds": int64(time.Since(s.started).Seconds()),
		"host": map[string]any{
			"logical_cpus":           runtime.NumCPU(),
			"physical_cores":         physicalCores,
			"smt_enabled":            smtEnabled,
			"numa_nodes":             numaNodes,
			"memory_total_bytes":     uint64(system.Totalram) * memoryUnit,
			"memory_available_bytes": uint64(system.Freeram+system.Bufferram) * memoryUnit,
			"architecture":           runtime.GOARCH,
			"operating_system":       runtime.GOOS,
			"disk_total_bytes":       int64(disk.Blocks) * int64(disk.Bsize),
			"disk_available_bytes":   int64(disk.Bavail) * int64(disk.Bsize),
			"benchmark_guidance":     "Benchmark physical-core and SMT thread counts; on multi-node hosts compare NUMA local and interleave profiles before selecting a manifest.",
		},
		"components": map[string]string{
			"controller": "healthy", "storage": "healthy",
			"runner": s.profile, "model": modelStatus, "memory": memoryStatus, "git": s.profile,
		},
	})
}

func hostTopology() (int, bool, string) {
	payload, _ := os.ReadFile("/proc/cpuinfo")
	cores := map[string]struct{}{}
	physicalID, coreID := "0", ""
	for _, line := range strings.Split(string(payload), "\n") {
		if strings.HasPrefix(line, "physical id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				physicalID = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "core id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				coreID = strings.TrimSpace(parts[1])
			}
		}
		if line == "" && coreID != "" {
			cores[physicalID+":"+coreID] = struct{}{}
			coreID = ""
		}
	}
	physical := len(cores)
	if physical == 0 {
		physical = runtime.NumCPU()
	}
	numaPayload, err := os.ReadFile("/sys/devices/system/node/online")
	numa := "unknown"
	if err == nil {
		numa = strings.TrimSpace(string(numaPayload))
	}
	return physical, runtime.NumCPU() > physical, numa
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
	if s.gitOperator != nil {
		if err := s.gitOperator.Register(r.Context(), gitbridge.Registration{ProjectID: request.ID, Provider: request.Provider, Repository: request.Repository, DefaultBranch: request.DefaultBranch, LocalRemoteName: request.LocalRemoteName}); err != nil {
			writeError(w, http.StatusBadGateway, "git_registration_failed", "the Git bridge rejected the project registration")
			return
		}
	}
	project, err := s.store.UpsertProject(r.Context(), request, actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) disableProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.DisableProject(r.Context(), r.PathValue("projectID"), actorID(r))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) syncProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		s.storageError(w, r, err)
		return
	}
	if s.gitOperator == nil {
		writeError(w, http.StatusServiceUnavailable, "git_bridge_unavailable", "repository synchronization is unavailable")
		return
	}
	result, err := s.gitOperator.Sync(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "git_sync_failed", "the repository could not be synchronized")
		return
	}
	details, _ := json.Marshal(result)
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "project.sync", TargetType: "project", TargetID: projectID, Details: details})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) projectDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.gitOperator == nil {
		writeError(w, http.StatusServiceUnavailable, "git_bridge_unavailable", "repository diagnostics are unavailable")
		return
	}
	result, err := s.gitOperator.RepositoryDiagnostics(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "git_diagnostics_failed", "repository diagnostics failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
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
	response := map[string]any{
		"job": job, "transitions": transitions, "findings": findings, "approvals": approvals, "phases": phases,
	}
	configurationSnapshot, err := s.store.GetJobConfigSnapshot(r.Context(), job.ID)
	if err == nil {
		response["configuration_snapshot"] = configurationSnapshot
	} else if !errors.Is(err, storage.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateCancelled, "operator cancelled job")
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateQueued, "operator retried job")
}

func (s *Server) requestVerification(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	phases, err := s.store.ListPhaseRecords(r.Context(), job.ID, 500)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	details, _ := json.Marshal(map[string]any{"state": job.State, "result_sha": job.ResultSHA})
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "job.verify.inspect", TargetType: "job", TargetID: job.ID, Details: details})
	verification := []storage.PhaseRecord{}
	for _, phase := range phases {
		if phase.PhaseState == jobs.StateVerifyingTargeted || phase.PhaseState == jobs.StateVerifyingFull || phase.PhaseState == jobs.StateFinalVerification {
			verification = append(verification, phase)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "verification_phases": verification, "resumable": job.State.Resumable()})
}

func (s *Server) requestReview(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	request, err := s.store.CreateApprovalRequest(r.Context(), job.ID, "review", actorID(r), "human review requested through the operator API")
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, request)
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	if s.modelManager == nil {
		writeError(w, http.StatusServiceUnavailable, "model_supervisor_unavailable", "model supervision is unavailable")
		return
	}
	profiles, err := s.modelManager.Profiles(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "model_supervisor_failed", "model profiles could not be listed")
		return
	}
	status, _ := s.modelManager.Status(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": profiles, "status": status})
}

func (s *Server) modelStatus(w http.ResponseWriter, r *http.Request) {
	if s.modelManager == nil {
		writeError(w, http.StatusServiceUnavailable, "model_supervisor_unavailable", "model supervision is unavailable")
		return
	}
	status, err := s.modelManager.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "model_supervisor_failed", "model status could not be read")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) benchmarkModel(w http.ResponseWriter, r *http.Request) {
	if s.modelManager == nil {
		writeError(w, http.StatusServiceUnavailable, "model_supervisor_unavailable", "model supervision is unavailable")
		return
	}
	result, err := s.modelManager.SmokeTest(r.Context(), r.PathValue("profileID"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "model_benchmark_failed", "the allow-listed model benchmark failed")
		return
	}
	details, _ := json.Marshal(result)
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "model.benchmark", TargetType: "model_profile", TargetID: result.ProfileID, Details: details})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) loadModel(w http.ResponseWriter, r *http.Request) {
	if s.modelManager == nil {
		writeError(w, http.StatusServiceUnavailable, "model_supervisor_unavailable", "model supervision is unavailable")
		return
	}
	profileID := r.PathValue("profileID")
	status, err := s.modelManager.Load(r.Context(), profileID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "model_load_failed", "the allow-listed model profile could not be loaded")
		return
	}
	details, _ := json.Marshal(status)
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "model.load", TargetType: "model_profile", TargetID: profileID, Details: details})
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) unloadModel(w http.ResponseWriter, r *http.Request) {
	if s.modelManager == nil {
		writeError(w, http.StatusServiceUnavailable, "model_supervisor_unavailable", "model supervision is unavailable")
		return
	}
	previous, _ := s.modelManager.Status(r.Context())
	if err := s.modelManager.Unload(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "model_unload_failed", "the loaded model could not be stopped")
		return
	}
	details, _ := json.Marshal(previous)
	_, _ = s.store.AppendAudit(r.Context(), audit.AppendRequest{ActorID: actorID(r), ActorRole: actorRole(r), Action: "model.unload", TargetType: "model_profile", TargetID: previous.ProfileID, Details: details})
	writeJSON(w, http.StatusOK, models.Status{State: "unloaded"})
}

func (s *Server) findingAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Rationale       string `json:"rationale"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Rationale = strings.TrimSpace(request.Rationale)
	if request.Rationale == "" || request.ExpectedVersion < 1 {
		writeError(w, http.StatusBadRequest, "invalid_finding_action", "a rationale and expected_version are required")
		return
	}
	if r.PathValue("action") == "escalate" {
		rationale := "finding " + r.PathValue("findingID") + " escalation: " + request.Rationale
		review, err := s.store.CreateApprovalRequest(r.Context(), r.PathValue("jobID"), "review", actorID(r), rationale)
		if err != nil {
			s.storageError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, review)
		return
	}
	targets := map[string]findings.Status{
		"dispute": findings.StatusDisputed,
		"accept":  findings.StatusAccepted,
		"waive":   findings.StatusHumanWaived,
	}
	target, ok := targets[r.PathValue("action")]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_finding_action", "the finding action is not allowed")
		return
	}
	principal, _ := principalFromRequest(r)
	record, err := s.store.TransitionFinding(r.Context(), r.PathValue("jobID"), r.PathValue("findingID"), findings.TransitionRequest{
		To: target, ActorID: actorID(r), ActorRole: actorRole(r), Rationale: request.Rationale,
		Reauthenticated: principal.RecentlyReauthenticated(time.Now().UTC()), ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
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
	principal, ok := principalFromRequest(r)
	if s.auth != nil && (!ok || !principal.RecentlyReauthenticated(time.Now().UTC())) {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "configuration rollback requires reauthentication within five minutes")
		return
	}
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
