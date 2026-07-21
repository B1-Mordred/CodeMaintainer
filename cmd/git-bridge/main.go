package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/local-code-maintainer/appliance/internal/gitbridge"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Git bridge stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	root := env("GIT_BRIDGE_DATA_ROOT", "/var/lib/maintainer")
	listen := env("GIT_BRIDGE_LISTEN", "0.0.0.0:8083")
	tokenPath := env("GIT_BRIDGE_TOKEN_FILE", "/run/secrets/git-bridge.token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	token = []byte(strings.TrimSpace(string(token)))
	manager, err := gitbridge.NewManager(
		filepath.Join(root, "mirrors"), filepath.Join(root, "worktrees"), filepath.Join(root, "remotes"),
	)
	if err != nil {
		return err
	}
	if err := configureGitHub(manager); err != nil {
		return err
	}
	if err := configureGitLab(manager); err != nil {
		return err
	}
	var webhook *gitbridge.WebhookValidator
	if webhookPath := strings.TrimSpace(os.Getenv("GIT_BRIDGE_WEBHOOK_SECRET_FILE")); webhookPath != "" {
		secret, readErr := os.ReadFile(webhookPath)
		if readErr != nil {
			return readErr
		}
		webhook, err = gitbridge.NewWebhookValidator([]byte(strings.TrimSpace(string(secret))))
		if err != nil {
			return err
		}
	}
	handler, err := gitbridge.NewServiceWithWebhook(manager, token, webhook, logger)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func configureGitLab(manager *gitbridge.Manager) error {
	tokenPath := strings.TrimSpace(os.Getenv("GITLAB_TOKEN_FILE"))
	apiURL := strings.TrimSpace(os.Getenv("GITLAB_API_URL"))
	gitURL := strings.TrimSpace(os.Getenv("GITLAB_GIT_URL"))
	if tokenPath == "" && apiURL == "" && gitURL == "" {
		return nil
	}
	if tokenPath == "" {
		return errors.New("GitLab activation requires a token file")
	}
	if apiURL == "" {
		apiURL = "https://gitlab.com"
	}
	if gitURL == "" {
		gitURL = apiURL
	}
	configuration, err := gitbridge.NewGitLabConfiguration(apiURL, gitURL, gitbridge.FileGitLabToken{Path: tokenPath}, nil)
	if err != nil {
		return err
	}
	return manager.EnableGitLab(configuration)
}

func configureGitHub(manager *gitbridge.Manager) error {
	appIDValue := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	installationValue := strings.TrimSpace(os.Getenv("GITHUB_APP_INSTALLATION_ID"))
	keyPath := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
	if appIDValue == "" && installationValue == "" && keyPath == "" {
		return nil
	}
	if appIDValue == "" || installationValue == "" || keyPath == "" {
		return errors.New("GitHub App activation requires app ID, installation ID, and private-key file")
	}
	appID, err := strconv.ParseInt(appIDValue, 10, 64)
	if err != nil {
		return errors.New("GITHUB_APP_ID is invalid")
	}
	installationID, err := strconv.ParseInt(installationValue, 10, 64)
	if err != nil {
		return errors.New("GITHUB_APP_INSTALLATION_ID is invalid")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	if len(key) > 64<<10 {
		return errors.New("GitHub App private key is oversized")
	}
	apiBase := env("GITHUB_API_URL", "https://api.github.com")
	tokens, err := gitbridge.NewGitHubAppTokenSource(appID, installationID, key, apiBase, nil)
	if err != nil {
		return err
	}
	configuration, err := gitbridge.NewGitHubConfiguration(apiBase, env("GITHUB_GIT_URL", "https://github.com"), tokens, nil)
	if err != nil {
		return err
	}
	return manager.EnableGitHub(configuration)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
