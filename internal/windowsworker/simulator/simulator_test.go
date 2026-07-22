package simulator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker/simulator"
)

func TestSimulatorProducesDeterministicInstallerAndIQEvidence(t *testing.T) {
	adapter := simulator.New()
	adapter.Now = func() time.Time { return time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC) }
	profile := windowsworker.Profile{ID: "windows-sim", Mode: "simulator", Capacity: 2, VMTemplateID: "windows-2022-sim-v1", Toolchains: map[string]string{"dotnet-sdk": "8.0.302"}}
	request := windowsworker.RunRequest{ProfileID: profile.ID, ProjectID: "project-one", JobID: "job-one", JobType: windowsworker.JobInstallerLifecycle, IdempotencyKey: "sim-run-one", Input: windowsworker.ImmutableInput{RepositorySHA: strings.Repeat("a", 40), CapabilityPackChecksum: strings.Repeat("b", 64), ToolchainInventoryChecksum: strings.Repeat("c", 64), SourceArtifactID: "source-one"}}
	first, err := adapter.RunWindowsJob(context.Background(), profile, request)
	if err != nil || len(first.Checks) != 6 || first.Artifacts[0].Kind != "installation-evidence" {
		t.Fatalf("first = %#v err=%v", first, err)
	}
	second, err := adapter.RunWindowsJob(context.Background(), profile, request)
	if err != nil || first.RunID != second.RunID || first.InputSHA256 != second.InputSHA256 || first.Artifacts[0].SHA256 != second.Artifacts[0].SHA256 {
		t.Fatalf("deterministic results differ: %#v %#v err=%v", first, second, err)
	}
}
