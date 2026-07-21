package runnerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
)

const (
	dockerAPIVersion  = "v1.44"
	maxDockerResponse = int64(2 << 20)
	maxDockerLogs     = int64(64 << 20)
)

var dockerContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type DockerExecutor struct {
	client            *dockerClient
	allowedRoot       string
	dependencyNetwork string
	inferenceNetwork  string
	inferenceURL      string
	workerUser        string
	ctx               context.Context
	cancel            context.CancelFunc
	monitorMu         sync.Mutex
	monitors          map[runners.RunID]struct{}
}

func NewDockerExecutor(socketPath, allowedRoot, dependencyNetwork, inferenceNetwork, inferenceURL, workerUser string) (*DockerExecutor, error) {
	parsedInferenceURL, urlErr := url.Parse(inferenceURL)
	if socketPath == "" || !filepath.IsAbs(allowedRoot) || !safeIdentifier(dependencyNetwork) ||
		!safeIdentifier(inferenceNetwork) || dependencyNetwork == inferenceNetwork || urlErr != nil ||
		parsedInferenceURL.Scheme != "http" || parsedInferenceURL.Host == "" || parsedInferenceURL.User != nil ||
		parsedInferenceURL.RawQuery != "" || parsedInferenceURL.Fragment != "" ||
		(parsedInferenceURL.Path != "" && parsedInferenceURL.Path != "/v1") || !numericUser.MatchString(workerUser) {
		return nil, fmt.Errorf("%w: invalid Docker executor configuration", ErrPolicyDenied)
	}
	root, err := filepath.EvalSymlinks(allowedRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve Docker executor data root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: Docker executor data root is unavailable", ErrPolicyDenied)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &DockerExecutor{
		client:      &dockerClient{http: &http.Client{Transport: transport, Timeout: 60 * time.Second}},
		allowedRoot: filepath.Clean(root), dependencyNetwork: dependencyNetwork,
		inferenceNetwork: inferenceNetwork, inferenceURL: strings.TrimSuffix(inferenceURL, "/"),
		workerUser: workerUser, ctx: ctx, cancel: cancel,
		monitors: make(map[runners.RunID]struct{}),
	}, nil
}

func (e *DockerExecutor) Close() error {
	e.cancel()
	e.client.http.CloseIdleConnections()
	return nil
}

func (e *DockerExecutor) Start(ctx context.Context, spec WorkerSpec) error {
	request, err := e.containerRequest(spec)
	if err != nil {
		return err
	}
	baseline, err := e.writableDiskUsage(spec.JobID, spec.ProjectID, spec.Kind)
	if err != nil {
		return fmt.Errorf("measure worker disk baseline: %w", err)
	}
	request.Labels["maintainer.disk_baseline_bytes"] = strconv.FormatInt(baseline, 10)
	var created struct {
		ID       string   `json:"Id"`
		Warnings []string `json:"Warnings"`
	}
	path := "/containers/create?name=" + url.QueryEscape("maintainer-"+string(spec.RunID))
	if err := e.client.json(ctx, http.MethodPost, path, request, &created); err != nil {
		if dockerStatus(err) != http.StatusConflict {
			return err
		}
		existing, inspectErr := e.inspect(ctx, spec.RunID)
		if inspectErr != nil {
			return inspectErr
		}
		labels := existing.Config.Labels
		if labels["maintainer.job_id"] != spec.JobID || labels["maintainer.project_id"] != spec.ProjectID ||
			labels["maintainer.kind"] != string(spec.Kind) {
			return errors.New("existing worker identity does not match the idempotent request")
		}
		if existing.State.Status == "created" {
			if err := e.client.json(ctx, http.MethodPost, "/containers/"+url.PathEscape("maintainer-"+string(spec.RunID))+"/start", nil, nil); err != nil {
				return err
			}
		}
		existingBaseline, parseErr := parsePositiveInt64(labels["maintainer.disk_baseline_bytes"], true)
		if parseErr != nil {
			return errors.New("existing worker disk baseline is invalid")
		}
		e.launchMonitor(spec.RunID, spec.JobID, spec.ProjectID, spec.Kind,
			time.Now().Add(spec.WallTimeout), existingBaseline, spec.MaxDiskBytes)
		return nil
	}
	if !dockerContainerID.MatchString(created.ID) || len(created.Warnings) != 0 {
		if dockerContainerID.MatchString(created.ID) {
			_ = e.client.json(context.Background(), http.MethodDelete, "/containers/"+created.ID+"?force=1", nil, nil)
		}
		return errors.New("worker daemon returned an invalid container identity or warning")
	}
	if err := e.client.json(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		_ = e.client.json(context.Background(), http.MethodDelete, "/containers/"+created.ID+"?force=1", nil, nil)
		return err
	}
	e.launchMonitor(spec.RunID, spec.JobID, spec.ProjectID, spec.Kind, time.Now().Add(spec.WallTimeout), baseline, spec.MaxDiskBytes)
	return nil
}

func (e *DockerExecutor) Inspect(ctx context.Context, runID runners.RunID) (runners.Status, error) {
	inspection, err := e.inspect(ctx, runID)
	if err != nil {
		return runners.Status{}, err
	}
	started, err := time.Parse(time.RFC3339Nano, inspection.State.StartedAt)
	if err != nil {
		started = time.Time{}
	}
	state := inspection.State.Status
	if inspection.State.Running {
		state = "running"
		if err := e.launchMonitorFromInspection(runID, inspection, started); err != nil {
			return runners.Status{}, err
		}
	} else if inspection.State.ExitCode == 0 && state == "exited" {
		state = "completed"
	} else if state == "exited" {
		state = "failed"
	}
	return runners.Status{
		ID: runID, JobID: inspection.Config.Labels["maintainer.job_id"],
		Kind: runners.Kind(inspection.Config.Labels["maintainer.kind"]), State: state, StartedAt: started,
	}, nil
}

func (e *DockerExecutor) Logs(ctx context.Context, runID runners.RunID, cursor int64, limit int) (runners.LogChunk, error) {
	if cursor < 0 || limit <= 0 || limit > 64<<10 {
		return runners.LogChunk{}, ErrPolicyDenied
	}
	if _, err := e.inspect(ctx, runID); err != nil {
		return runners.LogChunk{}, err
	}
	payload, sourceTruncated, err := e.client.raw(ctx, http.MethodGet,
		"/containers/maintainer-"+url.PathEscape(string(runID))+"/logs?stdout=1&stderr=1&timestamps=0", maxDockerLogs)
	if err != nil {
		return runners.LogChunk{}, err
	}
	decoded, decodeTruncated, err := decodeDockerLogs(payload, maxDockerLogs)
	if err != nil {
		return runners.LogChunk{}, err
	}
	if cursor >= int64(len(decoded)) {
		return runners.LogChunk{NextCursor: int64(len(decoded)), Truncated: sourceTruncated || decodeTruncated}, nil
	}
	end := cursor + int64(limit)
	if end > int64(len(decoded)) {
		end = int64(len(decoded))
	}
	return runners.LogChunk{
		NextCursor: end, Data: redactRunnerOutput(string(decoded[cursor:end])),
		Truncated: end < int64(len(decoded)) || sourceTruncated || decodeTruncated,
	}, nil
}

func (e *DockerExecutor) Stop(ctx context.Context, runID runners.RunID) error {
	if !safeIdentifier(string(runID)) {
		return ErrPolicyDenied
	}
	err := e.client.json(ctx, http.MethodPost,
		"/containers/maintainer-"+url.PathEscape(string(runID))+"/stop?t=10", nil, nil)
	if dockerStatus(err) == http.StatusNotModified {
		return nil
	}
	if dockerStatus(err) == http.StatusNotFound {
		return ErrRunNotFound
	}
	return err
}

func (e *DockerExecutor) Artifacts(ctx context.Context, runID runners.RunID) ([]runners.Artifact, error) {
	inspection, err := e.inspect(ctx, runID)
	if err != nil {
		return nil, err
	}
	jobID := inspection.Config.Labels["maintainer.job_id"]
	if !safeIdentifier(jobID) || !safeIdentifier(string(runID)) {
		return nil, ErrPolicyDenied
	}
	path := filepath.Join(e.allowedRoot, "artifacts", jobID, string(runID)+".manifest.json")
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []runners.Artifact{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read worker artifact manifest: %w", err)
	}
	if len(payload) > 1<<20 {
		return nil, errors.New("worker artifact manifest is oversized")
	}
	var items []runners.Artifact
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&items); err != nil || len(items) > 64 {
		return nil, errors.New("worker artifact manifest is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("worker artifact manifest is invalid")
	}
	for _, item := range items {
		digest, digestErr := hex.DecodeString(item.SHA256)
		if !safeIdentifier(item.ID) || !safeIdentifier(item.Kind) || digestErr != nil || len(digest) != 32 || item.Bytes < 0 {
			return nil, errors.New("worker artifact manifest entry is invalid")
		}
	}
	maximum, err := strconv.ParseInt(inspection.Config.Labels["maintainer.max_artifact_bytes"], 10, 64)
	if err != nil || maximum <= 0 || maximum > 16<<30 {
		return nil, errors.New("worker artifact limit label is invalid")
	}
	var total int64
	for _, item := range items {
		if item.Bytes > maximum-total {
			return nil, errors.New("worker artifacts exceed the run limit")
		}
		artifactPath := filepath.Join(e.allowedRoot, "artifacts", jobID, string(runID), item.ID)
		info, statErr := os.Lstat(artifactPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() != item.Bytes {
			return nil, errors.New("worker artifact file is missing, mutable, or has the wrong size")
		}
		file, openErr := os.Open(artifactPath)
		if openErr != nil {
			return nil, errors.New("worker artifact file cannot be opened")
		}
		hash := sha256.New()
		written, copyErr := io.Copy(hash, io.LimitReader(file, item.Bytes+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != item.Bytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return nil, errors.New("worker artifact file failed integrity verification")
		}
		total += item.Bytes
	}
	if items == nil {
		items = []runners.Artifact{}
	}
	return items, nil
}

type dockerCreateRequest struct {
	Image        string            `json:"Image"`
	User         string            `json:"User"`
	WorkingDir   string            `json:"WorkingDir"`
	AttachStdout bool              `json:"AttachStdout"`
	AttachStderr bool              `json:"AttachStderr"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	Env          []string          `json:"Env"`
	Labels       map[string]string `json:"Labels"`
	HostConfig   dockerHostConfig  `json:"HostConfig"`
}

type dockerHostConfig struct {
	Binds           []string          `json:"Binds"`
	Memory          int64             `json:"Memory"`
	MemorySwap      int64             `json:"MemorySwap"`
	NanoCPUs        int64             `json:"NanoCpus"`
	PidsLimit       int64             `json:"PidsLimit"`
	ReadonlyRootfs  bool              `json:"ReadonlyRootfs"`
	CapDrop         []string          `json:"CapDrop"`
	SecurityOpt     []string          `json:"SecurityOpt"`
	NetworkMode     string            `json:"NetworkMode"`
	Privileged      bool              `json:"Privileged"`
	PublishAllPorts bool              `json:"PublishAllPorts"`
	AutoRemove      bool              `json:"AutoRemove"`
	Tmpfs           map[string]string `json:"Tmpfs"`
	ShmSize         int64             `json:"ShmSize"`
	LogConfig       dockerLogConfig   `json:"LogConfig"`
	Init            bool              `json:"Init"`
}

type dockerLogConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config"`
}

func (e *DockerExecutor) containerRequest(spec WorkerSpec) (dockerCreateRequest, error) {
	if !safeIdentifier(string(spec.RunID)) || !safeIdentifier(spec.JobID) || !safeIdentifier(spec.ProjectID) ||
		!spec.Kind.Valid() || !digestImage.MatchString(spec.Image) || spec.User != e.workerUser ||
		!spec.ReadOnlyRoot || !spec.NoNewPrivileges || !spec.UseDefaultSeccomp ||
		len(spec.DropCapabilities) != 1 || spec.DropCapabilities[0] != "ALL" ||
		spec.MemoryBytes <= 0 || spec.NanoCPUs <= 0 || spec.PIDsLimit <= 0 ||
		spec.TmpfsBytes <= 0 || spec.MaxLogBytes <= 0 || spec.MaxArtifactBytes <= 0 ||
		spec.MaxDiskBytes <= 0 || spec.WallTimeout < time.Second {
		return dockerCreateRequest{}, fmt.Errorf("%w: incomplete hardened worker specification", ErrPolicyDenied)
	}
	network := "none"
	if spec.Network == NetworkDependencyEgress && spec.Kind == runners.KindDependencies {
		network = e.dependencyNetwork
	} else if spec.Network == NetworkInferenceOnly &&
		(spec.Kind == runners.KindImplementation || spec.Kind == runners.KindQC) {
		network = e.inferenceNetwork
	} else if spec.Network != NetworkNone {
		return dockerCreateRequest{}, fmt.Errorf("%w: network is not valid for worker kind", ErrPolicyDenied)
	}
	binds := make([]string, 0, len(spec.Mounts))
	targets := make(map[string]struct{}, len(spec.Mounts))
	for _, mount := range spec.Mounts {
		resolved, err := e.validateMount(spec, mount)
		if err != nil {
			return dockerCreateRequest{}, err
		}
		if _, exists := targets[mount.Target]; exists {
			return dockerCreateRequest{}, fmt.Errorf("%w: duplicate mount target", ErrPolicyDenied)
		}
		targets[mount.Target] = struct{}{}
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		binds = append(binds, resolved+":"+mount.Target+":"+mode)
	}
	environment := []string{
		"HOME=/tmp", "MAINTAINER_RUN_ID=" + string(spec.RunID), "MAINTAINER_JOB_ID=" + spec.JobID,
		"MAINTAINER_MAX_ARTIFACT_BYTES=" + strconv.FormatInt(spec.MaxArtifactBytes, 10),
	}
	if spec.Network == NetworkInferenceOnly {
		environment = append(environment, "MAINTAINER_MODEL_ENDPOINT="+e.inferenceURL)
	}
	return dockerCreateRequest{
		Image: spec.Image, User: spec.User, WorkingDir: "/workspace",
		AttachStdout: true, AttachStderr: true, Tty: false, OpenStdin: false,
		Env: environment,
		Labels: map[string]string{
			"maintainer.run_id": string(spec.RunID), "maintainer.job_id": spec.JobID,
			"maintainer.project_id": spec.ProjectID, "maintainer.kind": string(spec.Kind),
			"maintainer.wall_timeout_seconds": strconv.FormatInt(int64(spec.WallTimeout.Seconds()), 10),
			"maintainer.max_artifact_bytes":   strconv.FormatInt(spec.MaxArtifactBytes, 10),
			"maintainer.max_disk_bytes":       strconv.FormatInt(spec.MaxDiskBytes, 10),
		},
		HostConfig: dockerHostConfig{
			Binds: binds, Memory: spec.MemoryBytes, MemorySwap: spec.MemoryBytes,
			NanoCPUs: spec.NanoCPUs, PidsLimit: spec.PIDsLimit, ReadonlyRootfs: true,
			CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges"}, NetworkMode: network,
			Privileged: false, PublishAllPorts: false, AutoRemove: false,
			Tmpfs:   map[string]string{"/tmp": fmt.Sprintf("rw,exec,nosuid,nodev,size=%d", spec.TmpfsBytes)},
			ShmSize: 64 << 20, Init: true,
			LogConfig: dockerLogConfig{Type: "local", Config: map[string]string{
				"max-size": strconv.FormatInt(spec.MaxLogBytes, 10), "max-file": "1", "compress": "false",
			}},
		},
	}, nil
}

func (e *DockerExecutor) validateMount(spec WorkerSpec, mount Mount) (string, error) {
	if mount.Source == "" || strings.Contains(mount.Source, ":") || !filepath.IsAbs(mount.Source) {
		return "", ErrPolicyDenied
	}
	resolved, err := filepath.EvalSymlinks(mount.Source)
	if err != nil {
		return "", fmt.Errorf("%w: resolve worker mount: %v", ErrPolicyDenied, err)
	}
	relative, err := filepath.Rel(e.allowedRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: worker mount escapes data root", ErrPolicyDenied)
	}
	validTarget := false
	expectedSource := ""
	switch {
	case mount.Target == "/workspace":
		validTarget = mount.ReadOnly == (spec.Kind == runners.KindQC)
		expectedSource = filepath.Join(e.allowedRoot, "worktrees", spec.JobID)
	case mount.Target == "/artifacts":
		validTarget = !mount.ReadOnly
		expectedSource = filepath.Join(e.allowedRoot, "artifacts", spec.JobID)
	case mount.Target == "/cache":
		validTarget = !mount.ReadOnly && spec.Kind == runners.KindDependencies
		expectedSource = filepath.Join(e.allowedRoot, "caches", spec.ProjectID)
	case len(mount.Target) == len("/inputs/00") && strings.HasPrefix(mount.Target, "/inputs/"):
		_, parseErr := strconv.ParseUint(strings.TrimPrefix(mount.Target, "/inputs/"), 10, 8)
		inputRoot := filepath.Join(e.allowedRoot, "artifacts", "inputs", spec.JobID)
		relativeInput, relativeErr := filepath.Rel(inputRoot, resolved)
		validTarget = mount.ReadOnly && parseErr == nil && relativeErr == nil &&
			relativeInput != "." && relativeInput != ".." &&
			!strings.HasPrefix(relativeInput, ".."+string(filepath.Separator)) &&
			!strings.Contains(relativeInput, string(filepath.Separator))
		expectedSource = resolved
	}
	if !validTarget || filepath.Clean(resolved) != filepath.Clean(expectedSource) {
		return "", fmt.Errorf("%w: worker mount target or mode is forbidden", ErrPolicyDenied)
	}
	return filepath.Clean(resolved), nil
}

func (e *DockerExecutor) launchMonitor(runID runners.RunID, jobID, projectID string, kind runners.Kind, deadline time.Time, baseline, maximum int64) {
	e.monitorMu.Lock()
	if _, exists := e.monitors[runID]; exists {
		e.monitorMu.Unlock()
		return
	}
	e.monitors[runID] = struct{}{}
	e.monitorMu.Unlock()
	go e.monitorRun(runID, jobID, projectID, kind, deadline, baseline, maximum)
}

func (e *DockerExecutor) launchMonitorFromInspection(runID runners.RunID, inspection dockerInspection, started time.Time) error {
	labels := inspection.Config.Labels
	baseline, err := parsePositiveInt64(labels["maintainer.disk_baseline_bytes"], true)
	if err != nil {
		return errors.New("worker disk baseline label is invalid")
	}
	maximum, err := parsePositiveInt64(labels["maintainer.max_disk_bytes"], false)
	if err != nil || maximum > 64<<30 {
		return errors.New("worker disk limit label is invalid")
	}
	seconds, err := parsePositiveInt64(labels["maintainer.wall_timeout_seconds"], false)
	if err != nil || seconds > int64((24*time.Hour)/time.Second) || started.IsZero() {
		return errors.New("worker timeout label is invalid")
	}
	e.launchMonitor(runID, labels["maintainer.job_id"], labels["maintainer.project_id"],
		runners.Kind(labels["maintainer.kind"]), started.Add(time.Duration(seconds)*time.Second), baseline, maximum)
	return nil
}

func parsePositiveInt64(value string, allowZero bool) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || (!allowZero && parsed == 0) {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}

func (e *DockerExecutor) monitorRun(runID runners.RunID, jobID, projectID string, kind runners.Kind, deadline time.Time, baseline, maximum int64) {
	defer func() {
		e.monitorMu.Lock()
		delete(e.monitors, runID)
		e.monitorMu.Unlock()
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if !time.Now().Before(deadline) {
				_ = e.Stop(context.Background(), runID)
				return
			}
			usage, err := e.writableDiskUsage(jobID, projectID, kind)
			if err != nil || usage < baseline || usage-baseline > maximum {
				_ = e.Stop(context.Background(), runID)
				return
			}
			inspection, err := e.inspect(context.Background(), runID)
			if err != nil || !inspection.State.Running {
				return
			}
		}
	}
}

func (e *DockerExecutor) writableDiskUsage(jobID, projectID string, kind runners.Kind) (int64, error) {
	if !safeIdentifier(jobID) || !safeIdentifier(projectID) || !kind.Valid() {
		return 0, ErrPolicyDenied
	}
	paths := []string{
		filepath.Join(e.allowedRoot, "worktrees", jobID),
		filepath.Join(e.allowedRoot, "artifacts", jobID),
	}
	if kind == runners.KindDependencies {
		paths = append(paths, filepath.Join(e.allowedRoot, "caches", projectID))
	}
	var total int64
	for _, root := range paths {
		err := filepath.Walk(root, func(_ string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			if info.Size() > (1<<63-1)-total {
				return errors.New("worker disk usage overflow")
			}
			total += info.Size()
			return nil
		})
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

type dockerInspection struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		ExitCode  int    `json:"ExitCode"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
}

func (e *DockerExecutor) inspect(ctx context.Context, runID runners.RunID) (dockerInspection, error) {
	if !safeIdentifier(string(runID)) {
		return dockerInspection{}, ErrPolicyDenied
	}
	var inspection dockerInspection
	err := e.client.json(ctx, http.MethodGet, "/containers/maintainer-"+url.PathEscape(string(runID))+"/json", nil, &inspection)
	if dockerStatus(err) == http.StatusNotFound {
		return dockerInspection{}, ErrRunNotFound
	}
	if err != nil {
		return dockerInspection{}, err
	}
	if inspection.Config.Labels["maintainer.run_id"] != string(runID) ||
		!safeIdentifier(inspection.Config.Labels["maintainer.job_id"]) ||
		!safeIdentifier(inspection.Config.Labels["maintainer.project_id"]) ||
		!runners.Kind(inspection.Config.Labels["maintainer.kind"]).Valid() {
		return dockerInspection{}, errors.New("worker container identity labels are invalid")
	}
	return inspection, nil
}

type dockerClient struct{ http *http.Client }

type dockerError struct {
	Status int
	Body   string
}

func (e *dockerError) Error() string {
	return fmt.Sprintf("worker daemon returned %d: %s", e.Status, e.Body)
}

func dockerStatus(err error) int {
	var remote *dockerError
	if errors.As(err, &remote) {
		return remote.Status
	}
	return 0
}

func (c *dockerClient) json(ctx context.Context, method, path string, body, destination any) error {
	var source io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		source = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://worker/"+dockerAPIVersion+path, source)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call worker daemon: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxDockerResponse+1))
	if err != nil {
		return err
	}
	if int64(len(payload)) > maxDockerResponse {
		return errors.New("worker daemon response is oversized")
	}
	if response.StatusCode >= 300 {
		return &dockerError{Status: response.StatusCode, Body: redactRunnerOutput(string(payload))}
	}
	if destination != nil && len(payload) != 0 {
		if err := json.Unmarshal(payload, destination); err != nil {
			return fmt.Errorf("decode worker daemon response: %w", err)
		}
	}
	return nil
}

func (c *dockerClient) raw(ctx context.Context, method, path string, maximum int64) ([]byte, bool, error) {
	request, err := http.NewRequestWithContext(ctx, method, "http://worker/"+dockerAPIVersion+path, nil)
	if err != nil {
		return nil, false, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("call worker daemon: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, false, err
	}
	if response.StatusCode >= 300 {
		return nil, false, &dockerError{Status: response.StatusCode, Body: redactRunnerOutput(string(payload))}
	}
	truncated := int64(len(payload)) > maximum
	if truncated {
		payload = payload[:maximum]
	}
	return payload, truncated, nil
}

func decodeDockerLogs(payload []byte, maximum int64) ([]byte, bool, error) {
	var result bytes.Buffer
	truncated := false
	for len(payload) != 0 {
		if len(payload) < 8 {
			return nil, false, errors.New("worker daemon returned a malformed log frame")
		}
		size := int(binary.BigEndian.Uint32(payload[4:8]))
		if size < 0 || len(payload) < 8+size {
			return nil, false, errors.New("worker daemon returned a malformed log payload")
		}
		frame := payload[8 : 8+size]
		remaining := maximum - int64(result.Len())
		if remaining <= 0 {
			truncated = true
			break
		}
		if int64(len(frame)) > remaining {
			frame = frame[:remaining]
			truncated = true
		}
		_, _ = result.Write(frame)
		payload = payload[8+size:]
	}
	return result.Bytes(), truncated, nil
}
