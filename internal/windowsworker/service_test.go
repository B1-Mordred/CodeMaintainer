package windowsworker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type memoryStore struct {
	profiles map[string]Profile
	runs     map[string]Result
}

func (s *memoryStore) GetWindowsWorkerProfile(_ context.Context, id string) (Profile, error) {
	profile, ok := s.profiles[id]
	if !ok {
		return Profile{}, errors.New("not found")
	}
	return profile, nil
}
func (s *memoryStore) ListWindowsWorkerProfiles(context.Context, int) ([]Profile, error) {
	result := []Profile{}
	for _, profile := range s.profiles {
		result = append(result, profile)
	}
	return result, nil
}
func (s *memoryStore) SaveWindowsWorkerProfile(_ context.Context, request SaveProfileRequest) (Profile, error) {
	request.Profile.Revision = request.ExpectedRevision + 1
	s.profiles[request.Profile.ID] = request.Profile
	return request.Profile, nil
}
func (s *memoryStore) SaveWindowsWorkerResult(_ context.Context, request RunRequest, result Result) (Result, error) {
	if existing, ok := s.runs[request.IdempotencyKey]; ok {
		existing.Replay = true
		return existing, nil
	}
	s.runs[request.IdempotencyKey] = result
	return result, nil
}
func (s *memoryStore) ListWindowsWorkerResults(_ context.Context, profileID string, _ int) ([]Result, error) {
	result := []Result{}
	for _, run := range s.runs {
		if run.ProfileID == profileID {
			result = append(result, run)
		}
	}
	return result, nil
}

func TestSimulatorRunsOnlyApprovedImmutableJobsDeterministically(t *testing.T) {
	profile := simulatorProfile()
	store := &memoryStore{profiles: map[string]Profile{profile.ID: profile}, runs: map[string]Result{}}
	simulated := &fixtureAdapter{now: func() time.Time { return time.Date(2026, 7, 22, 8, 30, 0, 0, time.UTC) }}
	service, err := NewService(store, simulated)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := service.Probe(context.Background(), profile.ID)
	if err != nil || !probe.Ready || probe.Mode != "simulator" || probe.Toolchains["dotnet-sdk"] != "8.0.302" {
		t.Fatalf("probe = %#v err=%v", probe, err)
	}
	request := validRun(profile.ID, JobInstallerLifecycle)
	first, err := service.Run(context.Background(), request)
	if err != nil || first.State != "completed" || len(first.Checks) != 6 || first.Artifacts[0].Kind != "installation-evidence" {
		t.Fatalf("first run = %#v err=%v", first, err)
	}
	second, err := service.Run(context.Background(), request)
	if err != nil || !second.Replay || second.RunID != first.RunID || second.InputSHA256 != first.InputSHA256 {
		t.Fatalf("replay = %#v err=%v", second, err)
	}
	request.JobType = "powershell_arbitrary"
	if _, err := service.Run(context.Background(), request); err == nil {
		t.Fatal("unregistered worker operation was accepted")
	}
	request = validRun(profile.ID, JobEquipmentSimulator)
	request.Input.SimulatorProfileID = "unregistered-hardware"
	if _, err := service.Run(context.Background(), request); err == nil {
		t.Fatal("unregistered simulator endpoint was accepted")
	}
}

func TestProfilesFailClosedOnEndpointAuthorityAndManualGates(t *testing.T) {
	profile := simulatorProfile()
	if err := ValidateProfile(profile); err != nil {
		t.Fatal(err)
	}
	profile.Endpoint = "https://attacker.invalid"
	if err := ValidateProfile(profile); err == nil {
		t.Fatal("simulator accepted a mutable remote endpoint")
	}
	remote := simulatorProfile()
	remote.ID, remote.Mode = "windows-remote", "remote"
	remote.Endpoint = "https://windows-worker.example.test"
	remote.EndpointAllowlist = []string{remote.Endpoint}
	remote.CredentialReference = "worker-secret:windows-main"
	remote.ManualGates.CodeSigning = true
	remote.SigningPolicyReference = "release-signing-policy"
	store := &memoryStore{profiles: map[string]Profile{}, runs: map[string]Result{}}
	service, _ := NewService(store, &fixtureAdapter{now: time.Now})
	if _, err := service.SaveProfile(context.Background(), SaveProfileRequest{Profile: remote, Reason: "enable signing", ActorID: "admin"}); err == nil {
		t.Fatal("credential and signing gate change did not require reauthentication")
	}
	if _, err := service.SaveProfile(context.Background(), SaveProfileRequest{Profile: remote, Reason: "enable signing", ActorID: "admin", Reauthenticated: true}); err != nil {
		t.Fatal(err)
	}
	request := validRun(remote.ID, JobSigningRequest)
	request.OperatorGated, request.ActorRole = false, "operator"
	if _, err := service.Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "administrator operator gate") {
		t.Fatalf("ungated signing error = %v", err)
	}
}

func simulatorProfile() Profile {
	return Profile{
		ID: "windows-sim", Name: "Deterministic Windows simulator", Mode: "simulator",
		Endpoint: "simulator://windows-worker", EndpointAllowlist: []string{"simulator://windows-worker"},
		CredentialStatus: "not_required", Health: "healthy", Capacity: 2, VMTemplateID: "windows-2022-sim-v1",
		Toolchains:      map[string]string{"dotnet-sdk": "8.0.302", "powershell": "7.4.4", "pester": "5.6.1", "inno-setup": "6.3.3"},
		AllowedJobTypes: append([]string(nil), ApprovedJobTypes...), TimeoutSeconds: 1800,
		SimulatorProfileIDs: []string{"hamilton-sim-v1", "instrument-sim-v1"}, ArtifactRetentionDays: 30,
		ManualGates: ManualGates{}, Enabled: true, Revision: 1,
	}
}

func validRun(profileID, jobType string) RunRequest {
	return RunRequest{
		ProfileID: profileID, ProjectID: "project-one", JobID: "job-one", JobType: jobType,
		IdempotencyKey: "windows-run-one", ActorID: "operator", ActorRole: "operator",
		Input: ImmutableInput{
			RepositorySHA: strings.Repeat("a", 40), CapabilityPackChecksum: strings.Repeat("b", 64),
			ToolchainInventoryChecksum: strings.Repeat("c", 64), SourceArtifactID: "source-artifact-one",
			ReleaseVersion: "1.2.3", ExpectedServiceName: "CodeMaintainerFixture",
			HamiltonProfileID: "hamilton-sim-v1", SimulatorProfileID: "instrument-sim-v1",
		},
	}
}

type fixtureAdapter struct{ now func() time.Time }

func (a *fixtureAdapter) ProbeWindowsWorker(_ context.Context, profile Profile) (Probe, error) {
	return Probe{ProfileID: profile.ID, Ready: true, Mode: profile.Mode, Health: "healthy", Capacity: profile.Capacity, VMTemplateID: profile.VMTemplateID, Toolchains: profile.Toolchains, Problems: []string{}, CheckedAt: a.now().UTC()}, nil
}

func (a *fixtureAdapter) RunWindowsJob(_ context.Context, profile Profile, request RunRequest) (Result, error) {
	now := a.now().UTC()
	count := 3
	if request.JobType == JobInstallerLifecycle {
		count = 6
	}
	checks := make([]Check, count)
	for index := range checks {
		checks[index] = Check{ID: "check-" + string(rune('a'+index)), State: "passed", Summary: "fixture", Duration: 1}
	}
	metadata := json.RawMessage(`{"simulated":true}`)
	return Result{RunID: "windows-fixture-run", ProfileID: profile.ID, ProjectID: request.ProjectID, JobID: request.JobID, JobType: request.JobType, InputSHA256: InputSHA(request), State: "completed", Checks: checks, Artifacts: []Artifact{{ID: "windows-fixture-artifact", Kind: "installation-evidence", MediaType: "application/json", SHA256: strings.Repeat("d", 64), Bytes: int64(len(metadata)), Metadata: metadata}}, IdempotencyKey: request.IdempotencyKey, StartedAt: now, CompletedAt: now.Add(time.Millisecond)}, nil
}
