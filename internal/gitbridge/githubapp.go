package gitbridge

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubAPIVersion       = "2022-11-28"
	maxGitHubResponseBytes = int64(3 << 20)
)

var defaultGitHubPermissions = map[string]string{
	"actions":       "read",
	"checks":        "read",
	"contents":      "write",
	"issues":        "read",
	"metadata":      "read",
	"pull_requests": "write",
}

type InstallationToken struct {
	Value     string
	ExpiresAt time.Time
}

type InstallationTokenSource interface {
	Token(context.Context) (InstallationToken, error)
}

type GitHubAppTokenSource struct {
	mu             sync.Mutex
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	baseURL        *url.URL
	http           *http.Client
	now            func() time.Time
	cached         InstallationToken
}

func NewGitHubAppTokenSource(appID, installationID int64, privateKeyPEM []byte, apiBase string, client *http.Client) (*GitHubAppTokenSource, error) {
	if appID <= 0 || installationID <= 0 {
		return nil, ErrInvalid
	}
	key, err := parseGitHubAppKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	base, err := validateGitHubAPIBase(apiBase)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubAppTokenSource{
		appID: appID, installationID: installationID, privateKey: key,
		baseURL: base, http: client, now: time.Now,
	}, nil
}

func (s *GitHubAppTokenSource) Token(ctx context.Context) (InstallationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if s.cached.Value != "" && s.cached.ExpiresAt.After(now.Add(time.Minute)) {
		return s.cached, nil
	}
	assertion, err := s.signedAssertion(now)
	if err != nil {
		return InstallationToken{}, err
	}
	body, _ := json.Marshal(map[string]any{"permissions": defaultGitHubPermissions})
	endpoint := *s.baseURL
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/app/installations/" + strconv.FormatInt(s.installationID, 10) + "/access_tokens"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return InstallationToken{}, errors.New("construct GitHub App token request")
	}
	request.Header.Set("Authorization", "Bearer "+assertion)
	setGitHubHeaders(request)
	response, err := s.http.Do(request)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("request GitHub App installation token: %w", err)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxGitHubResponseBytes+1))
	if readErr != nil || int64(len(payload)) > maxGitHubResponseBytes {
		return InstallationToken{}, errors.New("GitHub App token response is unreadable or oversized")
	}
	if response.StatusCode != http.StatusCreated {
		return InstallationToken{}, fmt.Errorf("GitHub App token request returned status %d", response.StatusCode)
	}
	var decoded struct {
		Token       string            `json:"token"`
		ExpiresAt   time.Time         `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || len(decoded.Token) < 20 || len(decoded.Token) > 4096 ||
		!decoded.ExpiresAt.After(now.Add(time.Minute)) || decoded.ExpiresAt.After(now.Add(2*time.Hour)) ||
		!safeGitHubPermissions(decoded.Permissions) {
		return InstallationToken{}, errors.New("GitHub App token response violates the narrow contract")
	}
	s.cached = InstallationToken{Value: decoded.Token, ExpiresAt: decoded.ExpiresAt.UTC()}
	return s.cached, nil
}

func (s *GitHubAppTokenSource) signedAssertion(now time.Time) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(s.appID, 10),
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("sign GitHub App assertion")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseGitHubAppKey(payload []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(payload)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrInvalid
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrInvalid
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrInvalid
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	if key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, ErrInvalid
	}
	return key, nil
}

func validateGitHubAPIBase(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalid
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || (host != "localhost" && net.ParseIP(host) == nil) || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
			return nil, ErrInvalid
		}
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

func safeGitHubPermissions(actual map[string]string) bool {
	for name, level := range actual {
		allowed, ok := defaultGitHubPermissions[name]
		if !ok || permissionRank(level) > permissionRank(allowed) || permissionRank(level) == 0 {
			return false
		}
	}
	for name, required := range defaultGitHubPermissions {
		if permissionRank(actual[name]) < permissionRank(required) {
			return false
		}
	}
	return true
}

func permissionRank(value string) int {
	switch value {
	case "read":
		return 1
	case "write":
		return 2
	default:
		return 0
	}
}

func setGitHubHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
}
