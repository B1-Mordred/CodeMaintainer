package api

import (
	"context"
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

	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
	"github.com/local-code-maintainer/appliance/internal/ui"
)

const maxRequestBody = 1 << 20

type Server struct {
	store   storage.Store
	logger  *slog.Logger
	profile string
	started time.Time
	handler http.Handler
}

func NewServer(store storage.Store, logger *slog.Logger, profile string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: store, logger: logger, profile: profile, started: time.Now().UTC()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/system/status", s.systemStatus)
	mux.HandleFunc("GET /api/v1/workflow/states", s.workflowStates)
	mux.HandleFunc("GET /api/v1/jobs", s.listJobs)
	mux.HandleFunc("POST /api/v1/jobs", s.createJob)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}", s.getJob)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}/events", s.jobEvents)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/cancel", s.cancelJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/actions/retry", s.retryJob)
	mux.HandleFunc("GET /api/v1/config", s.getConfig)
	mux.HandleFunc("GET /api/v1/config/revisions", s.listConfigRevisions)
	mux.HandleFunc("GET /api/v1/audit", s.listAudit)
	mux.Handle("GET /", s.staticHandler())
	s.handler = s.middleware(mux)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "healthy", "profile": s.profile,
		"uptime_seconds": int64(time.Since(s.started).Seconds()),
		"components": map[string]string{
			"controller": "healthy", "storage": "healthy",
			"runner": "fake", "model": "unloaded", "memory": "fake", "git": "fake",
		},
	})
}

func (s *Server) workflowStates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs.AllStates()})
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
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "transitions": transitions})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateCancelled, "operator cancelled job")
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	s.transitionAction(w, r, jobs.StateQueued, "operator retried job")
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
	if value := strings.TrimSpace(r.Header.Get("X-Maintainer-Actor")); value != "" && len(value) <= 128 {
		return value
	}
	return "local-operator"
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content_type", "Content-Type must be application/json")
		return errors.New("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, storage.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "resource changed; refresh and retry")
	case errors.Is(err, storage.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
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
