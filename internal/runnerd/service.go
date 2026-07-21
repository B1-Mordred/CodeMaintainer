package runnerd

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
)

const maxRequestBytes = 64 << 10

type Service struct {
	policy   *Policy
	executor Executor
	token    []byte
	logger   *slog.Logger
	handler  http.Handler
}

func NewService(policy *Policy, executor Executor, token []byte, logger *slog.Logger) (*Service, error) {
	if policy == nil || executor == nil || len(token) < 32 {
		return nil, errors.New("runnerd requires a policy, executor, and at least 32 bytes of authentication material")
	}
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{policy: policy, executor: executor, token: append([]byte(nil), token...), logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", service.health)
	mux.HandleFunc("POST /v1/runs", service.start)
	mux.HandleFunc("GET /v1/runs/{runID}", service.inspect)
	mux.HandleFunc("GET /v1/runs/{runID}/logs", service.logs)
	mux.HandleFunc("POST /v1/runs/{runID}/stop", service.stop)
	mux.HandleFunc("GET /v1/runs/{runID}/artifacts", service.artifacts)
	service.handler = service.authenticate(mux)
	return service, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func (s *Service) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		provided, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !found || len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), s.token) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "authentication_required", "runnerd authentication failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var request runners.JobRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	runID, err := runIDFor(request)
	if err != nil {
		s.internalError(w, err)
		return
	}
	spec, err := s.policy.Resolve(request, runID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "policy_denied", err.Error())
		return
	}
	if err := s.executor.Start(r.Context(), spec); err != nil {
		s.internalError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/runs/"+string(runID))
	writeJSON(w, http.StatusCreated, map[string]runners.RunID{"run_id": runID})
}

func (s *Service) inspect(w http.ResponseWriter, r *http.Request) {
	status, err := s.executor.Inspect(r.Context(), runners.RunID(r.PathValue("runID")))
	s.writeExecutorResult(w, status, err)
}

func (s *Service) logs(w http.ResponseWriter, r *http.Request) {
	cursor, err := parseBoundedInteger(r.URL.Query().Get("cursor"), 0, 1<<62, 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	limit, err := parseBoundedInteger(r.URL.Query().Get("limit"), 1, 64<<10, 64<<10)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	chunk, executeErr := s.executor.Logs(r.Context(), runners.RunID(r.PathValue("runID")), cursor, int(limit))
	s.writeExecutorResult(w, chunk, executeErr)
}

func (s *Service) stop(w http.ResponseWriter, r *http.Request) {
	err := s.executor.Stop(r.Context(), runners.RunID(r.PathValue("runID")))
	if errors.Is(err, ErrRunNotFound) {
		writeError(w, http.StatusNotFound, "run_not_found", "run was not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Service) artifacts(w http.ResponseWriter, r *http.Request) {
	items, err := s.executor.Artifacts(r.Context(), runners.RunID(r.PathValue("runID")))
	s.writeExecutorResult(w, map[string]any{"items": items}, err)
}

func (s *Service) writeExecutorResult(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, ErrRunNotFound) {
		writeError(w, http.StatusNotFound, "run_not_found", "run was not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Service) internalError(w http.ResponseWriter, err error) {
	s.logger.Error("runnerd request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "runnerd could not complete the operation")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "Content-Type must be application/json")
		return errors.New("JSON content type required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request must match the bounded runner contract")
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "request must contain exactly one JSON document")
		return errors.New("multiple JSON documents")
	}
	return nil
}

func runIDFor(request runners.JobRequest) (runners.RunID, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode bounded run identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return runners.RunID("run_" + hex.EncodeToString(digest[:16])), nil
}

func parseBoundedInteger(value string, minimum, maximum, fallback int64) (int64, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("value must be between %d and %d", minimum, maximum)
	}
	return parsed, nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
