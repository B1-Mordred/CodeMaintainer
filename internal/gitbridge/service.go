package gitbridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const maxRequestBytes = int64(1 << 20)

type Backend interface {
	Register(context.Context, Registration) error
	Sync(context.Context, string) (SyncResult, error)
	CreateWorktree(context.Context, WorktreeRequest) (WorktreeResult, error)
	Commit(context.Context, CommitRequest) (CommitResult, error)
	Diff(context.Context, DiffRequest) (DiffResult, error)
	Publish(context.Context, PublishRequest) (Publication, error)
}

func NewService(backend Backend, token []byte, logger *slog.Logger) (http.Handler, error) {
	if backend == nil || len(token) < 32 || logger == nil {
		return nil, errors.New("Git bridge backend, private token, and logger are required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/projects/register", func(w http.ResponseWriter, r *http.Request) {
		var request Registration
		if !decode(w, r, &request) {
			return
		}
		if err := backend.Register(r.Context(), request); err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
	})
	mux.HandleFunc("POST /v1/projects/{projectID}/sync", func(w http.ResponseWriter, r *http.Request) {
		result, err := backend.Sync(r.Context(), r.PathValue("projectID"))
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/worktrees", func(w http.ResponseWriter, r *http.Request) {
		var request WorktreeRequest
		if !decode(w, r, &request) {
			return
		}
		result, err := backend.CreateWorktree(r.Context(), request)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/commits", func(w http.ResponseWriter, r *http.Request) {
		var request CommitRequest
		if !decode(w, r, &request) {
			return
		}
		result, err := backend.Commit(r.Context(), request)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/diffs", func(w http.ResponseWriter, r *http.Request) {
		var request DiffRequest
		if !decode(w, r, &request) {
			return
		}
		result, err := backend.Diff(r.Context(), request)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/publications", func(w http.ResponseWriter, r *http.Request) {
		var request PublishRequest
		if !decode(w, r, &request) {
			return
		}
		result, err := backend.Publish(r.Context(), request)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		logger.InfoContext(r.Context(), "draft publication created", "project_id", request.ProjectID, "job_id", request.JobID, "branch", result.Branch)
		writeJSON(w, http.StatusOK, result)
	})
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}
		provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), token) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		mux.ServeHTTP(w, r)
	})
	return authenticated, nil
}

func decode(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request violates the bounded contract"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain one JSON document"})
		return false
	}
	return true
}

func writeBackendError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrConflict), errors.Is(err, ErrUpstreamMoved):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
