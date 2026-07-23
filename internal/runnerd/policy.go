package runnerd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
)

const maxPolicyFileBytes = 64 << 10

var (
	ErrPolicyDenied = errors.New("runner policy denied request")
	safeID          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	digestImage     = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9._/:@-]*@)?sha256:[a-f0-9]{64}$`)
	numericUser     = regexp.MustCompile(`^[1-9][0-9]{0,4}:[1-9][0-9]{0,4}$`)
)

type Network string

const (
	NetworkNone             Network = "none"
	NetworkDependencyEgress Network = "dependency-egress"
	NetworkInferenceOnly    Network = "inference-only"
)

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// WorkerSpec is constructed entirely by runnerd. It is never decoded from an
// API request and deliberately contains no caller-controlled command, image,
// mount, network, capability, user, or environment field.
type WorkerSpec struct {
	RunID             runners.RunID
	JobID             string
	ProjectID         string
	Kind              runners.Kind
	Image             string
	User              string
	ReadOnlyRoot      bool
	Network           Network
	Mounts            []Mount
	TmpfsBytes        int64
	MemoryBytes       int64
	NanoCPUs          int64
	PIDsLimit         int64
	WallTimeout       time.Duration
	MaxLogBytes       int64
	MaxArtifactBytes  int64
	MaxDiskBytes      int64
	DropCapabilities  []string
	NoNewPrivileges   bool
	UseDefaultSeccomp bool
}

type profile struct {
	network          Network
	worktreeReadOnly bool
	memoryBytes      int64
	nanoCPUs         int64
	pidsLimit        int64
	timeout          time.Duration
	tmpfsBytes       int64
	maxLogBytes      int64
	maxArtifactBytes int64
	maxDiskBytes     int64
}

type Policy struct {
	dataRoot   string
	workerUser string
	images     map[runners.Kind]string
	profiles   map[runners.Kind]profile
}

type policyFile struct {
	SchemaVersion int               `json:"schema_version"`
	WorkerUser    string            `json:"worker_user"`
	Images        map[string]string `json:"images"`
}

// LoadPolicyFile reads the bootstrap-controlled immutable worker allow-list.
// The file is deliberately separate from controller-managed configuration so
// neither a browser request nor repository content can select an image.
func LoadPolicyFile(path, dataRoot string) (*Policy, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: policy file path must be absolute", ErrPolicyDenied)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect runner policy file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%w: runner policy file must be regular and not writable by group or other", ErrPolicyDenied)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open runner policy file: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxPolicyFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read runner policy file: %w", err)
	}
	if len(payload) > maxPolicyFileBytes {
		return nil, fmt.Errorf("%w: runner policy file is oversized", ErrPolicyDenied)
	}
	var document policyFile
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: decode runner policy file: %v", ErrPolicyDenied, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: runner policy file must contain one JSON document", ErrPolicyDenied)
	}
	if document.SchemaVersion != 1 || len(document.Images) != 5 {
		return nil, fmt.Errorf("%w: runner policy file must use schema version 1 and define exactly five images", ErrPolicyDenied)
	}
	images := make(map[runners.Kind]string, 4)
	for key, image := range document.Images {
		kind := runners.Kind(key)
		if !kind.Valid() {
			return nil, fmt.Errorf("%w: unknown worker image kind %q", ErrPolicyDenied, key)
		}
		images[kind] = image
	}
	return newPolicy(dataRoot, document.WorkerUser, images)
}

func NewPolicy(dataRoot string, images map[runners.Kind]string) (*Policy, error) {
	return newPolicy(dataRoot, "65532:65532", images)
}

func newPolicy(dataRoot, workerUser string, images map[runners.Kind]string) (*Policy, error) {
	root, err := filepath.Abs(dataRoot)
	if err != nil || !filepath.IsAbs(dataRoot) || !numericUser.MatchString(workerUser) {
		return nil, fmt.Errorf("%w: data root and non-root numeric worker user are required", ErrPolicyDenied)
	}
	root = filepath.Clean(root)
	requiredKinds := []runners.Kind{
		runners.KindDependencies, runners.KindImplementation,
		runners.KindVerification, runners.KindQC, runners.KindDocumentation,
	}
	ownedImages := make(map[runners.Kind]string, len(requiredKinds))
	for _, kind := range requiredKinds {
		image := images[kind]
		if !digestImage.MatchString(image) {
			return nil, fmt.Errorf("%w: %s image must use an immutable sha256 digest", ErrPolicyDenied, kind)
		}
		ownedImages[kind] = image
	}
	return &Policy{
		dataRoot: root, workerUser: workerUser,
		images: ownedImages,
		profiles: map[runners.Kind]profile{
			runners.KindDependencies: {
				network: NetworkDependencyEgress, memoryBytes: 4 << 30, nanoCPUs: 2_000_000_000,
				pidsLimit: 512, timeout: 30 * time.Minute, tmpfsBytes: 512 << 20,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30, maxDiskBytes: 4 << 30,
			},
			runners.KindImplementation: {
				network: NetworkInferenceOnly, memoryBytes: 16 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 512, timeout: 2 * time.Hour, tmpfsBytes: 1 << 30,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30, maxDiskBytes: 8 << 30,
			},
			runners.KindVerification: {
				network: NetworkNone, memoryBytes: 8 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 1024, timeout: time.Hour, tmpfsBytes: 2 << 30,
				maxLogBytes: 16 << 20, maxArtifactBytes: 2 << 30, maxDiskBytes: 8 << 30,
			},
			runners.KindQC: {
				network: NetworkInferenceOnly, worktreeReadOnly: true, memoryBytes: 16 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 512, timeout: time.Hour, tmpfsBytes: 1 << 30,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30, maxDiskBytes: 4 << 30,
			},
			runners.KindDocumentation: {
				network: NetworkInferenceOnly, memoryBytes: 16 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 512, timeout: time.Hour, tmpfsBytes: 1 << 30,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30, maxDiskBytes: 4 << 30,
			},
		},
	}, nil
}

func (p *Policy) WorkerUser() string {
	if p == nil {
		return ""
	}
	return p.workerUser
}

func (p *Policy) Resolve(request runners.JobRequest, runID runners.RunID) (WorkerSpec, error) {
	if p == nil || !safeIdentifier(request.JobID) || !safeIdentifier(request.ProjectID) || !request.Kind.Valid() || !safeIdentifier(string(runID)) {
		return WorkerSpec{}, fmt.Errorf("%w: invalid job, project, kind, or run identifier", ErrPolicyDenied)
	}
	if len(request.InputArtifactIDs) > 32 {
		return WorkerSpec{}, fmt.Errorf("%w: at most 32 input artifacts are allowed", ErrPolicyDenied)
	}
	seenArtifacts := make(map[string]struct{}, len(request.InputArtifactIDs))
	for _, artifactID := range request.InputArtifactIDs {
		if !safeIdentifier(artifactID) {
			return WorkerSpec{}, fmt.Errorf("%w: invalid artifact identifier", ErrPolicyDenied)
		}
		if _, exists := seenArtifacts[artifactID]; exists {
			return WorkerSpec{}, fmt.Errorf("%w: duplicate artifact identifier", ErrPolicyDenied)
		}
		seenArtifacts[artifactID] = struct{}{}
	}
	selected := p.profiles[request.Kind]
	worktree, err := p.path("worktrees", request.JobID)
	if err != nil {
		return WorkerSpec{}, err
	}
	artifacts, err := p.path("artifacts", request.JobID)
	if err != nil {
		return WorkerSpec{}, err
	}
	mounts := []Mount{
		{Source: worktree, Target: "/workspace", ReadOnly: selected.worktreeReadOnly},
		{Source: artifacts, Target: "/artifacts", ReadOnly: false},
	}
	if request.Kind == runners.KindDependencies {
		cache, pathErr := p.path("caches", request.ProjectID)
		if pathErr != nil {
			return WorkerSpec{}, pathErr
		}
		mounts = append(mounts, Mount{Source: cache, Target: "/cache", ReadOnly: false})
	}
	for index, artifactID := range request.InputArtifactIDs {
		// The trusted controller stages authorized object hardlinks in a
		// per-job input directory. runnerd never resolves a caller-provided
		// artifact identifier directly into the global object store.
		input, pathErr := p.path("artifacts", "inputs", request.JobID, artifactID)
		if pathErr != nil {
			return WorkerSpec{}, pathErr
		}
		mounts = append(mounts, Mount{Source: input, Target: fmt.Sprintf("/inputs/%02d", index), ReadOnly: true})
	}
	return WorkerSpec{
		RunID: runID, JobID: request.JobID, ProjectID: request.ProjectID, Kind: request.Kind,
		Image: p.images[request.Kind], User: p.workerUser, ReadOnlyRoot: true,
		Network: selected.network, Mounts: mounts, TmpfsBytes: selected.tmpfsBytes,
		MemoryBytes: selected.memoryBytes, NanoCPUs: selected.nanoCPUs, PIDsLimit: selected.pidsLimit,
		WallTimeout: selected.timeout, MaxLogBytes: selected.maxLogBytes, MaxArtifactBytes: selected.maxArtifactBytes,
		MaxDiskBytes:     selected.maxDiskBytes,
		DropCapabilities: []string{"ALL"}, NoNewPrivileges: true, UseDefaultSeccomp: true,
	}, nil
}

func (p *Policy) path(parts ...string) (string, error) {
	result := filepath.Join(append([]string{p.dataRoot}, parts...)...)
	relative, err := filepath.Rel(p.dataRoot, result)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: resolved path escapes data root", ErrPolicyDenied)
	}
	return result, nil
}

func safeIdentifier(value string) bool {
	return value != "." && value != ".." && safeID.MatchString(value)
}
