package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	handler, err := gitbridge.NewService(manager, token, logger)
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

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
