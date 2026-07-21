package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	artifactfiles "github.com/B1-Mordred/CodeMaintainer/internal/artifacts"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/runners"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/verification"
)

var workflowID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type VerificationResult struct {
	SchemaVersion int                            `json:"schema_version"`
	Passed        bool                           `json:"passed"`
	Scan          *verification.ScanResult       `json:"scan,omitempty"`
	Results       []verification.ExecutionResult `json:"results"`
}

type ExecutionBackend interface {
	PrepareDependencies(context.Context, jobs.Job) error
	Implement(context.Context, jobs.Job, agents.TaskPacket) (agents.ImplementationResult, error)
	Verify(context.Context, jobs.Job, verification.Language, []verification.Class, string) (VerificationResult, error)
	Review(context.Context, jobs.Job, agents.TaskPacket) (agents.QCReport, error)
}

type artifactManager interface {
	Put(context.Context, artifactfiles.PutRequest) (storage.ArtifactRecord, error)
	StageInputs(context.Context, string, []string) (string, error)
}

type ContainerBackend struct {
	runner    runners.Runner
	artifacts artifactManager
	dataRoot  string
	poll      time.Duration
}

func NewContainerBackend(runner runners.Runner, artifacts artifactManager, dataRoot string) (*ContainerBackend, error) {
	if runner == nil || artifacts == nil || !filepath.IsAbs(dataRoot) {
		return nil, storage.ErrInvalid
	}
	return &ContainerBackend{runner: runner, artifacts: artifacts, dataRoot: filepath.Clean(dataRoot), poll: 200 * time.Millisecond}, nil
}

func (b *ContainerBackend) PrepareDependencies(ctx context.Context, job jobs.Job) error {
	runID, err := b.runner.Start(ctx, runners.JobRequest{JobID: job.ID, ProjectID: job.ProjectID, Kind: runners.KindDependencies})
	if err != nil {
		return err
	}
	status, err := b.wait(ctx, runID)
	if err != nil {
		return err
	}
	if status.State != "completed" {
		return b.runFailure(ctx, runID, "dependency acquisition", status.State)
	}
	return nil
}

func (b *ContainerBackend) Implement(ctx context.Context, job jobs.Job, packet agents.TaskPacket) (agents.ImplementationResult, error) {
	payload, err := json.Marshal(packet)
	if err != nil {
		return agents.ImplementationResult{}, err
	}
	raw, err := b.runWithPacket(ctx, job, runners.KindImplementation, payload, phaseKey(job)+"_implementation_packet", "implementation_result")
	if err != nil {
		return agents.ImplementationResult{}, err
	}
	return agents.DecodeImplementationResult(raw)
}

func (b *ContainerBackend) Verify(ctx context.Context, job jobs.Job, language verification.Language, classes []verification.Class, purpose string) (VerificationResult, error) {
	if !workflowID.MatchString(purpose) || len(classes) == 0 {
		return VerificationResult{}, storage.ErrInvalid
	}
	packet := struct {
		SchemaVersion         int                   `json:"schema_version"`
		Language              verification.Language `json:"language"`
		Classes               []verification.Class  `json:"classes"`
		BaseSHA               string                `json:"base_sha"`
		Policy                map[string]any        `json:"policy"`
		CommandTimeoutSeconds int64                 `json:"command_timeout_seconds"`
		MaxLogBytes           int64                 `json:"max_log_bytes"`
	}{
		SchemaVersion: 1, Language: language, Classes: classes, BaseSHA: job.BaseSHA,
		Policy: map[string]any{
			"protected_paths":   []string{".github/workflows/", "CODEOWNERS", ".gitmodules"},
			"max_changed_files": 200, "max_patch_bytes": int64(2 << 20), "max_file_bytes": int64(5 << 20),
		},
		CommandTimeoutSeconds: 900, MaxLogBytes: 8 << 20,
	}
	payload, err := json.Marshal(packet)
	if err != nil {
		return VerificationResult{}, err
	}
	raw, runState, err := b.runPacketAllowFailure(ctx, job, runners.KindVerification, payload,
		phaseKey(job)+"_"+purpose+"_packet", "command_results")
	if err != nil {
		return VerificationResult{}, err
	}
	var result VerificationResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.SchemaVersion != 1 || len(result.Results) != len(classes) {
		return VerificationResult{}, errors.New("verification worker returned an invalid report")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return VerificationResult{}, errors.New("verification worker report contains trailing data")
	}
	if (runState == "completed") != result.Passed {
		return VerificationResult{}, errors.New("verification worker state does not match its report")
	}
	return result, nil
}

func (b *ContainerBackend) Review(ctx context.Context, job jobs.Job, packet agents.TaskPacket) (agents.QCReport, error) {
	payload, err := json.Marshal(packet)
	if err != nil {
		return agents.QCReport{}, err
	}
	raw, err := b.runWithPacket(ctx, job, runners.KindQC, payload, phaseKey(job)+"_qc_packet", "qc_report")
	if err != nil {
		return agents.QCReport{}, err
	}
	return agents.DecodeQCReport(raw, packet)
}

func (b *ContainerBackend) runWithPacket(ctx context.Context, job jobs.Job, kind runners.Kind, payload []byte, key, artifactID string) ([]byte, error) {
	raw, state, err := b.runPacketAllowFailure(ctx, job, kind, payload, key, artifactID)
	if err != nil {
		return nil, err
	}
	if state != "completed" {
		return nil, fmt.Errorf("%s worker failed after publishing bounded evidence", kind)
	}
	return raw, nil
}

func (b *ContainerBackend) runPacketAllowFailure(ctx context.Context, job jobs.Job, kind runners.Kind, payload []byte, key, artifactID string) ([]byte, string, error) {
	packet, err := b.artifacts.Put(ctx, artifactfiles.PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: "task_packet", MediaType: "application/json",
		Producer: "workflow-controller", IdempotencyKey: key, Metadata: json.RawMessage(fmt.Sprintf(`{"worker_kind":%q}`, kind)),
		Reader: bytes.NewReader(payload), MaxBytes: 2 << 20,
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := b.artifacts.StageInputs(ctx, job.ID, []string{packet.ID}); err != nil {
		return nil, "", err
	}
	runID, err := b.runner.Start(ctx, runners.JobRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: kind, InputArtifactIDs: []string{packet.ID},
	})
	if err != nil {
		return nil, "", err
	}
	status, err := b.wait(ctx, runID)
	if err != nil {
		return nil, "", err
	}
	if status.State != "completed" && status.State != "failed" {
		return nil, "", b.runFailure(ctx, runID, string(kind), status.State)
	}
	raw, err := b.collectArtifact(ctx, job, runID, artifactID, key+"_"+artifactID)
	if err != nil {
		return nil, "", b.runFailure(ctx, runID, string(kind), status.State)
	}
	return raw, status.State, nil
}

func (b *ContainerBackend) wait(ctx context.Context, runID runners.RunID) (runners.Status, error) {
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()
	for {
		status, err := b.runner.Inspect(ctx, runID)
		if err != nil {
			return runners.Status{}, err
		}
		if status.State != "running" && status.State != "created" {
			return status, nil
		}
		select {
		case <-ctx.Done():
			_ = b.runner.Stop(context.WithoutCancel(ctx), runID)
			return runners.Status{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *ContainerBackend) collectArtifact(ctx context.Context, job jobs.Job, runID runners.RunID, id, idempotencyKey string) ([]byte, error) {
	items, err := b.runner.Artifacts(ctx, runID)
	if err != nil {
		return nil, err
	}
	var expected *runners.Artifact
	for index := range items {
		if items[index].ID == id {
			expected = &items[index]
			break
		}
	}
	if expected == nil || !workflowID.MatchString(id) || expected.Bytes < 0 || expected.Bytes > 64<<20 {
		return nil, errors.New("worker did not publish the required bounded artifact")
	}
	path := filepath.Join(b.dataRoot, "artifacts", job.ID, string(runID), id)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() != expected.Bytes {
		return nil, errors.New("worker artifact path is missing, mutable, or invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(io.LimitReader(file, expected.Bytes+1))
	closeErr := file.Close()
	digest := sha256.Sum256(payload)
	if readErr != nil || closeErr != nil || int64(len(payload)) != expected.Bytes || hex.EncodeToString(digest[:]) != expected.SHA256 {
		return nil, errors.New("worker artifact failed controller integrity verification")
	}
	_, err = b.artifacts.Put(ctx, artifactfiles.PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: expected.Kind, MediaType: "application/json",
		Producer: "workflow-controller", IdempotencyKey: idempotencyKey,
		Metadata: json.RawMessage(fmt.Sprintf(`{"run_id":%q,"worker_artifact":%q}`, runID, id)),
		Reader:   bytes.NewReader(payload), MaxBytes: 64 << 20,
	})
	return payload, err
}

func (b *ContainerBackend) runFailure(ctx context.Context, runID runners.RunID, phase, state string) error {
	logs, _ := b.runner.Logs(ctx, runID, 0, 16<<10)
	return fmt.Errorf("%s worker ended in %s: %s", phase, state, strings.TrimSpace(logs.Data))
}

func phaseKey(job jobs.Job) string {
	return "phase_" + strconv.FormatInt(job.Version, 10) + "_" + string(job.State)
}

func BuildRelevantFiles(worktree string, maximumFiles int, maximumBytes int64) ([]agents.FileContext, error) {
	if !filepath.IsAbs(worktree) || maximumFiles < 1 || maximumFiles > 64 || maximumBytes < 1 || maximumBytes > 2<<20 {
		return nil, storage.ErrInvalid
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == ".git" {
			return nil
		}
		if entry.Type().IsRegular() {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			paths = append(paths, filepath.ToSlash(relative))
			if len(paths) > maximumFiles*20 {
				return errors.New("repository context candidate count exceeds bound")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(paths, func(i, j int) bool {
		return contextPriority(paths[i]) < contextPriority(paths[j]) ||
			(contextPriority(paths[i]) == contextPriority(paths[j]) && paths[i] < paths[j])
	})
	files := make([]agents.FileContext, 0, maximumFiles)
	var total int64
	for _, relative := range paths {
		if len(files) == maximumFiles {
			break
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() > 128<<10 || info.Size() > maximumBytes-total {
			continue
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil || bytes.IndexByte(payload, 0) >= 0 {
			continue
		}
		files = append(files, agents.FileContext{Path: relative, Content: string(payload)})
		total += int64(len(payload))
	}
	if len(files) == 0 {
		return nil, errors.New("repository contains no bounded text context")
	}
	return files, nil
}

func contextPriority(path string) int {
	switch {
	case strings.HasSuffix(path, "_test.go"), strings.Contains(path, ".test."), strings.HasPrefix(path, "test"), strings.Contains(path, "/test"):
		return 1
	case strings.HasSuffix(path, ".go"), strings.HasSuffix(path, ".py"), strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".rs"), strings.HasSuffix(path, ".c"), strings.HasSuffix(path, ".cpp"):
		return 0
	default:
		return 2
	}
}
