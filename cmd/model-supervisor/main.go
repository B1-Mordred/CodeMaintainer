package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/models"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "model-supervisor", "version", version)
	if err := run(logger); err != nil {
		logger.Error("model supervisor stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	catalog, err := models.LoadCatalog(environment("MODEL_MANIFEST_ROOT", "/etc/maintainer/models"))
	if err != nil {
		return err
	}
	runtime, err := models.NewLlamaRuntime(environment("LLAMA_SERVER_BINARY", "/usr/local/bin/llama-server"))
	if err != nil {
		return err
	}
	supervisor, err := models.NewSupervisor(catalog, environment("MODEL_ROOT", "/models"), runtime)
	if err != nil {
		return err
	}
	tokenPayload, err := os.ReadFile(environment("MODEL_CONTROL_TOKEN_FILE", "/run/secrets/model-control.token"))
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(tokenPayload))
	service, err := models.NewService(supervisor, token, logger)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: environment("MODEL_CONTROL_LISTEN", "0.0.0.0:8081"), Handler: service,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 3 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = supervisor.Unload(shutdown)
		return server.Shutdown(shutdown)
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
