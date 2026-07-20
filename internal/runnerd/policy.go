package runnerd

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/runners"
)

var (
	ErrPolicyDenied = errors.New("runner policy denied request")
	safeID          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	digestImage     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*@sha256:[a-f0-9]{64}$`)
)

type Network string

const (
	NetworkNone             Network = "none"
	NetworkDependencyEgress Network = "dependency-egress"
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
}

type Policy struct {
	dataRoot string
	images   map[runners.Kind]string
	profiles map[runners.Kind]profile
}

func NewPolicy(dataRoot string, images map[runners.Kind]string) (*Policy, error) {
	root, err := filepath.Abs(dataRoot)
	if err != nil || !filepath.IsAbs(dataRoot) {
		return nil, fmt.Errorf("%w: data root must be absolute", ErrPolicyDenied)
	}
	root = filepath.Clean(root)
	requiredKinds := []runners.Kind{
		runners.KindDependencies, runners.KindImplementation,
		runners.KindVerification, runners.KindQC,
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
		dataRoot: root,
		images:   ownedImages,
		profiles: map[runners.Kind]profile{
			runners.KindDependencies: {
				network: NetworkDependencyEgress, memoryBytes: 4 << 30, nanoCPUs: 2_000_000_000,
				pidsLimit: 512, timeout: 30 * time.Minute, tmpfsBytes: 512 << 20,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30,
			},
			runners.KindImplementation: {
				network: NetworkNone, memoryBytes: 16 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 512, timeout: 2 * time.Hour, tmpfsBytes: 1 << 30,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30,
			},
			runners.KindVerification: {
				network: NetworkNone, memoryBytes: 8 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 1024, timeout: time.Hour, tmpfsBytes: 2 << 30,
				maxLogBytes: 16 << 20, maxArtifactBytes: 2 << 30,
			},
			runners.KindQC: {
				network: NetworkNone, worktreeReadOnly: true, memoryBytes: 16 << 30, nanoCPUs: 4_000_000_000,
				pidsLimit: 512, timeout: time.Hour, tmpfsBytes: 1 << 30,
				maxLogBytes: 8 << 20, maxArtifactBytes: 1 << 30,
			},
		},
	}, nil
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
		Image: p.images[request.Kind], User: "65532:65532", ReadOnlyRoot: true,
		Network: selected.network, Mounts: mounts, TmpfsBytes: selected.tmpfsBytes,
		MemoryBytes: selected.memoryBytes, NanoCPUs: selected.nanoCPUs, PIDsLimit: selected.pidsLimit,
		WallTimeout: selected.timeout, MaxLogBytes: selected.maxLogBytes, MaxArtifactBytes: selected.maxArtifactBytes,
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
