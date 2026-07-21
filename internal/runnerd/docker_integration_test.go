package runnerd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
)

func TestDockerExecutorRunsOfflineGoFixture(t *testing.T) {
	if os.Getenv("RUNNERD_DOCKER_INTEGRATION") != "1" {
		t.Skip("set RUNNERD_DOCKER_INTEGRATION=1 only for the explicit Docker integration profile")
	}
	socket := os.Getenv("RUNNERD_DOCKER_SOCKET")
	image := os.Getenv("RUNNERD_TEST_IMAGE")
	rootParent := os.Getenv("RUNNERD_TEST_ROOT")
	if socket == "" || image == "" || rootParent == "" || !filepath.IsAbs(rootParent) {
		t.Fatal("integration profile requires absolute socket, immutable image ID, and host-visible test root")
	}
	root, err := os.MkdirTemp(rootParent, "runnerd-integration-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	jobID := "job_integration"
	runID := runners.RunID("run_integration_" + filepath.Base(root))
	worktree := filepath.Join(root, "worktrees", jobID)
	artifacts := filepath.Join(root, "artifacts", jobID)
	inputs := filepath.Join(root, "artifacts", "inputs", jobID)
	for _, directory := range []string{worktree, artifacts, inputs} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(worktree, "go.mod"):          "module fixture.local/offline\n\ngo 1.25\n",
		filepath.Join(worktree, "answer.go"):       "package answer\n\nfunc Value() int { return 42 }\n",
		filepath.Join(worktree, "answer_test.go"):  "package answer\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 42 {\n\t\tt.Fatal(Value())\n\t}\n}\n",
		filepath.Join(inputs, "verification_task"): `{"schema_version":1,"language":"go","classes":["format","compile","lint","full_tests"],"command_timeout_seconds":60,"max_log_bytes":1048576}`,
	}
	for path, payload := range files {
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	images := map[runners.Kind]string{
		runners.KindDependencies: image, runners.KindImplementation: image,
		runners.KindVerification: image, runners.KindQC: image,
	}
	workerUser := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	policy, err := newPolicy(root, workerUser, images)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewDockerExecutor(socket, root, "maintainer-dependency-egress", "maintainer-inference-only", "http://model-gateway:8081/v1", workerUser)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	defer executor.client.json(context.Background(), "DELETE", "/containers/maintainer-"+string(runID)+"?force=1", nil, nil)
	spec, err := policy.Resolve(runners.JobRequest{
		JobID: jobID, ProjectID: "fixture", Kind: runners.KindVerification,
		InputArtifactIDs: []string{"verification_task"},
	}, runID)
	if err != nil {
		t.Fatal(err)
	}
	spec.MemoryBytes = 2 << 30
	spec.NanoCPUs = 2_000_000_000
	spec.PIDsLimit = 256
	spec.WallTimeout = 2 * time.Minute
	spec.TmpfsBytes = 512 << 20
	spec.MaxLogBytes = 2 << 20
	spec.MaxArtifactBytes = 16 << 20
	if err := executor.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		status, inspectErr := executor.Inspect(context.Background(), runID)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}
		if status.State == "completed" {
			break
		}
		if status.State == "failed" || time.Now().After(deadline) {
			logs, _ := executor.Logs(context.Background(), runID, 0, 64<<10)
			artifactItems, artifactErr := executor.Artifacts(context.Background(), runID)
			artifactPayload, _ := os.ReadFile(filepath.Join(artifacts, string(runID), "command_results"))
			t.Fatalf("offline worker ended as %s: logs=%s artifacts=%#v artifact_error=%v report=%s",
				status.State, logs.Data, artifactItems, artifactErr, artifactPayload)
		}
		time.Sleep(100 * time.Millisecond)
	}
	items, err := executor.Artifacts(context.Background(), runID)
	if err != nil || len(items) != 1 || items[0].ID != "command_results" {
		t.Fatalf("artifacts returned %#v, %v", items, err)
	}
	payload, err := os.ReadFile(filepath.Join(artifacts, string(runID), "command_results"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Passed bool `json:"passed"`
	}
	if err := json.Unmarshal(payload, &report); err != nil || !report.Passed {
		t.Fatalf("command report returned %s, %v", payload, err)
	}
}
