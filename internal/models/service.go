package models

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const maxModelRequestBytes = int64(16 << 10)

func NewService(manager Manager, token string, logger *slog.Logger) (http.Handler, error) {
	if manager == nil || len(token) < 32 || logger == nil {
		return nil, errors.New("model manager, private token, and logger are required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeModelJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	authenticated := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				writeModelJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			handler(w, r)
		}
	}
	mux.HandleFunc("GET /v1/profiles", authenticated(func(w http.ResponseWriter, r *http.Request) {
		profiles, err := manager.Profiles(r.Context())
		if err != nil {
			writeModelError(w, err)
			return
		}
		writeModelJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	}))
	mux.HandleFunc("GET /v1/status", authenticated(func(w http.ResponseWriter, r *http.Request) {
		status, err := manager.Status(r.Context())
		if err != nil {
			writeModelError(w, err)
			return
		}
		writeModelJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("POST /v1/load", authenticated(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ProfileID string `json:"profile_id"`
		}
		if err := decodeModelRequest(w, r, &request); err != nil || !manifestID.MatchString(request.ProfileID) {
			writeModelJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid load request"})
			return
		}
		status, err := manager.Load(r.Context(), request.ProfileID)
		if err != nil {
			writeModelError(w, err)
			return
		}
		logger.Info("model profile loaded", "profile_id", request.ProfileID)
		writeModelJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("POST /v1/unload", authenticated(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			writeModelJSON(w, http.StatusBadRequest, map[string]string{"error": "unload request body must be empty"})
			return
		}
		if err := manager.Unload(r.Context()); err != nil {
			writeModelError(w, err)
			return
		}
		logger.Info("model unloaded")
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /v1/smoke", authenticated(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ProfileID string `json:"profile_id"`
		}
		if err := decodeModelRequest(w, r, &request); err != nil || !manifestID.MatchString(request.ProfileID) {
			writeModelJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid smoke request"})
			return
		}
		result, err := manager.SmokeTest(r.Context(), request.ProfileID)
		if err != nil {
			writeModelError(w, err)
			return
		}
		writeModelJSON(w, http.StatusOK, result)
	}))
	return mux, nil
}

func decodeModelRequest(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxModelRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request contains trailing data")
	}
	return nil
}

func writeModelError(w http.ResponseWriter, err error) {
	writeModelJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
}

func writeModelJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
