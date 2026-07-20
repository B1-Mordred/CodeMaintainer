package gitbridge

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GitHubConfiguration struct {
	apiBase *url.URL
	gitBase *url.URL
	tokens  InstallationTokenSource
	http    *http.Client
}

func NewGitHubConfiguration(apiBase, gitBase string, tokens InstallationTokenSource, client *http.Client) (*GitHubConfiguration, error) {
	apiURL, err := validateGitHubAPIBase(apiBase)
	if err != nil || tokens == nil {
		return nil, ErrInvalid
	}
	gitURL, err := validateGitHubAPIBase(gitBase)
	if err != nil || gitURL.RawQuery != "" || gitURL.Fragment != "" {
		return nil, ErrInvalid
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubConfiguration{apiBase: apiURL, gitBase: gitURL, tokens: tokens, http: client}, nil
}

func (g *GitHubConfiguration) remote(repository string) (string, error) {
	if !validRepository(repository) {
		return "", ErrInvalid
	}
	remote := *g.gitBase
	remote.Path = strings.TrimSuffix(remote.Path, "/") + "/" + repository + ".git"
	return remote.String(), nil
}

func (g *GitHubConfiguration) api(repository string) (*GitHubAPI, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return nil, ErrInvalid
	}
	return NewGitHubAPI(g.apiBase.String(), parts[0], parts[1], g.tokens, g.http)
}

func (g *GitHubConfiguration) token(ctx context.Context) (string, error) {
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	if token.Value == "" {
		return "", ErrInvalid
	}
	return token.Value, nil
}
