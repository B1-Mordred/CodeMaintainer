package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxRunnerResponseBytes = 2 << 20

type UnixClient struct {
	http  *http.Client
	token string
}

type RemoteError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("runnerd returned %d %s: %s", e.StatusCode, e.Code, e.Message)
}

func NewUnixClient(socketPath string, token []byte) (*UnixClient, error) {
	if socketPath == "" || len(token) < 32 {
		return nil, ErrInvalidRequest
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second,
	}
	return &UnixClient{http: &http.Client{Transport: transport, Timeout: 30 * time.Second}, token: string(token)}, nil
}

func (c *UnixClient) Start(ctx context.Context, request JobRequest) (RunID, error) {
	var response struct {
		RunID RunID `json:"run_id"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/runs", request, &response); err != nil {
		return "", err
	}
	if response.RunID == "" {
		return "", errors.New("runnerd returned an empty run identifier")
	}
	return response.RunID, nil
}

func (c *UnixClient) Inspect(ctx context.Context, runID RunID) (Status, error) {
	var response Status
	err := c.request(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID)), nil, &response)
	return response, err
}

func (c *UnixClient) Logs(ctx context.Context, runID RunID, cursor int64, limit int) (LogChunk, error) {
	var response LogChunk
	query := url.Values{}
	query.Set("cursor", strconv.FormatInt(cursor, 10))
	query.Set("limit", strconv.Itoa(limit))
	err := c.request(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID))+"/logs?"+query.Encode(), nil, &response)
	return response, err
}

func (c *UnixClient) Stop(ctx context.Context, runID RunID) error {
	return c.request(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(string(runID))+"/stop", nil, nil)
}

func (c *UnixClient) Artifacts(ctx context.Context, runID RunID) ([]Artifact, error) {
	var response struct {
		Items []Artifact `json:"items"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID))+"/artifacts", nil, &response)
	if response.Items == nil {
		response.Items = []Artifact{}
	}
	return response.Items, err
}

func (c *UnixClient) request(ctx context.Context, method, path string, body, destination any) error {
	var source io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		source = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://runnerd"+path, source)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call runnerd: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxRunnerResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read runnerd response: %w", err)
	}
	if len(payload) > maxRunnerResponseBytes {
		return errors.New("runnerd response exceeded the client limit")
	}
	if response.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &envelope)
		return &RemoteError{
			StatusCode: response.StatusCode, Code: strings.TrimSpace(envelope.Error.Code),
			Message: strings.TrimSpace(envelope.Error.Message),
		}
	}
	if destination == nil {
		return nil
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("decode runnerd response: %w", err)
	}
	return nil
}
