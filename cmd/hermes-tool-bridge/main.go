package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/hermesbridge"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(); err != nil {
		logger.Error("Hermes tool bridge stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	tokenPath := strings.TrimSpace(os.Getenv("HERMES_BRIDGE_CONTROLLER_TOKEN_FILE"))
	if tokenPath == "" {
		return errors.New("HERMES_BRIDGE_CONTROLLER_TOKEN_FILE is required")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	bridge, err := hermesbridge.New(env("HERMES_BRIDGE_CONTROLLER_URL", "http://controller:8080"), []byte(strings.TrimSpace(string(token))))
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: env("HERMES_BRIDGE_LISTEN", "0.0.0.0:8085"), Handler: bridge,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	return server.ListenAndServe()
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
