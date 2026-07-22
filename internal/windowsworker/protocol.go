package windowsworker

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxProtocolBytes = 1 << 20

type ProtocolHandler struct {
	token    []byte
	profiles map[string]Profile
	adapter  Adapter
}

func NewProtocolHandler(token []byte, profiles []Profile, adapter Adapter) (http.Handler, error) {
	if len(token) < 32 || adapter == nil || len(profiles) == 0 || len(profiles) > 100 {
		return nil, errors.New("Windows worker protocol requires a private token, profiles, and adapter")
	}
	server := &ProtocolHandler{token: append([]byte(nil), token...), profiles: map[string]Profile{}, adapter: adapter}
	for _, profile := range profiles {
		if validateProfile(profile) != nil || !profile.Enabled {
			return nil, errors.New("Windows worker protocol profile is invalid or disabled")
		}
		server.profiles[profile.ID] = profile
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeProtocolJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/probe", server.probe)
	mux.HandleFunc("POST /v1/run", server.run)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if len(provided) != len(server.token) || subtle.ConstantTimeCompare([]byte(provided), server.token) != 1 {
				writeProtocolJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func (s *ProtocolHandler) probe(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProfileID string `json:"profile_id"`
	}
	if !decodeProtocol(w, r, &request) || !safeID.MatchString(request.ProfileID) {
		return
	}
	profile, ok := s.profiles[request.ProfileID]
	if !ok {
		writeProtocolJSON(w, http.StatusNotFound, map[string]string{"error": "profile_not_found"})
		return
	}
	result, err := s.adapter.ProbeWindowsWorker(r.Context(), profile)
	if err != nil {
		writeProtocolJSON(w, http.StatusBadGateway, map[string]string{"error": "probe_failed"})
		return
	}
	writeProtocolJSON(w, http.StatusOK, result)
}

func (s *ProtocolHandler) run(w http.ResponseWriter, r *http.Request) {
	var request RunRequest
	if !decodeProtocol(w, r, &request) {
		return
	}
	profile, ok := s.profiles[request.ProfileID]
	if !ok {
		writeProtocolJSON(w, http.StatusNotFound, map[string]string{"error": "profile_not_found"})
		return
	}
	if !contains(profile.AllowedJobTypes, request.JobType) {
		writeProtocolJSON(w, http.StatusForbidden, map[string]string{"error": "job_type_not_allowed"})
		return
	}
	// Authentication identifies the controller. OperatorGated is already
	// bound to its durable controller approval before crossing this boundary.
	request.ActorRole = "administrator"
	if err := validateRun(profile, request); err != nil {
		writeProtocolJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_immutable_request"})
		return
	}
	result, err := s.adapter.RunWindowsJob(r.Context(), profile, request)
	if err != nil || validateResult(profile, request, result) != nil {
		writeProtocolJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid_worker_result"})
		return
	}
	writeProtocolJSON(w, http.StatusOK, result)
}

type Client struct {
	base  *url.URL
	token []byte
	http  *http.Client
}

func NewClient(endpoint string, token []byte, client *http.Client) (*Client, error) {
	if len(token) < 32 {
		return nil, errors.New("Windows worker client token is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Windows worker protocol endpoint")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || strings.HasPrefix(parsed.Hostname(), "127."))) {
		return nil, errors.New("Windows worker protocol requires HTTPS except loopback tests")
	}
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{base: parsed, token: append([]byte(nil), token...), http: client}, nil
}

func (c *Client) ProbeWindowsWorker(ctx context.Context, profile Profile) (Probe, error) {
	var result Probe
	err := c.call(ctx, "/v1/probe", map[string]string{"profile_id": profile.ID}, &result)
	return result, err
}

func (c *Client) RunWindowsJob(ctx context.Context, _ Profile, request RunRequest) (Result, error) {
	request.ActorID, request.ActorRole = "", ""
	var result Result
	err := c.call(ctx, "/v1/run", request, &result)
	return result, err
}

func (c *Client) call(ctx context.Context, path string, body, result any) error {
	payload, err := json.Marshal(body)
	if err != nil || len(payload) > maxProtocolBytes {
		return errors.New("invalid Windows worker protocol request")
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+string(c.token))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("Windows worker protocol request failed: %w", err)
	}
	defer response.Body.Close()
	responsePayload, readErr := io.ReadAll(io.LimitReader(response.Body, maxProtocolBytes+1))
	if readErr != nil || len(responsePayload) > maxProtocolBytes {
		return errors.New("Windows worker protocol response is unreadable or oversized")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Windows worker protocol returned status %d", response.StatusCode)
	}
	if json.Unmarshal(responsePayload, result) != nil {
		return errors.New("Windows worker protocol response is malformed")
	}
	return nil
}

func decodeProtocol(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxProtocolBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		writeProtocolJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeProtocolJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func writeProtocolJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
