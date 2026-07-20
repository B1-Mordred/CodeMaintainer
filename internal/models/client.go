package models

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ControlClient struct {
	endpoint string
	token    string
	http     *http.Client
}

func NewControlClient(endpoint string, token []byte) (*ControlClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 {
		return nil, errors.New("invalid model control client configuration")
	}
	return &ControlClient{
		endpoint: strings.TrimSuffix(endpoint, "/"), token: string(token),
		http: &http.Client{Timeout: 4 * time.Minute},
	}, nil
}

func (c *ControlClient) Profiles(ctx context.Context) ([]Profile, error) {
	var envelope struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/profiles", nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Profiles, nil
}

func (c *ControlClient) Load(ctx context.Context, id string) (Status, error) {
	var status Status
	err := c.call(ctx, http.MethodPost, "/v1/load", map[string]string{"profile_id": id}, &status)
	return status, err
}

func (c *ControlClient) Unload(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/v1/unload", nil, nil)
}

func (c *ControlClient) Status(ctx context.Context) (Status, error) {
	var status Status
	err := c.call(ctx, http.MethodGet, "/v1/status", nil, &status)
	return status, err
}

func (c *ControlClient) SmokeTest(ctx context.Context, id string) (SmokeResult, error) {
	var result SmokeResult
	err := c.call(ctx, http.MethodPost, "/v1/smoke", map[string]string{"profile_id": id}, &result)
	return result, err
}

func (c *ControlClient) call(ctx context.Context, method, path string, body, destination any) error {
	var source io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		source = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, source)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call model supervisor: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(payload) > 2<<20 {
		return errors.New("model supervisor response is unreadable or oversized")
	}
	if response.StatusCode >= 300 {
		var envelope map[string]string
		_ = json.Unmarshal(payload, &envelope)
		return fmt.Errorf("model supervisor returned %d: %s", response.StatusCode, envelope["error"])
	}
	if destination == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.Unmarshal(payload, destination)
}

var _ Manager = (*ControlClient)(nil)
