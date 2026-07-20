package runnerd

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/runners"
)

func TestDockerExecutorTranslatesOnlyHardenedPolicyAndImplementsContract(t *testing.T) {
	root := t.TempDir()
	runID := runners.RunID("run_docker_fixture")
	jobID := "job_docker_fixture"
	for _, directory := range []string{
		filepath.Join(root, "worktrees", jobID), filepath.Join(root, "artifacts", jobID),
		filepath.Join(root, "caches"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var created dockerCreateRequest
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	containerID := strings.Repeat("c", 64)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
			if r.URL.Query().Get("name") != "maintainer-"+string(runID) {
				t.Errorf("unexpected container name %q", r.URL.Query().Get("name"))
			}
			mu.Lock()
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("decode create request: %v", err)
			}
			mu.Unlock()
			writeDockerFixture(w, http.StatusCreated, map[string]any{"Id": containerID, "Warnings": []string{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+containerID+"/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/maintainer-"+string(runID)+"/json":
			writeDockerFixture(w, http.StatusOK, map[string]any{
				"Config": map[string]any{"Labels": map[string]string{
					"maintainer.run_id": string(runID), "maintainer.job_id": jobID,
					"maintainer.project_id": "project", "maintainer.kind": "implementation",
					"maintainer.max_artifact_bytes": "1073741824",
					"maintainer.max_disk_bytes":     "8589934592", "maintainer.disk_baseline_bytes": "0",
					"maintainer.wall_timeout_seconds": "30",
				}},
				"State": map[string]any{"Status": "running", "Running": true, "ExitCode": 0, "StartedAt": startedAt},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/maintainer-"+string(runID)+"/logs":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dockerLogFrame(1, []byte("hello runner\n")))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/maintainer-"+string(runID)+"/stop":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker API request %s %s", r.Method, r.URL.String())
			writeDockerFixture(w, http.StatusNotFound, map[string]string{"message": "not found"})
		}
	})
	socketPath, closeDaemon := dockerFixtureDaemon(t, handler)
	defer closeDaemon()
	executor, err := NewDockerExecutor(socketPath, root, "dependency-egress", "inference-only", "http://model-gateway:8081/v1", "65532:65532")
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	policy, err := NewPolicy(root, testImages())
	if err != nil {
		t.Fatal(err)
	}
	spec, err := policy.Resolve(runners.JobRequest{
		JobID: jobID, ProjectID: "project", Kind: runners.KindImplementation,
	}, runID)
	if err != nil {
		t.Fatal(err)
	}
	spec.WallTimeout = 30 * time.Second
	if err := executor.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	request := created
	mu.Unlock()
	if request.Image != spec.Image || request.User != "65532:65532" || request.WorkingDir != "/workspace" ||
		!request.HostConfig.ReadonlyRootfs || request.HostConfig.Privileged || request.HostConfig.PublishAllPorts ||
		request.HostConfig.NetworkMode != "inference-only" || len(request.HostConfig.CapDrop) != 1 || request.HostConfig.CapDrop[0] != "ALL" ||
		len(request.HostConfig.SecurityOpt) != 1 || request.HostConfig.SecurityOpt[0] != "no-new-privileges" ||
		request.HostConfig.Memory != spec.MemoryBytes || request.HostConfig.PidsLimit != spec.PIDsLimit {
		t.Fatalf("unsafe Docker create translation: %#v", request)
	}
	if request.HostConfig.LogConfig.Config["max-file"] != "1" || request.HostConfig.LogConfig.Config["compress"] != "false" {
		t.Fatalf("worker logs are not bounded compatibly: %#v", request.HostConfig.LogConfig)
	}
	if !strings.Contains(strings.Join(request.Env, "\n"), "MAINTAINER_MODEL_ENDPOINT=http://model-gateway:8081/v1") {
		t.Fatalf("implementation worker lacks the fixed inference endpoint: %#v", request.Env)
	}
	if request.Labels["maintainer.max_disk_bytes"] != "8589934592" || request.Labels["maintainer.disk_baseline_bytes"] != "0" {
		t.Fatalf("worker disk limits are not server-owned labels: %#v", request.Labels)
	}
	status, err := executor.Inspect(context.Background(), runID)
	if err != nil || status.JobID != jobID || status.State != "running" {
		t.Fatalf("inspect returned %#v, %v", status, err)
	}
	logs, err := executor.Logs(context.Background(), runID, 0, 5)
	if err != nil || logs.Data != "hello" || !logs.Truncated {
		t.Fatalf("logs returned %#v, %v", logs, err)
	}
	manifest := []runners.Artifact{{ID: "artifact_one", Kind: "junit", SHA256: strings.Repeat("a", 64), Bytes: 12}}
	artifactDirectory := filepath.Join(root, "artifacts", jobID, string(runID))
	if err := os.Mkdir(artifactDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	artifactPayload := []byte("hello runner")
	manifest[0].SHA256 = fmt.Sprintf("%x", sha256.Sum256(artifactPayload))
	if err := os.WriteFile(filepath.Join(artifactDirectory, manifest[0].ID), artifactPayload, 0o400); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "artifacts", jobID, string(runID)+".manifest.json"), payload, 0o400); err != nil {
		t.Fatal(err)
	}
	items, err := executor.Artifacts(context.Background(), runID)
	if err != nil || len(items) != 1 || items[0].ID != "artifact_one" {
		t.Fatalf("artifacts returned %#v, %v", items, err)
	}
	if err := executor.Stop(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
}

func TestDockerExecutorRejectsUnsafeOrSharedNetworkConfiguration(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		dependency string
		inference  string
		endpoint   string
	}{
		{"same", "same", "http://model-gateway:8081/v1"},
		{"dependency", "inference", "https://public.example/v1"},
		{"dependency", "inference", "http://user:secret@model-gateway:8081/v1"},
		{"dependency", "inference", "http://model-gateway:8081/arbitrary"},
	} {
		if _, err := NewDockerExecutor(filepath.Join(root, "missing.sock"), root,
			test.dependency, test.inference, test.endpoint, "65532:65532"); !errors.Is(err, ErrPolicyDenied) {
			t.Errorf("unsafe network configuration %#v returned %v", test, err)
		}
	}
}

func TestDockerExecutorStopsRunWhenWritableDiskGrowthExceedsLimit(t *testing.T) {
	root := t.TempDir()
	jobID := "job_disk_limit"
	runID := runners.RunID("run_disk_limit")
	for _, directory := range []string{filepath.Join(root, "worktrees", jobID), filepath.Join(root, "artifacts", jobID)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	containerID := strings.Repeat("d", 64)
	stopped := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
			writeDockerFixture(w, http.StatusCreated, map[string]any{"Id": containerID, "Warnings": []string{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+containerID+"/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/maintainer-"+string(runID)+"/stop":
			select {
			case stopped <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker API request %s %s", r.Method, r.URL.String())
			writeDockerFixture(w, http.StatusNotFound, map[string]string{"message": "not found"})
		}
	})
	socketPath, closeDaemon := dockerFixtureDaemon(t, handler)
	defer closeDaemon()
	executor, err := NewDockerExecutor(socketPath, root, "dependency-egress", "inference-only", "http://model-gateway:8081/v1", "65532:65532")
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	policy, _ := NewPolicy(root, testImages())
	spec, _ := policy.Resolve(runners.JobRequest{JobID: jobID, ProjectID: "project", Kind: runners.KindImplementation}, runID)
	spec.MaxDiskBytes = 1
	if err := executor.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worktrees", jobID, "growth"), []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("runnerd did not stop a worker that exceeded its writable disk limit")
	}
}

func TestDockerExecutorRejectsEscapingMountAndPolicyDowngradesBeforeDaemon(t *testing.T) {
	root := t.TempDir()
	jobID := "job_reject"
	for _, directory := range []string{filepath.Join(root, "worktrees", jobID), filepath.Join(root, "artifacts", jobID)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	socketPath, closeDaemon := dockerFixtureDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer closeDaemon()
	executor, err := NewDockerExecutor(socketPath, root, "dependency-egress", "inference-only", "http://model-gateway:8081/v1", "65532:65532")
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	policy, _ := NewPolicy(root, testImages())
	spec, _ := policy.Resolve(runners.JobRequest{JobID: jobID, ProjectID: "project", Kind: runners.KindImplementation}, "run_reject")

	unsafe := spec
	unsafe.Mounts = append([]Mount(nil), spec.Mounts...)
	unsafe.Mounts[0].Source = "/etc"
	if err := executor.Start(context.Background(), unsafe); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("escaping mount returned %v", err)
	}
	unsafe = spec
	unsafe.Network = NetworkDependencyEgress
	if err := executor.Start(context.Background(), unsafe); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("implementation egress returned %v", err)
	}
	unsafe = spec
	unsafe.User = "0:0"
	if err := executor.Start(context.Background(), unsafe); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("root worker returned %v", err)
	}
	unsafe = spec
	unsafe.ReadOnlyRoot = false
	if err := executor.Start(context.Background(), unsafe); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("writable root returned %v", err)
	}
	if requests != 0 {
		t.Fatalf("unsafe specifications reached daemon %d times", requests)
	}
}

func dockerFixtureDaemon(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	return path, func() {
		server.Close()
		<-done
	}
}

func dockerLogFrame(stream byte, payload []byte) []byte {
	result := make([]byte, 8+len(payload))
	result[0] = stream
	binary.BigEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	return result
}

func writeDockerFixture(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
