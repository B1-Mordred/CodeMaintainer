package gitbridge

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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type GitLabTokenSource interface {
	Token(context.Context) (string, error)
}
type StaticGitLabToken string

func (s StaticGitLabToken) Token(context.Context) (string, error) {
	if len(strings.TrimSpace(string(s))) < 20 {
		return "", ErrInvalid
	}
	return strings.TrimSpace(string(s)), nil
}

type FileGitLabToken struct{ Path string }

func (s FileGitLabToken) Token(context.Context) (string, error) {
	if !filepath.IsAbs(s.Path) {
		return "", ErrInvalid
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return "", err
	}
	if len(raw) > 64<<10 {
		return "", ErrInvalid
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < 20 {
		return "", ErrInvalid
	}
	return value, nil
}

type GitLabConfiguration struct {
	apiBase, gitBase *url.URL
	tokens           GitLabTokenSource
	http             *http.Client
}

func NewGitLabConfiguration(apiBase, gitBase string, tokens GitLabTokenSource, client *http.Client) (*GitLabConfiguration, error) {
	apiURL, err := validateForgeBase(apiBase)
	if err != nil || tokens == nil {
		return nil, ErrInvalid
	}
	gitURL, err := validateForgeBase(gitBase)
	if err != nil || gitURL.RawQuery != "" || gitURL.Fragment != "" {
		return nil, ErrInvalid
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitLabConfiguration{apiBase: apiURL, gitBase: gitURL, tokens: tokens, http: client}, nil
}
func validateForgeBase(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalid
	}
	host := parsed.Hostname()
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return nil, ErrInvalid
	}
	return parsed, nil
}
func (g *GitLabConfiguration) remote(repository string) (string, error) {
	if !validRepository(repository) {
		return "", ErrInvalid
	}
	remote := *g.gitBase
	remote.Path = strings.TrimSuffix(remote.Path, "/") + "/" + repository + ".git"
	return remote.String(), nil
}
func (g *GitLabConfiguration) token(ctx context.Context) (string, error) { return g.tokens.Token(ctx) }
func (g *GitLabConfiguration) api(repository string) (*GitLabAPI, error) {
	if !validRepository(repository) {
		return nil, ErrInvalid
	}
	return &GitLabAPI{base: g.apiBase, project: url.PathEscape(repository), repository: repository, tokens: g.tokens, http: g.http}, nil
}

type GitLabAPI struct {
	base                *url.URL
	project, repository string
	tokens              GitLabTokenSource
	http                *http.Client
}
type GitLabMergeRequest struct {
	IID          int    `json:"iid"`
	ID           int64  `json:"id"`
	WebURL       string `json:"web_url"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	SHA          string `json:"sha"`
}

func (g *GitLabAPI) Diagnostics(ctx context.Context) (RepositoryDiagnostics, error) {
	var project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		DefaultBranch     string `json:"default_branch"`
		Archived          bool   `json:"archived"`
		Permissions       struct {
			ProjectAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
		} `json:"permissions"`
	}
	if err := g.call(ctx, http.MethodGet, "/projects/"+g.project, nil, &project); err != nil {
		return RepositoryDiagnostics{}, err
	}
	if project.PathWithNamespace != g.repository || !validGitRef(project.DefaultBranch) {
		return RepositoryDiagnostics{}, ErrInvalid
	}
	level := 0
	if project.Permissions.ProjectAccess != nil {
		level = project.Permissions.ProjectAccess.AccessLevel
	}
	problems := []string{}
	if project.Archived {
		problems = append(problems, "repository is archived")
	}
	if level < 20 {
		problems = append(problems, "repository read permission is unavailable")
	}
	if level < 30 {
		problems = append(problems, "repository publication permission is unavailable")
	}
	return RepositoryDiagnostics{Repository: g.repository, DefaultBranch: project.DefaultBranch, CanRead: level >= 20, CanPublish: level >= 30, Ready: len(problems) == 0, Problems: problems}, nil
}
func (g *GitLabAPI) CreateDraftMergeRequest(ctx context.Context, request DraftPullRequestRequest) (GitLabMergeRequest, error) {
	if !safeID.MatchString(request.IdempotencyKey) || !validGitRef(request.Branch) || !validGitRef(request.Base) || strings.TrimSpace(request.Title) == "" || len(request.Title) > 256 || len(request.Body) > 64<<10 {
		return GitLabMergeRequest{}, ErrInvalid
	}
	marker := "<!-- maintainer-operation:" + request.IdempotencyKey + " -->"
	var existing []struct {
		GitLabMergeRequest
		Description string `json:"description"`
	}
	path := "/projects/" + g.project + "/merge_requests?state=opened&source_branch=" + url.QueryEscape(request.Branch) + "&target_branch=" + url.QueryEscape(request.Base) + "&per_page=20"
	if err := g.call(ctx, http.MethodGet, path, nil, &existing); err != nil {
		return GitLabMergeRequest{}, err
	}
	for _, item := range existing {
		if strings.Contains(item.Description, marker) {
			return item.GitLabMergeRequest, nil
		}
	}
	payload := map[string]any{"source_branch": request.Branch, "target_branch": request.Base, "title": "Draft: " + request.Title, "description": request.Body + "\n\n" + marker, "remove_source_branch": false, "squash": false}
	var created GitLabMergeRequest
	if err := g.call(ctx, http.MethodPost, "/projects/"+g.project+"/merge_requests", payload, &created); err != nil {
		return GitLabMergeRequest{}, err
	}
	if created.IID <= 0 || created.ID <= 0 || created.WebURL == "" || !validGitRef(created.SourceBranch) || !validGitRef(created.TargetBranch) {
		return GitLabMergeRequest{}, ErrInvalid
	}
	created.Draft = true
	return created, nil
}
func (g *GitLabAPI) call(ctx context.Context, method, path string, body, destination any) error {
	endpoint := *g.base
	parts := strings.SplitN(path, "?", 2)
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/api/v4" + parts[0]
	if len(parts) == 2 {
		endpoint.RawQuery = parts[1]
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return ErrInvalid
		}
		reader = bytes.NewReader(payload)
	}
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return ErrInvalid
	}
	request.Header.Set("PRIVATE-TOKEN", token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := g.http.Do(request)
	if err != nil {
		return fmt.Errorf("GitLab API request failed: %w", err)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxGitHubResponseBytes+1))
	if readErr != nil || int64(len(payload)) > maxGitHubResponseBytes {
		return errors.New("GitLab API response is unreadable or oversized")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusConflict, http.StatusUnprocessableEntity:
			return ErrConflict
		case http.StatusTooManyRequests:
			retry := response.Header.Get("Retry-After")
			if _, err := strconv.Atoi(retry); err != nil {
				return errors.New("GitLab rate limit response is malformed")
			}
			return fmt.Errorf("GitLab rate limited for %s seconds", retry)
		default:
			return fmt.Errorf("GitLab API returned status %d", response.StatusCode)
		}
	}
	if destination != nil && json.Unmarshal(payload, destination) != nil {
		return ErrInvalid
	}
	return nil
}
