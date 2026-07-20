package gitbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = int64(3 << 20)

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func NewClient(endpoint string, token []byte) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 {
		return nil, ErrInvalid
	}
	return &Client{
		endpoint: strings.TrimSuffix(endpoint, "/"), token: string(token),
		http: &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

func (c *Client) Register(ctx context.Context, request Registration) error {
	return c.call(ctx, "/v1/projects/register", request, nil)
}

func (c *Client) Sync(ctx context.Context, projectID string) (SyncResult, error) {
	var result SyncResult
	err := c.call(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/sync", struct{}{}, &result)
	return result, err
}

func (c *Client) CreateWorktree(ctx context.Context, request WorktreeRequest) (WorktreeResult, error) {
	var result WorktreeResult
	err := c.call(ctx, "/v1/worktrees", request, &result)
	return result, err
}

func (c *Client) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	var result CommitResult
	err := c.call(ctx, "/v1/commits", request, &result)
	return result, err
}

func (c *Client) Diff(ctx context.Context, request DiffRequest) (DiffResult, error) {
	var result DiffResult
	err := c.call(ctx, "/v1/diffs", request, &result)
	return result, err
}

func (c *Client) Publish(ctx context.Context, request PublishRequest) (Publication, error) {
	var result Publication
	err := c.call(ctx, "/v1/publications", request, &result)
	return result, err
}

func (c *Client) ValidateWebhook(ctx context.Context, request WebhookValidationRequest) (PullRequestEvent, error) {
	var result PullRequestEvent
	err := c.call(ctx, "/v1/webhooks/validate", request, &result)
	return result, err
}

func (c *Client) PullRequestEvent(ctx context.Context, projectID string, number int) (PullRequestEvent, error) {
	if !safeID.MatchString(projectID) || number <= 0 {
		return PullRequestEvent{}, ErrInvalid
	}
	var result PullRequestEvent
	err := c.call(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/pulls/"+strconv.Itoa(number)+"/event", struct{}{}, &result)
	return result, err
}

func (c *Client) RepositoryDiagnostics(ctx context.Context, projectID string) (RepositoryDiagnostics, error) {
	if !safeID.MatchString(projectID) {
		return RepositoryDiagnostics{}, ErrInvalid
	}
	var result RepositoryDiagnostics
	err := c.call(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/diagnostics", struct{}{}, &result)
	return result, err
}

func (c *Client) Issue(ctx context.Context, projectID string, number int) (GitHubIssue, error) {
	if !safeID.MatchString(projectID) || number <= 0 {
		return GitHubIssue{}, ErrInvalid
	}
	var result GitHubIssue
	err := c.call(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/issues/"+strconv.Itoa(number), struct{}{}, &result)
	return result, err
}

func (c *Client) PullRequest(ctx context.Context, projectID string, number int) (GitHubPullRequest, error) {
	if !safeID.MatchString(projectID) || number <= 0 {
		return GitHubPullRequest{}, ErrInvalid
	}
	var result GitHubPullRequest
	err := c.call(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/pulls/"+strconv.Itoa(number), struct{}{}, &result)
	return result, err
}

func (c *Client) call(ctx context.Context, path string, body, destination any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call Git bridge: %w", err)
	}
	defer response.Body.Close()
	responsePayload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || int64(len(responsePayload)) > maxResponseBytes {
		return errors.New("Git bridge response is unreadable or oversized")
	}
	if response.StatusCode >= 300 {
		var envelope map[string]string
		_ = json.Unmarshal(responsePayload, &envelope)
		message := envelope["error"]
		if message == "" {
			message = response.Status
		}
		switch response.StatusCode {
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", ErrNotFound, message)
		case http.StatusConflict:
			if strings.Contains(message, ErrUpstreamMoved.Error()) {
				return fmt.Errorf("%w: %s", ErrUpstreamMoved, message)
			}
			return fmt.Errorf("%w: %s", ErrConflict, message)
		default:
			return fmt.Errorf("%w: %s", ErrInvalid, message)
		}
	}
	if destination == nil {
		return nil
	}
	if err := json.Unmarshal(responsePayload, destination); err != nil {
		return fmt.Errorf("decode Git bridge response: %w", err)
	}
	return nil
}

var _ Backend = (*Client)(nil)
