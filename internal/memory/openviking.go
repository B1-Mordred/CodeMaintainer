package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxOpenVikingResponse = 2 << 20

type OpenVikingIndex struct {
	baseURL    *url.URL
	apiKeyFile string
	account    string
	user       string
	client     *http.Client
}

func NewOpenVikingIndex(rawURL, apiKeyFile, account, user string) (*OpenVikingIndex, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("OpenViking base URL must be an origin without credentials, query, or path: %w", ErrInvalid)
	}
	if apiKeyFile != "" && !filepath.IsAbs(apiKeyFile) {
		return nil, fmt.Errorf("OpenViking API key file must be absolute: %w", ErrInvalid)
	}
	parsed.Path = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &OpenVikingIndex{
		baseURL: parsed, apiKeyFile: apiKeyFile, account: strings.TrimSpace(account), user: strings.TrimSpace(user),
		client: &http.Client{
			Timeout:       30 * time.Second,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OpenViking redirects are disabled") },
		},
	}, nil
}

func (o *OpenVikingIndex) Health(ctx context.Context) error {
	var response struct {
		Status  string `json:"status"`
		Healthy bool   `json:"healthy"`
	}
	if err := o.request(ctx, http.MethodGet, "/health", nil, nil, &response); err != nil {
		return err
	}
	if response.Status != "ok" || !response.Healthy {
		return errors.New("OpenViking health response was not healthy")
	}
	return nil
}

func (o *OpenVikingIndex) Upsert(ctx context.Context, record Record) error {
	if !record.Scope.Valid() || !ValidID(record.ID) || record.Status != StatusCanonical || record.Namespace != record.Scope.Namespace() {
		return ErrInvalid
	}
	document, err := json.Marshal(map[string]any{
		"schema_version": 1, "memory_id": record.ID, "repository": record.Scope.Owner + "/" + record.Scope.Repository,
		"base_commit": record.BaseCommit, "merged_commit": record.MergedCommit, "source_uri": record.SourceURI,
		"verified": record.Verified, "affected_paths": record.AffectedPaths, "content_hash": record.ContentHash,
		"content": record.Content,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{"uri": indexRecordURI(record.Scope, record.ID), "content": string(document), "mode": "create", "wait": false}
	err = o.request(ctx, http.MethodPost, "/api/v1/content/write", nil, payload, nil)
	var statusError *openVikingStatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusConflict {
		payload["mode"] = "replace"
		return o.request(ctx, http.MethodPost, "/api/v1/content/write", nil, payload, nil)
	}
	return err
}

func (o *OpenVikingIndex) Forget(ctx context.Context, scope ProjectScope, id string) error {
	if !scope.Valid() || !ValidID(id) {
		return ErrInvalid
	}
	query := url.Values{"uri": []string{indexRecordURI(scope, id)}, "recursive": []string{"false"}}
	err := o.request(ctx, http.MethodDelete, "/api/v1/fs", query, nil, nil)
	var statusError *openVikingStatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

func (o *OpenVikingIndex) Find(ctx context.Context, scope ProjectScope, query string, limit int) ([]IndexMatch, error) {
	query = strings.TrimSpace(query)
	if !scope.Valid() || query == "" || len(query) > 4096 {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var response struct {
		Result struct {
			Resources []struct {
				URI      string  `json:"uri"`
				Score    float64 `json:"score"`
				Abstract string  `json:"abstract"`
			} `json:"resources"`
		} `json:"result"`
	}
	payload := map[string]any{
		"query": query, "target_uri": scope.Namespace() + "records/", "node_limit": limit,
		"include_provenance": true,
	}
	if err := o.request(ctx, http.MethodPost, "/api/v1/search/find", nil, payload, &response); err != nil {
		return nil, err
	}
	prefix := scope.Namespace() + "records/"
	matches := make([]IndexMatch, 0, len(response.Result.Resources))
	seen := make(map[string]bool)
	for _, result := range response.Result.Resources {
		if !strings.HasPrefix(result.URI, prefix) || !strings.HasSuffix(result.URI, ".md") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(result.URI, prefix), ".md")
		if strings.Contains(name, "/") || !ValidID(name) || seen[name] {
			continue
		}
		seen[name] = true
		matches = append(matches, IndexMatch{RecordID: name, URI: result.URI, Score: result.Score, Abstract: result.Abstract})
		if len(matches) == limit {
			break
		}
	}
	return matches, nil
}

func (o *OpenVikingIndex) Reindex(ctx context.Context, scope ProjectScope, mode string) error {
	if !scope.Valid() || (mode != "vectors_only" && mode != "semantic_and_vectors") {
		return ErrInvalid
	}
	return o.request(ctx, http.MethodPost, "/api/v1/content/reindex", nil, map[string]any{
		"uri": scope.Namespace(), "mode": mode, "wait": false,
	}, nil)
}

type openVikingStatusError struct {
	StatusCode int
	Code       string
}

func (e *openVikingStatusError) Error() string {
	return fmt.Sprintf("OpenViking request failed with status %d (%s)", e.StatusCode, e.Code)
}

func (o *OpenVikingIndex) request(ctx context.Context, method, route string, query url.Values, body any, destination any) error {
	endpoint := *o.baseURL
	endpoint.Path = route
	endpoint.RawQuery = query.Encode()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	key, err := o.apiKey()
	if err != nil {
		return err
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if o.account != "" {
		request.Header.Set("X-OpenViking-Account", o.account)
	}
	if o.user != "" {
		request.Header.Set("X-OpenViking-User", o.user)
	}
	response, err := o.client.Do(request)
	if err != nil {
		return fmt.Errorf("call OpenViking: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxOpenVikingResponse+1))
	if err != nil {
		return fmt.Errorf("read OpenViking response: %w", err)
	}
	if len(payload) > maxOpenVikingResponse {
		return errors.New("OpenViking response exceeded the configured limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &envelope)
		return &openVikingStatusError{StatusCode: response.StatusCode, Code: envelope.Error.Code}
	}
	if destination != nil && len(payload) != 0 {
		if err := json.Unmarshal(payload, destination); err != nil {
			return fmt.Errorf("decode OpenViking response: %w", err)
		}
	}
	return nil
}

func (o *OpenVikingIndex) apiKey() (string, error) {
	if o.apiKeyFile == "" {
		return "", nil
	}
	payload, err := os.ReadFile(o.apiKeyFile)
	if err != nil {
		return "", fmt.Errorf("read OpenViking API key file: %w", err)
	}
	if len(payload) > 4096 {
		return "", errors.New("OpenViking API key file exceeds 4096 bytes")
	}
	key := strings.TrimSpace(string(payload))
	if key == "" || strings.IndexFunc(key, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return "", errors.New("OpenViking API key file is empty or malformed")
	}
	return key, nil
}
