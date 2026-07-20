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

type GitHubAPI struct {
	base       *url.URL
	owner      string
	repository string
	tokens     InstallationTokenSource
	http       *http.Client
}

type GitHubIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Trusted bool   `json:"trusted"`
}

type RepositoryDiagnostics struct {
	Repository    string   `json:"repository"`
	DefaultBranch string   `json:"default_branch"`
	CanRead       bool     `json:"can_read"`
	CanPublish    bool     `json:"can_publish"`
	Ready         bool     `json:"ready"`
	Problems      []string `json:"problems"`
}

type DraftPullRequestRequest struct {
	IdempotencyKey string
	Branch         string
	Base           string
	Title          string
	Body           string
}

type GitHubPullRequest struct {
	Number       int    `json:"number"`
	NodeID       string `json:"node_id"`
	HTMLURL      string `json:"html_url"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	Merged       bool   `json:"merged"`
	HeadSHA      string `json:"head_sha"`
	Branch       string `json:"branch"`
	Base         string `json:"base"`
	MergedCommit string `json:"merged_commit,omitempty"`
}

func NewGitHubAPI(apiBase, owner, repository string, tokens InstallationTokenSource, client *http.Client) (*GitHubAPI, error) {
	base, err := validateGitHubAPIBase(apiBase)
	if err != nil || !safeID.MatchString(owner) || !safeID.MatchString(repository) || tokens == nil {
		return nil, ErrInvalid
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubAPI{base: base, owner: owner, repository: repository, tokens: tokens, http: client}, nil
}

func (g *GitHubAPI) Issue(ctx context.Context, number int) (GitHubIssue, error) {
	if number <= 0 {
		return GitHubIssue{}, ErrInvalid
	}
	var issue GitHubIssue
	if err := g.call(ctx, http.MethodGet, g.repoPath()+"/issues/"+strconv.Itoa(number), nil, &issue); err != nil {
		return GitHubIssue{}, err
	}
	if issue.Number != number || len(issue.Title) > 1024 || len(issue.Body) > 1<<20 || !validGitHubState(issue.State) {
		return GitHubIssue{}, ErrInvalid
	}
	issue.Trusted = false
	return issue, nil
}

func (g *GitHubAPI) Diagnostics(ctx context.Context) (RepositoryDiagnostics, error) {
	var response struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Disabled      bool   `json:"disabled"`
		Permissions   struct {
			Admin bool `json:"admin"`
			Pull  bool `json:"pull"`
			Push  bool `json:"push"`
		} `json:"permissions"`
	}
	if err := g.call(ctx, http.MethodGet, g.repoPath(), nil, &response); err != nil {
		return RepositoryDiagnostics{}, err
	}
	expected := g.owner + "/" + g.repository
	if response.FullName != expected || !validGitRef(response.DefaultBranch) || response.Permissions.Admin {
		return RepositoryDiagnostics{}, ErrInvalid
	}
	problems := []string{}
	if response.Archived {
		problems = append(problems, "repository is archived")
	}
	if response.Disabled {
		problems = append(problems, "repository is disabled")
	}
	if !response.Permissions.Pull {
		problems = append(problems, "contents read permission is unavailable")
	}
	if !response.Permissions.Push {
		problems = append(problems, "contents publication permission is unavailable")
	}
	return RepositoryDiagnostics{
		Repository: expected, DefaultBranch: response.DefaultBranch, CanRead: response.Permissions.Pull,
		CanPublish: response.Permissions.Push, Ready: len(problems) == 0, Problems: problems,
	}, nil
}

func (g *GitHubAPI) PullRequest(ctx context.Context, number int) (GitHubPullRequest, error) {
	if number <= 0 {
		return GitHubPullRequest{}, ErrInvalid
	}
	var response githubPullResponse
	if err := g.call(ctx, http.MethodGet, g.repoPath()+"/pulls/"+strconv.Itoa(number), nil, &response); err != nil {
		return GitHubPullRequest{}, err
	}
	return response.normalized()
}

func (g *GitHubAPI) CreateDraftPullRequest(ctx context.Context, request DraftPullRequestRequest) (GitHubPullRequest, error) {
	if !safeID.MatchString(request.IdempotencyKey) || !validGitRef(request.Branch) || !validGitRef(request.Base) ||
		strings.TrimSpace(request.Title) == "" || len(request.Title) > 256 || len(request.Body) > 64<<10 {
		return GitHubPullRequest{}, ErrInvalid
	}
	if existing, found, err := g.findOpenPull(ctx, request); err != nil || found {
		return existing, err
	}
	payload := map[string]any{
		"title": request.Title, "body": request.Body + "\n\n<!-- maintainer-operation:" + request.IdempotencyKey + " -->",
		"head": request.Branch, "base": request.Base, "draft": true,
	}
	var response githubPullResponse
	err := g.call(ctx, http.MethodPost, g.repoPath()+"/pulls", payload, &response)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			if existing, found, lookupErr := g.findOpenPull(ctx, request); lookupErr == nil && found {
				return existing, nil
			}
		}
		return GitHubPullRequest{}, err
	}
	result, err := response.normalized()
	if err != nil || !result.Draft {
		return GitHubPullRequest{}, ErrInvalid
	}
	return result, nil
}

func (g *GitHubAPI) findOpenPull(ctx context.Context, request DraftPullRequestRequest) (GitHubPullRequest, bool, error) {
	query := url.Values{"state": {"open"}, "head": {g.owner + ":" + request.Branch}, "base": {request.Base}, "per_page": {"10"}}
	var responses []githubPullResponse
	if err := g.call(ctx, http.MethodGet, g.repoPath()+"/pulls?"+query.Encode(), nil, &responses); err != nil {
		return GitHubPullRequest{}, false, err
	}
	marker := "<!-- maintainer-operation:" + request.IdempotencyKey + " -->"
	for _, response := range responses {
		if response.Head.Ref != request.Branch || response.Base.Ref != request.Base || !strings.Contains(response.Body, marker) {
			continue
		}
		result, err := response.normalized()
		return result, err == nil, err
	}
	return GitHubPullRequest{}, false, nil
}

func (g *GitHubAPI) call(ctx context.Context, method, path string, body, destination any) error {
	endpoint := *g.base
	parts := strings.SplitN(path, "?", 2)
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + parts[0]
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
	request.Header.Set("Authorization", "Bearer "+token.Value)
	setGitHubHeaders(request)
	response, err := g.http.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxGitHubResponseBytes+1))
	if readErr != nil || int64(len(payload)) > maxGitHubResponseBytes {
		return errors.New("GitHub API response is unreadable or oversized")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusConflict, http.StatusUnprocessableEntity:
			return ErrConflict
		default:
			return fmt.Errorf("GitHub API returned status %d", response.StatusCode)
		}
	}
	if destination != nil && json.Unmarshal(payload, destination) != nil {
		return ErrInvalid
	}
	return nil
}

func (g *GitHubAPI) repoPath() string {
	return "/repos/" + url.PathEscape(g.owner) + "/" + url.PathEscape(g.repository)
}

type githubPullResponse struct {
	Number         int    `json:"number"`
	NodeID         string `json:"node_id"`
	HTMLURL        string `json:"html_url"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Body           string `json:"body"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (r githubPullResponse) normalized() (GitHubPullRequest, error) {
	if r.Number <= 0 || r.NodeID == "" || r.HTMLURL == "" || !validGitHubState(r.State) || !commit.MatchString(r.Head.SHA) ||
		!validGitRef(r.Head.Ref) || !validGitRef(r.Base.Ref) || (r.Merged && !commit.MatchString(r.MergeCommitSHA)) {
		return GitHubPullRequest{}, ErrInvalid
	}
	return GitHubPullRequest{
		Number: r.Number, NodeID: r.NodeID, HTMLURL: r.HTMLURL, State: r.State,
		Draft: r.Draft, Merged: r.Merged, HeadSHA: r.Head.SHA, Branch: r.Head.Ref,
		Base: r.Base.Ref, MergedCommit: r.MergeCommitSHA,
	}, nil
}

func validGitRef(value string) bool {
	return value != "" && len(value) <= 255 && !strings.ContainsAny(value, " ~^:?*[\\") &&
		!strings.Contains(value, "..") && !strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/")
}

func validGitHubState(value string) bool { return value == "open" || value == "closed" }
