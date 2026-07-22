package simulator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/windowsworker"
)

type Adapter struct{ Now func() time.Time }

func New() *Adapter { return &Adapter{Now: time.Now} }

func (s *Adapter) ProbeWindowsWorker(_ context.Context, profile windowsworker.Profile) (windowsworker.Probe, error) {
	if profile.Mode != "simulator" {
		return windowsworker.Probe{}, errors.New("the deterministic simulator cannot probe a remote Windows worker")
	}
	return windowsworker.Probe{
		ProfileID: profile.ID, Ready: true, Mode: "simulator", Health: "healthy",
		Capacity: profile.Capacity, VMTemplateID: profile.VMTemplateID,
		Toolchains: cloneStrings(profile.Toolchains), Problems: []string{}, CheckedAt: s.Now().UTC(),
	}, nil
}

func (s *Adapter) RunWindowsJob(_ context.Context, profile windowsworker.Profile, request windowsworker.RunRequest) (windowsworker.Result, error) {
	if profile.Mode != "simulator" {
		return windowsworker.Result{}, errors.New("the deterministic simulator cannot execute a remote Windows worker job")
	}
	started := s.Now().UTC()
	inputSHA := windowsworker.InputSHA(request)
	checkIDs := checks(request.JobType)
	results := make([]windowsworker.Check, 0, len(checkIDs))
	for index, id := range checkIDs {
		results = append(results, windowsworker.Check{ID: id, State: "passed", Summary: "deterministic simulator evidence for " + id, Duration: 10 + index})
	}
	metadata, _ := json.Marshal(map[string]any{"simulated": true, "vm_template_id": profile.VMTemplateID, "repository_sha": request.Input.RepositorySHA, "job_type": request.JobType})
	artifactDigest := sha256.Sum256(append([]byte(inputSHA+":"+request.JobType+":"), metadata...))
	completed := started.Add(time.Duration(len(results)+1) * time.Millisecond)
	return windowsworker.Result{
		RunID: "winrun-" + hex.EncodeToString(artifactDigest[8:16]), ProfileID: profile.ID,
		ProjectID: request.ProjectID, JobID: request.JobID, JobType: request.JobType,
		InputSHA256: inputSHA, State: "completed", Checks: results,
		Artifacts:      []windowsworker.Artifact{{ID: "win-" + hex.EncodeToString(artifactDigest[:8]), Kind: artifactKind(request.JobType), MediaType: "application/json", SHA256: hex.EncodeToString(artifactDigest[:]), Bytes: int64(len(metadata)), Metadata: metadata}},
		IdempotencyKey: request.IdempotencyKey, StartedAt: started, CompletedAt: completed,
	}, nil
}

func checks(jobType string) []string {
	switch jobType {
	case windowsworker.JobDotNet:
		return []string{"restore-locked", "build-release", "test-results"}
	case windowsworker.JobPowerShell:
		return []string{"powershell-analysis", "pester-tests"}
	case windowsworker.JobServiceLifecycle:
		return []string{"service-install", "service-start-stop", "service-recovery", "service-cleanup"}
	case windowsworker.JobInstallerLifecycle:
		return []string{"inno-build", "install", "upgrade", "repair", "uninstall", "residue"}
	case windowsworker.JobHamiltonDiscovery:
		return []string{"hamilton-paths", "hamilton-packages", "hamilton-drivers"}
	case windowsworker.JobReleaseConsistency:
		return []string{"assembly-version", "file-version", "installer-version", "release-metadata"}
	case windowsworker.JobInstallerEvidence:
		return []string{"installer-artifact", "iq-installation-evidence"}
	case windowsworker.JobEquipmentSimulator:
		return []string{"simulator-connect", "protocol-journey", "simulator-reset"}
	case windowsworker.JobVPNWorkflow:
		return []string{"operator-vpn-gate", "vpn-workflow-trigger", "vpn-cleanup"}
	case windowsworker.JobSigningRequest:
		return []string{"operator-signing-gate", "isolated-signing-request"}
	default:
		return []string{"unsupported"}
	}
}

func artifactKind(jobType string) string {
	switch jobType {
	case windowsworker.JobInstallerLifecycle, windowsworker.JobInstallerEvidence:
		return "installation-evidence"
	case windowsworker.JobServiceLifecycle:
		return "service-lifecycle-report"
	case windowsworker.JobEquipmentSimulator:
		return "equipment-simulator-report"
	case windowsworker.JobSigningRequest:
		return "signing-request-receipt"
	default:
		return "windows-verification-report"
	}
}

func cloneStrings(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
