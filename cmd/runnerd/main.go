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

	"github.com/B1-Mordred/CodeMaintainer/internal/runnerd"
	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "runnerd", "version", version)
	if err := run(logger); err != nil {
		logger.Error("runnerd stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	profile := env("RUNNERD_PROFILE", "mock")
	dataRoot := env("RUNNERD_DATA_ROOT", "/var/lib/maintainer")
	var policy *runnerd.Policy
	var executor runnerd.Executor
	var closeExecutor func() error
	switch profile {
	case "mock":
		digest := strings.Repeat("0", 64)
		var err error
		policy, err = runnerd.NewPolicy(dataRoot, map[runners.Kind]string{
			runners.KindDependencies:   "local/maintainer-dependencies@sha256:" + digest,
			runners.KindImplementation: "local/maintainer-implementation@sha256:" + digest,
			runners.KindVerification:   "local/maintainer-verification@sha256:" + digest,
			runners.KindQC:             "local/maintainer-qc@sha256:" + digest,
			runners.KindDocumentation:  "local/maintainer-documentation@sha256:" + digest,
		})
		if err != nil {
			return err
		}
		executor = runnerd.NewFakeExecutor()
	case "production":
		var err error
		policy, err = runnerd.LoadPolicyFile(env("RUNNERD_POLICY_FILE", "/etc/maintainer/runner-policy.json"), dataRoot)
		if err != nil {
			return err
		}
		dockerExecutor, err := runnerd.NewDockerExecutor(
			env("RUNNERD_DOCKER_SOCKET", "/run/worker-docker/docker.sock"),
			dataRoot,
			env("RUNNERD_DEPENDENCY_NETWORK", "maintainer-dependency-egress"),
			env("RUNNERD_INFERENCE_NETWORK", "maintainer-inference-only"),
			env("RUNNERD_INFERENCE_URL", "http://model-supervisor:8082/v1"),
			policy.WorkerUser(),
		)
		if err != nil {
			return err
		}
		executor = dockerExecutor
		closeExecutor = dockerExecutor.Close
	default:
		return errors.New("RUNNERD_PROFILE must be exactly mock or production")
	}
	if closeExecutor != nil {
		defer closeExecutor()
	}
	token, err := runnerd.ReadTokenFile(env("RUNNERD_TOKEN_FILE", "/run/secrets/runnerd.token"))
	if err != nil {
		return err
	}
	service, err := runnerd.NewService(policy, executor, token, logger)
	if err != nil {
		return err
	}
	listener, err := runnerd.ListenUnix(env("RUNNERD_SOCKET", "/run/maintainer/runnerd.sock"))
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler: service, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("runnerd listening", "network", "unix", "profile", profile)
		errorsChannel <- server.Serve(listener)
	}()
	select {
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
