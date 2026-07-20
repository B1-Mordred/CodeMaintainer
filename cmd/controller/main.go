package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/local-code-maintainer/appliance/internal/api"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dataRoot := env("MAINTAINER_DATA_ROOT", ".data")
	profile := env("MAINTAINER_PROFILE", "mock")
	listen := env("MAINTAINER_LISTEN", "127.0.0.1:8080")
	databasePath := env("MAINTAINER_DATABASE", filepath.Join(dataRoot, "database", "controller.db"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := storesqlite.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := ensureDefaultConfig(ctx, store, dataRoot, listen, profile); err != nil {
		return err
	}

	handler := api.NewServer(store, logger.With("component", "api", "version", version), profile)
	server := &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE responses intentionally outlive request timeouts.
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("controller listening", "address", listen, "profile", profile, "data_root", dataRoot)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := api.ShutdownContext(context.Background())
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func ensureDefaultConfig(ctx context.Context, store storage.ConfigStore, dataRoot, listen, profile string) error {
	if _, err := store.CurrentConfig(ctx); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	document := appconfig.Default(dataRoot)
	document.Deployment.ListenAddress = listen
	document.Deployment.Profile = profile
	after, err := json.Marshal(document)
	if err != nil {
		return err
	}
	validation, _ := json.Marshal(map[string]any{"valid": true, "errors": []string{}})
	diff, _ := json.Marshal([]map[string]any{{"op": "add", "path": "/", "value": document}})
	_, err = store.CreateConfigRevision(ctx, appconfig.Revision{
		ActorID: "system", SchemaVersion: appconfig.SchemaVersion,
		Before: json.RawMessage(`{}`), After: after, Diff: diff, ValidationResult: validation,
		Reason: "initial bootstrap configuration",
	})
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
