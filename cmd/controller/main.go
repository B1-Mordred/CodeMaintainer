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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/local-code-maintainer/appliance/internal/api"
	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	maintainerauth "github.com/local-code-maintainer/appliance/internal/auth"
	"github.com/local-code-maintainer/appliance/internal/automation"
	"github.com/local-code-maintainer/appliance/internal/backup"
	appconfig "github.com/local-code-maintainer/appliance/internal/config"
	"github.com/local-code-maintainer/appliance/internal/gitbridge"
	"github.com/local-code-maintainer/appliance/internal/githubsync"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
	"github.com/local-code-maintainer/appliance/internal/models"
	"github.com/local-code-maintainer/appliance/internal/queue"
	"github.com/local-code-maintainer/appliance/internal/runners"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
	"github.com/local-code-maintainer/appliance/internal/workflow"
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
	if applied, err := backup.ApplyPending(dataRoot); err != nil {
		return err
	} else if applied {
		logger.Info("staged restore applied before database open")
	}

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
	activeRevision, err := store.CurrentConfig(ctx)
	if err != nil {
		return err
	}
	var activeConfig appconfig.System
	if err := json.Unmarshal(activeRevision.After, &activeConfig); err != nil {
		return errors.New("decode active configuration for registry initialization")
	}
	registry, err := appconfig.BuiltInRegistry(activeConfig)
	if err != nil {
		return err
	}
	configRegistry, err := appconfig.NewRegistryService(store, registry)
	if err != nil {
		return err
	}
	if _, err := configRegistry.EnsureSystemScope(ctx, activeConfig, activeRevision.ID); err != nil {
		return err
	}
	if err := store.SetConfigurationRegistry(registry); err != nil {
		return err
	}
	artifactStore, err := artifactfiles.New(filepath.Join(dataRoot, "artifacts"), store)
	if err != nil {
		return err
	}
	authService, err := maintainerauth.NewService(store)
	if err != nil {
		return err
	}
	backupManager, err := backup.New(dataRoot, profile, store, env("MAINTAINER_BACKUP_KEY_FILE", filepath.Join(dataRoot, "secrets", "backup.key")))
	if err != nil {
		return err
	}
	secureCookie, err := strconv.ParseBool(env("MAINTAINER_SECURE_COOKIE", "false"))
	if err != nil {
		return errors.New("MAINTAINER_SECURE_COOKIE must be true or false")
	}
	engine, modelManager, err := newWorkflowEngine(store, artifactStore, dataRoot, profile)
	if err != nil {
		return err
	}
	worker := queue.NewWorker(store, queue.ProcessorFunc(func(processContext context.Context, job jobs.Job) error {
		cancel := func() {}
		if !job.DeadlineAt.IsZero() {
			processContext, cancel = context.WithDeadline(processContext, job.DeadlineAt)
		}
		defer cancel()
		_, stepErr := engine.Step(processContext, job.ID)
		return stepErr
	}), "controller-workflow", 30*time.Second, 250*time.Millisecond, logger.With("component", "workflow"))
	workerErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(ctx) }()
	scheduler, err := automation.NewScheduler(store, time.Second, logger.With("component", "scheduler"))
	if err != nil {
		return err
	}
	schedulerErrors := make(chan error, 1)
	go func() { schedulerErrors <- scheduler.Run(ctx) }()
	memoryIndex, err := newMemoryIndex(profile, dataRoot)
	if err != nil {
		return err
	}
	indexErrors := make(chan error, 1)
	if memoryIndex != nil {
		synchronizer, syncErr := memory.NewSynchronizer(store, memoryIndex, "controller-memory-index", time.Second, logger.With("component", "memory-index"))
		if syncErr != nil {
			return syncErr
		}
		go func() { indexErrors <- synchronizer.Run(ctx) }()
	}

	intelligenceService, err := intelligence.NewService(store, nil)
	if err != nil {
		return err
	}
	serverOptions := []api.Option{api.WithArtifactReader(artifactStore), api.WithAuthentication(authService, secureCookie), api.WithModelManager(modelManager), api.WithBackupService(backupManager), api.WithConfigRegistry(configRegistry), api.WithIntelligence(intelligenceService), api.WithVersion(version)}
	gitToken, err := readToken(env("MAINTAINER_GIT_BRIDGE_TOKEN_FILE", filepath.Join(dataRoot, "secrets", "git-bridge.token")))
	if err != nil {
		return err
	}
	gitWebhookClient, err := gitbridge.NewClient(env("MAINTAINER_GIT_BRIDGE_URL", "http://git-bridge:8083"), gitToken)
	if err != nil {
		return err
	}
	serverOptions = append(serverOptions, api.WithGitHubWebhookValidator(gitWebhookClient), api.WithGitOperator(gitWebhookClient))
	githubReconciler, err := githubsync.New(store, artifactStore, gitWebhookClient, 5*time.Minute, logger.With("component", "github-poll"))
	if err != nil {
		return err
	}
	githubErrors := make(chan error, 1)
	go func() { githubErrors <- githubReconciler.Run(ctx) }()
	if memoryIndex != nil {
		serverOptions = append(serverOptions, api.WithMemoryIndex(memoryIndex))
	}
	if tokenFile := strings.TrimSpace(os.Getenv("MAINTAINER_HERMES_TOKEN_FILE")); tokenFile != "" {
		hermesToken, tokenErr := readToken(tokenFile)
		if tokenErr != nil {
			return tokenErr
		}
		serverOptions = append(serverOptions, api.WithHermesToken(hermesToken))
	}
	handler := api.NewServer(store, logger.With("component", "api", "version", version), profile, serverOptions...)
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
	case err := <-workerErrors:
		if ctx.Err() != nil {
			return nil
		}
		return err
	case err := <-indexErrors:
		if ctx.Err() != nil {
			return nil
		}
		return err
	case err := <-schedulerErrors:
		if ctx.Err() != nil {
			return nil
		}
		return err
	case err := <-githubErrors:
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}

func newMemoryIndex(profile, dataRoot string) (memory.Index, error) {
	rawURL := strings.TrimSpace(os.Getenv("MAINTAINER_OPENVIKING_URL"))
	if rawURL == "" {
		if profile == "mock" {
			return memory.NewFakeIndex(), nil
		}
		return nil, nil
	}
	keyFile := env("MAINTAINER_OPENVIKING_API_KEY_FILE", filepath.Join(dataRoot, "secrets", "openviking.token"))
	return memory.NewOpenVikingIndex(rawURL, keyFile, env("MAINTAINER_OPENVIKING_ACCOUNT", "maintainer"), env("MAINTAINER_OPENVIKING_USER", "maintainer-controller"))
}

func newWorkflowEngine(store *storesqlite.Store, artifactStore *artifactfiles.Store, dataRoot, profile string) (*workflow.Engine, models.Manager, error) {
	gitToken, err := readToken(env("MAINTAINER_GIT_BRIDGE_TOKEN_FILE", filepath.Join(dataRoot, "secrets", "git-bridge.token")))
	if err != nil {
		return nil, nil, err
	}
	gitClient, err := gitbridge.NewClient(env("MAINTAINER_GIT_BRIDGE_URL", "http://git-bridge:8083"), gitToken)
	if err != nil {
		return nil, nil, err
	}
	var modelManager models.Manager
	var execution workflow.ExecutionBackend
	worktreesRoot := filepath.Join(dataRoot, "worktrees")
	if profile == "mock" {
		modelManager = models.NewFake([]models.Profile{
			{ID: "implementation", Role: "implementation", ModelFamily: "qwen-mock", Context: 32768},
			{ID: "qc", Role: "qc", ModelFamily: "mistral-mock", Context: 32768},
		})
		execution, err = workflow.NewDeterministicBackend(worktreesRoot)
	} else if profile == "production" {
		modelToken, tokenErr := readToken(env("MAINTAINER_MODEL_CONTROL_TOKEN_FILE", filepath.Join(dataRoot, "secrets", "model-control.token")))
		if tokenErr != nil {
			return nil, nil, tokenErr
		}
		modelManager, err = models.NewControlClient(env("MAINTAINER_MODEL_CONTROL_URL", "http://model-supervisor:8081"), modelToken)
		if err != nil {
			return nil, nil, err
		}
		runnerToken, tokenErr := readToken(env("MAINTAINER_RUNNERD_TOKEN_FILE", filepath.Join(dataRoot, "secrets", "runnerd.token")))
		if tokenErr != nil {
			return nil, nil, tokenErr
		}
		runnerClient, clientErr := runners.NewUnixClient(env("MAINTAINER_RUNNERD_SOCKET", filepath.Join(dataRoot, "run", "runnerd.sock")), runnerToken)
		if clientErr != nil {
			return nil, nil, clientErr
		}
		execution, err = workflow.NewContainerBackend(runnerClient, artifactStore, dataRoot)
	} else {
		return nil, nil, errors.New("MAINTAINER_PROFILE must be mock or production")
	}
	if err != nil {
		return nil, nil, err
	}
	revision, err := store.CurrentConfig(context.Background())
	if err != nil {
		return nil, nil, err
	}
	var document appconfig.System
	if err := json.Unmarshal(revision.After, &document); err != nil {
		return nil, nil, err
	}
	coordinator, err := workflow.NewCoordinator(store, gitClient, modelManager, execution, artifactStore,
		worktreesRoot, document.Workflow.MaxReviewCycles)
	if err != nil {
		return nil, nil, err
	}
	return workflow.New(store, coordinator), modelManager, nil
}

func readToken(path string) ([]byte, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	token := []byte(strings.TrimSpace(string(payload)))
	if len(token) < 32 {
		return nil, errors.New("service token must contain at least 32 bytes")
	}
	return token, nil
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
