package windowsworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	safeID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	commitSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
	checksum  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	version   = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)
)

type Store interface {
	GetWindowsWorkerProfile(context.Context, string) (Profile, error)
	ListWindowsWorkerProfiles(context.Context, int) ([]Profile, error)
	SaveWindowsWorkerProfile(context.Context, SaveProfileRequest) (Profile, error)
	SaveWindowsWorkerResult(context.Context, RunRequest, Result) (Result, error)
	ListWindowsWorkerResults(context.Context, string, int) ([]Result, error)
}

type Adapter interface {
	ProbeWindowsWorker(context.Context, Profile) (Probe, error)
	RunWindowsJob(context.Context, Profile, RunRequest) (Result, error)
}

type Service struct {
	store   Store
	adapter Adapter
}

func NewService(store Store, adapter Adapter) (*Service, error) {
	if store == nil || adapter == nil {
		return nil, errors.New("Windows worker store and adapter are required")
	}
	return &Service{store: store, adapter: adapter}, nil
}

func (s *Service) Profiles(ctx context.Context) ([]Profile, error) {
	return s.store.ListWindowsWorkerProfiles(ctx, 100)
}

func (s *Service) Profile(ctx context.Context, id string) (Profile, error) {
	return s.store.GetWindowsWorkerProfile(ctx, id)
}

func (s *Service) SaveProfile(ctx context.Context, request SaveProfileRequest) (Profile, error) {
	current, currentErr := s.store.GetWindowsWorkerProfile(ctx, request.Profile.ID)
	if request.Profile.Mode == "remote" && request.Profile.CredentialReference == "" && currentErr == nil {
		request.Profile.CredentialReference = current.CredentialReference
	}
	if err := validateProfile(request.Profile); err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		return Profile{}, errors.New("a bounded worker-profile reason is required")
	}
	credentialChanged := current.CredentialReference != request.Profile.CredentialReference
	gateRaised := (!current.ManualGates.CodeSigning && request.Profile.ManualGates.CodeSigning) ||
		(!current.ManualGates.PhysicalHardware && request.Profile.ManualGates.PhysicalHardware) ||
		(!current.ManualGates.ProductionVPN && request.Profile.ManualGates.ProductionVPN)
	if (credentialChanged || gateRaised || current.SigningPolicyReference != request.Profile.SigningPolicyReference) && !request.Reauthenticated {
		return Profile{}, errors.New("recent reauthentication is required for worker credentials, signing policy, or manual gates")
	}
	return s.store.SaveWindowsWorkerProfile(ctx, request)
}

func (s *Service) Probe(ctx context.Context, id string) (Probe, error) {
	profile, err := s.store.GetWindowsWorkerProfile(ctx, id)
	if err != nil {
		return Probe{}, err
	}
	if !profile.Enabled {
		return Probe{}, errors.New("Windows worker profile is disabled")
	}
	probe, err := s.adapter.ProbeWindowsWorker(ctx, profile)
	if err != nil {
		return Probe{}, err
	}
	if probe.ProfileID != profile.ID || probe.Mode != profile.Mode || probe.Capacity < 0 || len(probe.Toolchains) > 50 {
		return Probe{}, errors.New("Windows worker returned an invalid probe")
	}
	return probe, nil
}

func (s *Service) Run(ctx context.Context, request RunRequest) (Result, error) {
	profile, err := s.store.GetWindowsWorkerProfile(ctx, request.ProfileID)
	if err != nil {
		return Result{}, err
	}
	if !profile.Enabled || !contains(profile.AllowedJobTypes, request.JobType) {
		return Result{}, errors.New("Windows worker job type is not enabled for this profile")
	}
	if err := validateRun(profile, request); err != nil {
		return Result{}, err
	}
	result, err := s.adapter.RunWindowsJob(ctx, profile, request)
	if err != nil {
		return Result{}, err
	}
	if err := validateResult(profile, request, result); err != nil {
		return Result{}, err
	}
	return s.store.SaveWindowsWorkerResult(ctx, request, result)
}

func (s *Service) Runs(ctx context.Context, profileID string) ([]Result, error) {
	return s.store.ListWindowsWorkerResults(ctx, profileID, 100)
}

func (s *Service) EnsureSimulatorProfile(ctx context.Context, actorID string) (Profile, error) {
	profile, err := s.store.GetWindowsWorkerProfile(ctx, DefaultSimulatorProfile().ID)
	if err == nil {
		return profile, nil
	}
	profile = DefaultSimulatorProfile()
	return s.SaveProfile(ctx, SaveProfileRequest{
		Profile: profile, ActorID: actorID, Reason: "bootstrap deterministic Windows worker simulator", Reauthenticated: true,
	})
}

func validateProfile(profile Profile) error {
	if !safeID.MatchString(profile.ID) || strings.TrimSpace(profile.Name) == "" || len(profile.Name) > 128 || (profile.Mode != "simulator" && profile.Mode != "remote") {
		return errors.New("invalid Windows worker profile identity")
	}
	if profile.Capacity < 1 || profile.Capacity > 64 || profile.TimeoutSeconds < 30 || profile.TimeoutSeconds > 86400 || profile.ArtifactRetentionDays < 1 || profile.ArtifactRetentionDays > 3650 {
		return errors.New("Windows worker capacity, timeout, or retention is out of bounds")
	}
	if !safeID.MatchString(profile.VMTemplateID) || len(profile.Toolchains) == 0 || len(profile.Toolchains) > 50 || len(profile.AllowedJobTypes) == 0 || len(profile.AllowedJobTypes) > len(ApprovedJobTypes) || len(profile.SimulatorProfileIDs) > 50 {
		return errors.New("Windows worker inventory or approved jobs are invalid")
	}
	for name, value := range profile.Toolchains {
		if !safeID.MatchString(name) || !version.MatchString(value) {
			return errors.New("Windows worker toolchain inventory is invalid")
		}
	}
	seen := map[string]bool{}
	for _, jobType := range profile.AllowedJobTypes {
		if !approvedJobType(jobType) || seen[jobType] {
			return errors.New("Windows worker allowed job types are invalid")
		}
		seen[jobType] = true
	}
	for _, simulator := range profile.SimulatorProfileIDs {
		if !safeID.MatchString(simulator) {
			return errors.New("Windows worker simulator profile is invalid")
		}
	}
	if profile.Mode == "simulator" {
		if profile.Endpoint != "simulator://windows-worker" || len(profile.EndpointAllowlist) != 1 || profile.EndpointAllowlist[0] != profile.Endpoint || profile.CredentialReference != "" || profile.ManualGates != (ManualGates{}) {
			return errors.New("simulated Windows worker must use the fixed credential-free endpoint with every operator gate disabled")
		}
	} else if err := validateHostedEndpoint(profile.Endpoint, profile.EndpointAllowlist); err != nil {
		return err
	}
	if profile.Mode == "remote" && !safeID.MatchString(profile.CredentialReference) {
		return errors.New("remote Windows worker requires a safe opaque credential reference")
	}
	if len(profile.SigningPolicyReference) > 128 || (profile.SigningPolicyReference != "" && !safeID.MatchString(profile.SigningPolicyReference)) {
		return errors.New("Windows worker signing policy reference is invalid")
	}
	return nil
}

func validateHostedEndpoint(endpoint string, allowlist []string) error {
	if len(allowlist) == 0 || len(allowlist) > 20 {
		return errors.New("a bounded Windows worker endpoint allow-list is required")
	}
	matched := false
	for _, candidate := range allowlist {
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("invalid Windows worker endpoint")
		}
		loopback := parsed.Hostname() == "localhost"
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			loopback = true
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
			return errors.New("remote Windows worker endpoints require HTTPS except loopback tests")
		}
		if candidate == endpoint {
			matched = true
		}
	}
	if !matched {
		return errors.New("Windows worker endpoint is outside its exact allow-list")
	}
	return nil
}

func validateRun(profile Profile, request RunRequest) error {
	if !safeID.MatchString(request.ProfileID) || !safeID.MatchString(request.ProjectID) || !safeID.MatchString(request.JobID) || !safeID.MatchString(request.IdempotencyKey) || request.ProfileID != profile.ID || !approvedJobType(request.JobType) {
		return errors.New("invalid Windows worker run identity")
	}
	input := request.Input
	if !commitSHA.MatchString(input.RepositorySHA) || !checksum.MatchString(input.CapabilityPackChecksum) || !checksum.MatchString(input.ToolchainInventoryChecksum) || !safeID.MatchString(input.SourceArtifactID) {
		return errors.New("invalid immutable Windows worker input identity")
	}
	for _, optional := range []string{input.PreviousInstallerArtifact, input.ExpectedServiceName, input.HamiltonProfileID, input.SimulatorProfileID} {
		if optional != "" && !safeID.MatchString(optional) {
			return errors.New("invalid immutable Windows worker profile reference")
		}
	}
	if input.ReleaseVersion != "" && !version.MatchString(input.ReleaseVersion) {
		return errors.New("invalid Windows release version")
	}
	if input.SimulatorProfileID != "" && !contains(profile.SimulatorProfileIDs, input.SimulatorProfileID) {
		return errors.New("unregistered Windows simulator profile")
	}
	if input.HamiltonProfileID != "" && !contains(profile.SimulatorProfileIDs, input.HamiltonProfileID) {
		return errors.New("unregistered HAMILTON discovery profile")
	}
	switch request.JobType {
	case JobServiceLifecycle:
		if input.ExpectedServiceName == "" {
			return errors.New("Windows service lifecycle requires an expected service name")
		}
	case JobHamiltonDiscovery:
		if input.HamiltonProfileID == "" {
			return errors.New("HAMILTON discovery requires a registered profile")
		}
	case JobEquipmentSimulator:
		if input.SimulatorProfileID == "" {
			return errors.New("equipment simulation requires a registered simulator profile")
		}
	case JobReleaseConsistency:
		if input.ReleaseVersion == "" {
			return errors.New("release consistency requires a release version")
		}
	case JobSigningRequest:
		if input.ReleaseVersion == "" {
			return errors.New("Windows signing requires a release version")
		}
	}
	requiresGate := request.JobType == JobSigningRequest || request.JobType == JobVPNWorkflow || (request.JobType == JobEquipmentSimulator && input.SimulatorProfileID == "physical-hardware")
	if requiresGate && (!request.OperatorGated || request.ActorRole != "administrator") {
		return errors.New("Windows signing, VPN, and physical hardware jobs require an administrator operator gate")
	}
	if request.JobType == JobSigningRequest && (!profile.ManualGates.CodeSigning || profile.SigningPolicyReference == "") {
		return errors.New("Windows signing is disabled by profile policy")
	}
	if request.JobType == JobVPNWorkflow && !profile.ManualGates.ProductionVPN {
		return errors.New("Windows production VPN is disabled by profile policy")
	}
	return nil
}

func validateResult(profile Profile, request RunRequest, result Result) error {
	expectedInput := canonicalInputSHA(request)
	if !safeID.MatchString(result.RunID) || result.ProfileID != profile.ID || result.ProjectID != request.ProjectID || result.JobID != request.JobID || result.JobType != request.JobType || result.InputSHA256 != expectedInput || result.IdempotencyKey != request.IdempotencyKey || (result.State != "completed" && result.State != "failed") || result.CompletedAt.Before(result.StartedAt) || len(result.Checks) > 100 || len(result.Artifacts) > 100 {
		return errors.New("Windows worker returned an invalid result")
	}
	for _, check := range result.Checks {
		if !safeID.MatchString(check.ID) || (check.State != "passed" && check.State != "failed" && check.State != "skipped") || len(check.Summary) > 4096 || check.Duration < 0 || check.Duration > profile.TimeoutSeconds*1000 {
			return errors.New("Windows worker returned an invalid check")
		}
	}
	for _, artifact := range result.Artifacts {
		if !safeID.MatchString(artifact.ID) || !safeID.MatchString(artifact.Kind) || len(artifact.MediaType) > 128 || !checksum.MatchString(artifact.SHA256) || artifact.Bytes < 0 || artifact.Bytes > 256<<20 || len(artifact.Metadata) > 64<<10 || !json.Valid(artifact.Metadata) {
			return errors.New("Windows worker returned an invalid artifact")
		}
	}
	return nil
}

func canonicalInputSHA(request RunRequest) string {
	payload, _ := json.Marshal(struct {
		ProfileID string         `json:"profile_id"`
		ProjectID string         `json:"project_id"`
		JobID     string         `json:"job_id"`
		JobType   string         `json:"job_type"`
		Input     ImmutableInput `json:"input"`
	}{request.ProfileID, request.ProjectID, request.JobID, request.JobType, request.Input})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func approvedJobType(value string) bool { return contains(ApprovedJobTypes, value) }

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func SortedJobTypes(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func InputSHA(request RunRequest) string { return canonicalInputSHA(request) }

func ValidateProfile(profile Profile) error { return validateProfile(profile) }

func ValidateResult(profile Profile, request RunRequest, result Result) error {
	return validateResult(profile, request, result)
}
