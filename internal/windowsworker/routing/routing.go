package routing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
)

const maxTokenBytes = 4096

var safeReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type TokenResolver interface {
	ResolveWindowsWorkerToken(context.Context, string) ([]byte, error)
}

// DirectoryTokens maps an opaque credential reference to one regular,
// non-symlink token file below an operator-owned fixed directory. Callers can
// never supply a path; "worker-main" resolves only to "worker-main.token".
type DirectoryTokens struct{ Root string }

func (d DirectoryTokens) ResolveWindowsWorkerToken(_ context.Context, reference string) ([]byte, error) {
	if !filepath.IsAbs(d.Root) || !safeReference.MatchString(reference) {
		return nil, errors.New("invalid Windows worker credential reference")
	}
	path := filepath.Join(filepath.Clean(d.Root), reference+".token")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 32 || info.Size() > maxTokenBytes {
		return nil, errors.New("Windows worker credential is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Windows worker credential is unavailable")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	token := []byte(strings.TrimSpace(string(payload)))
	if err != nil || len(payload) > maxTokenBytes || len(token) < 32 {
		return nil, errors.New("Windows worker credential is unavailable")
	}
	return token, nil
}

type Adapter struct {
	simulator windowsworker.Adapter
	tokens    TokenResolver
	http      *http.Client
}

func New(simulator windowsworker.Adapter, tokens TokenResolver, client *http.Client) (*Adapter, error) {
	if simulator == nil || tokens == nil {
		return nil, errors.New("Windows worker router requires simulator and token resolver")
	}
	return &Adapter{simulator: simulator, tokens: tokens, http: client}, nil
}

func (a *Adapter) ProbeWindowsWorker(ctx context.Context, profile windowsworker.Profile) (windowsworker.Probe, error) {
	if profile.Mode == "simulator" {
		return a.simulator.ProbeWindowsWorker(ctx, profile)
	}
	client, err := a.remoteClient(ctx, profile)
	if err != nil {
		return windowsworker.Probe{}, err
	}
	return client.ProbeWindowsWorker(ctx, profile)
}

func (a *Adapter) RunWindowsJob(ctx context.Context, profile windowsworker.Profile, request windowsworker.RunRequest) (windowsworker.Result, error) {
	if profile.Mode == "simulator" {
		return a.simulator.RunWindowsJob(ctx, profile, request)
	}
	client, err := a.remoteClient(ctx, profile)
	if err != nil {
		return windowsworker.Result{}, err
	}
	return client.RunWindowsJob(ctx, profile, request)
}

func (a *Adapter) remoteClient(ctx context.Context, profile windowsworker.Profile) (*windowsworker.Client, error) {
	if profile.Mode != "remote" || !safeReference.MatchString(profile.CredentialReference) {
		return nil, errors.New("invalid remote Windows worker profile")
	}
	token, err := a.tokens.ResolveWindowsWorkerToken(ctx, profile.CredentialReference)
	if err != nil {
		return nil, err
	}
	client := a.http
	if client == nil {
		client = &http.Client{Timeout: time.Duration(profile.TimeoutSeconds) * time.Second}
	}
	return windowsworker.NewClient(profile.Endpoint, token, client)
}

var _ windowsworker.Adapter = (*Adapter)(nil)
