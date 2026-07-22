package gitbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/forges"
)

const forgeCursorPrefix = "forge-v1:"

type forgeCursor struct {
	BaseSHA  string `json:"base_sha"`
	Endpoint int    `json:"endpoint"`
	Page     int    `json:"page"`
}

type inventoryResult struct {
	Objects            []forges.Object
	Endpoint           int
	Page               int
	Done               bool
	RateLimitRemaining int
	RetryAfterSeconds  int
	Unsupported        []string
}

type inventoryEndpoint struct {
	kind string
	path string
}

type pageResponse struct {
	payload            []byte
	status             int
	nextPage           int
	rateLimitRemaining int
	retryAfterSeconds  int
}

var githubInventoryEndpoints = []inventoryEndpoint{
	{kind: "issue", path: "/issues"},
	{kind: "change_request", path: "/pulls"},
	{kind: "discussion", path: "/discussions"},
	{kind: "pipeline", path: "/actions/runs"},
	{kind: "artifact", path: "/actions/artifacts"},
	{kind: "branch", path: "/branches"},
	{kind: "tag", path: "/tags"},
	{kind: "release", path: "/releases"},
}

var gitlabInventoryEndpoints = []inventoryEndpoint{
	{kind: "issue", path: "/issues"},
	{kind: "change_request", path: "/merge_requests"},
	{kind: "pipeline", path: "/pipelines"},
	{kind: "job", path: "/jobs"},
	{kind: "branch", path: "/repository/branches"},
	{kind: "tag", path: "/repository/tags"},
	{kind: "release", path: "/releases"},
}

func encodeForgeCursor(cursor forgeCursor) string {
	payload, _ := json.Marshal(cursor)
	return forgeCursorPrefix + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeForgeCursor(value string) (forgeCursor, bool) {
	if !strings.HasPrefix(value, forgeCursorPrefix) || len(value) > 4096 {
		return forgeCursor{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, forgeCursorPrefix))
	if err != nil {
		return forgeCursor{}, false
	}
	var cursor forgeCursor
	if json.Unmarshal(payload, &cursor) != nil || !commit.MatchString(cursor.BaseSHA) || cursor.Endpoint < 0 || cursor.Page < 1 {
		return forgeCursor{}, false
	}
	return cursor, true
}

func (g *GitHubAPI) inventory(ctx context.Context, projectID string, endpointIndex, page, limit int) (inventoryResult, error) {
	return collectInventory(ctx, "github", projectID, githubInventoryEndpoints, endpointIndex, page, limit, func(ctx context.Context, path string, page, perPage int) (pageResponse, error) {
		return g.getInventoryPage(ctx, path, page, perPage)
	})
}

func (g *GitLabAPI) inventory(ctx context.Context, projectID string, endpointIndex, page, limit int) (inventoryResult, error) {
	result, err := collectInventory(ctx, "gitlab", projectID, gitlabInventoryEndpoints, endpointIndex, page, limit, func(ctx context.Context, path string, page, perPage int) (pageResponse, error) {
		return g.getInventoryPage(ctx, path, page, perPage)
	})
	if err != nil {
		return result, err
	}
	// GitLab exposes artifact metadata on jobs rather than through a bounded
	// project-wide artifact listing. Preserve that evidence as a distinct,
	// normalized object without attempting to download untrusted archives.
	for _, item := range append([]forges.Object(nil), result.Objects...) {
		if item.Kind != "job" || len(result.Objects) >= limit {
			continue
		}
		var raw map[string]any
		if json.Unmarshal(item.ProviderMetadata, &raw) != nil {
			continue
		}
		artifact, ok := raw["artifacts_file"].(map[string]any)
		if !ok || stringValue(artifact["filename"]) == "" {
			continue
		}
		result.Objects = append(result.Objects, forges.Object{
			ProjectID: projectID, Provider: "gitlab", Kind: "artifact",
			ExternalID: item.ExternalID + ":" + stringValue(artifact["filename"]),
			Title:      stringValue(artifact["filename"]), ParentID: item.ExternalID,
			ProviderMetadata: item.ProviderMetadata,
		})
	}
	return result, nil
}

func collectInventory(
	ctx context.Context,
	provider, projectID string,
	endpoints []inventoryEndpoint,
	endpointIndex, page, limit int,
	get func(context.Context, string, int, int) (pageResponse, error),
) (inventoryResult, error) {
	result := inventoryResult{Endpoint: endpointIndex, Page: page, RateLimitRemaining: -1}
	if endpointIndex < 0 || endpointIndex > len(endpoints) || page < 1 || limit < 1 || limit > 500 {
		return result, ErrInvalid
	}
	for index := endpointIndex; index < len(endpoints) && len(result.Objects) < limit; index++ {
		currentPage := 1
		if index == endpointIndex {
			currentPage = page
		}
		remaining := limit - len(result.Objects)
		if remaining > 100 {
			remaining = 100
		}
		response, err := get(ctx, endpoints[index].path, currentPage, remaining)
		if err != nil {
			return result, err
		}
		if response.rateLimitRemaining >= 0 && (result.RateLimitRemaining < 0 || response.rateLimitRemaining < result.RateLimitRemaining) {
			result.RateLimitRemaining = response.rateLimitRemaining
		}
		if response.status == http.StatusForbidden || response.status == http.StatusNotFound {
			result.Unsupported = append(result.Unsupported, endpoints[index].kind+": unavailable for this credential, forge edition, or repository")
			continue
		}
		if response.status == http.StatusTooManyRequests {
			result.Endpoint, result.Page = index, currentPage
			result.RetryAfterSeconds = response.retryAfterSeconds
			if result.RateLimitRemaining < 0 {
				result.RateLimitRemaining = 1
			}
			return result, nil
		}
		objects, err := normalizeInventoryPayload(provider, projectID, endpoints[index].kind, response.payload)
		if err != nil {
			return result, err
		}
		if len(objects) > remaining {
			return result, ErrInvalid
		}
		result.Objects = append(result.Objects, objects...)
		if response.nextPage > currentPage {
			result.Endpoint, result.Page = index, response.nextPage
			if result.RateLimitRemaining < 0 {
				result.RateLimitRemaining = 1
			}
			return result, nil
		}
		result.Endpoint, result.Page = index+1, 1
	}
	result.Done = result.Endpoint >= len(endpoints)
	if result.RateLimitRemaining < 0 {
		result.RateLimitRemaining = 1
	}
	return result, nil
}

func (g *GitHubAPI) getInventoryPage(ctx context.Context, path string, page, perPage int) (pageResponse, error) {
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return pageResponse{}, err
	}
	return doInventoryGET(ctx, g.http, func() (*http.Request, error) {
		endpoint := *g.base
		endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + g.repoPath() + path
		query := endpoint.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(perPage))
		endpoint.RawQuery = query.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if requestErr == nil {
			request.Header.Set("Authorization", "Bearer "+token.Value)
			setGitHubHeaders(request)
		}
		return request, requestErr
	}, "github")
}

func (g *GitLabAPI) getInventoryPage(ctx context.Context, path string, page, perPage int) (pageResponse, error) {
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return pageResponse{}, err
	}
	return doInventoryGET(ctx, g.http, func() (*http.Request, error) {
		endpoint := *g.base
		endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/api/v4/projects/" + g.project + path
		query := endpoint.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(perPage))
		endpoint.RawQuery = query.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if requestErr == nil {
			request.Header.Set("PRIVATE-TOKEN", token)
			request.Header.Set("Accept", "application/json")
		}
		return request, requestErr
	}, "gitlab")
}

func doInventoryGET(ctx context.Context, client *http.Client, makeRequest func() (*http.Request, error), provider string) (pageResponse, error) {
	for attempt := 0; attempt < 3; attempt++ {
		request, err := makeRequest()
		if err != nil {
			return pageResponse{}, ErrInvalid
		}
		response, err := client.Do(request)
		if err != nil {
			return pageResponse{}, fmt.Errorf("%s inventory request failed: %w", provider, err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxGitHubResponseBytes+1))
		response.Body.Close()
		if readErr != nil || int64(len(payload)) > maxGitHubResponseBytes {
			return pageResponse{}, errors.New("forge inventory response is unreadable or oversized")
		}
		if (response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout) && attempt < 2 {
			continue
		}
		result := pageResponse{payload: payload, status: response.StatusCode, rateLimitRemaining: -1}
		if value := response.Header.Get("X-RateLimit-Remaining"); value != "" {
			remaining, parseErr := strconv.Atoi(value)
			if parseErr != nil || remaining < 0 {
				return pageResponse{}, errors.New("forge rate limit response is malformed")
			}
			result.rateLimitRemaining = remaining
		}
		if response.StatusCode == http.StatusTooManyRequests {
			retry, parseErr := strconv.Atoi(response.Header.Get("Retry-After"))
			if parseErr != nil || retry < 1 || retry > 86400 {
				return pageResponse{}, errors.New("forge retry delay is malformed")
			}
			result.retryAfterSeconds = retry
			return result, nil
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
			return result, nil
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return pageResponse{}, fmt.Errorf("%s inventory API returned status %d", provider, response.StatusCode)
		}
		if provider == "github" {
			result.nextPage = githubNextPage(response.Header.Get("Link"))
		} else if value := response.Header.Get("X-Next-Page"); value != "" {
			next, parseErr := strconv.Atoi(value)
			if parseErr != nil || next < 1 {
				return pageResponse{}, errors.New("GitLab pagination response is malformed")
			}
			result.nextPage = next
		}
		return result, nil
	}
	return pageResponse{}, errors.New("forge inventory retries exhausted")
}

func githubNextPage(header string) int {
	for _, link := range strings.Split(header, ",") {
		if !strings.Contains(link, `rel="next"`) {
			continue
		}
		start, end := strings.Index(link, "<"), strings.Index(link, ">")
		if start < 0 || end <= start {
			return 0
		}
		parsed, err := url.Parse(link[start+1 : end])
		if err != nil {
			return 0
		}
		next, _ := strconv.Atoi(parsed.Query().Get("page"))
		return next
	}
	return 0
}

func normalizeInventoryPayload(provider, projectID, kind string, payload []byte) ([]forges.Object, error) {
	var values []json.RawMessage
	if provider == "github" && (kind == "pipeline" || kind == "artifact") {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(payload, &wrapper) != nil {
			return nil, ErrInvalid
		}
		key := "workflow_runs"
		if kind == "artifact" {
			key = "artifacts"
		}
		if json.Unmarshal(wrapper[key], &values) != nil {
			return nil, ErrInvalid
		}
	} else if json.Unmarshal(payload, &values) != nil {
		return nil, ErrInvalid
	}
	objects := make([]forges.Object, 0, len(values))
	for _, raw := range values {
		var value map[string]any
		if json.Unmarshal(raw, &value) != nil {
			return nil, ErrInvalid
		}
		if provider == "github" && kind == "issue" {
			if _, pull := value["pull_request"]; pull {
				continue
			}
		}
		id := firstString(value, "id", "number", "iid", "name", "tag_name")
		if id == "" {
			return nil, ErrInvalid
		}
		item := forges.Object{
			ProjectID: projectID, Provider: provider, Kind: kind, ExternalID: id,
			Title:            firstString(value, "title", "name", "ref"),
			State:            firstString(value, "state", "status", "conclusion"),
			Ref:              firstString(value, "ref", "name", "tag_name", "source_branch"),
			SHA:              firstString(value, "sha", "target", "commit_sha", "head_sha"),
			URL:              firstString(value, "html_url", "web_url", "url"),
			ProviderMetadata: append(json.RawMessage(nil), raw...),
		}
		if parent := value["pipeline"]; parent != nil {
			if nested, ok := parent.(map[string]any); ok {
				item.ParentID = firstString(nested, "id")
			}
		}
		if commitValue, ok := value["commit"].(map[string]any); ok && !commit.MatchString(item.SHA) {
			item.SHA = firstString(commitValue, "sha", "id")
		}
		if head, ok := value["head"].(map[string]any); ok {
			if item.Ref == "" {
				item.Ref = firstString(head, "ref")
			}
			if item.SHA == "" {
				item.SHA = firstString(head, "sha")
			}
		}
		if updated := firstString(value, "updated_at", "created_at"); updated != "" {
			parsed, err := time.Parse(time.RFC3339, updated)
			if err != nil {
				return nil, ErrInvalid
			}
			parsed = parsed.UTC()
			item.UpdatedAt = &parsed
		}
		objects = append(objects, item)
	}
	return objects, nil
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := stringValue(value[key]); result != "" {
			return result
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed >= 0 && typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	case json.Number:
		return typed.String()
	}
	return ""
}
