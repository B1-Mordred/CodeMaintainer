package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidWorktree = errors.New("invalid worktree request")
	ErrWorktreeRetired = errors.New("worktree identifier has been retired and cannot be reused")
	worktreeIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	commitPattern      = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

const maxGitOutputBytes = 64 << 10

type WorktreeManager struct {
	mirrorsRoot   string
	worktreesRoot string
	claimsRoot    string
	retiredRoot   string
	gitPath       string
}

type worktreeClaim struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
	BaseSHA   string `json:"base_sha"`
}

func NewWorktreeManager(mirrorsRoot, worktreesRoot string) (*WorktreeManager, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("find git: %w", err)
	}
	mirrors, err := canonicalRoot(mirrorsRoot, false)
	if err != nil {
		return nil, err
	}
	worktrees, err := canonicalRoot(worktreesRoot, true)
	if err != nil {
		return nil, err
	}
	manager := &WorktreeManager{
		mirrorsRoot: mirrors, worktreesRoot: worktrees,
		claimsRoot: filepath.Join(worktrees, ".claims"), retiredRoot: filepath.Join(worktrees, ".retired"),
		gitPath: gitPath,
	}
	for _, directory := range []string{manager.claimsRoot, manager.retiredRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create worktree metadata directory: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return nil, fmt.Errorf("protect worktree metadata directory: %w", err)
		}
	}
	return manager, nil
}

func (m *WorktreeManager) Create(ctx context.Context, projectID, jobID, baseSHA string) (Worktree, error) {
	if !validWorktreeID(projectID) || !validWorktreeID(jobID) || !commitPattern.MatchString(baseSHA) {
		return Worktree{}, ErrInvalidWorktree
	}
	mirror, err := m.mirrorPath(projectID)
	if err != nil {
		return Worktree{}, err
	}
	if _, err := os.Stat(filepath.Join(m.retiredRoot, jobID)); err == nil {
		return Worktree{}, ErrWorktreeRetired
	} else if !errors.Is(err, os.ErrNotExist) {
		return Worktree{}, fmt.Errorf("inspect retired worktree: %w", err)
	}
	claim := worktreeClaim{ProjectID: projectID, JobID: jobID, BaseSHA: baseSHA}
	if err := m.claim(claim); err != nil {
		return Worktree{}, err
	}
	resolved, err := m.git(ctx, "--git-dir="+mirror, "rev-parse", "--verify", baseSHA+"^{commit}")
	if err != nil || strings.TrimSpace(resolved) != baseSHA {
		return Worktree{}, fmt.Errorf("verify exact base commit: %w", coalesce(err, ErrInvalidWorktree))
	}
	path := filepath.Join(m.worktreesRoot, jobID)
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.IsDir() {
			return Worktree{}, ErrInvalidWorktree
		}
		head, headErr := m.git(ctx, "-C", path, "rev-parse", "HEAD")
		if headErr != nil || strings.TrimSpace(head) != baseSHA {
			return Worktree{}, errors.New("claimed worktree exists at an unexpected commit")
		}
		return claimedWorktree(claim, path), nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Worktree{}, fmt.Errorf("inspect worktree path: %w", statErr)
	}
	if _, err := m.git(ctx, "-c", "core.hooksPath=/dev/null", "--git-dir="+mirror,
		"worktree", "add", "--detach", path, baseSHA); err != nil {
		_, _ = m.git(ctx, "--git-dir="+mirror, "worktree", "prune", "--expire", "now")
		if _, retryErr := m.git(ctx, "-c", "core.hooksPath=/dev/null", "--git-dir="+mirror,
			"worktree", "add", "--detach", path, baseSHA); retryErr != nil {
			return Worktree{}, retryErr
		}
	}
	return claimedWorktree(claim, path), nil
}

func (m *WorktreeManager) Remove(ctx context.Context, projectID, jobID string) error {
	if !validWorktreeID(projectID) || !validWorktreeID(jobID) {
		return ErrInvalidWorktree
	}
	claim, err := m.readClaim(jobID)
	if err != nil {
		return err
	}
	if claim.ProjectID != projectID {
		return ErrInvalidWorktree
	}
	retiredPath := filepath.Join(m.retiredRoot, jobID)
	retired, err := os.OpenFile(retiredPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("retire worktree identifier: %w", err)
	}
	if err == nil {
		if _, writeErr := retired.WriteString(claim.BaseSHA + "\n"); writeErr != nil {
			retired.Close()
			return fmt.Errorf("record retired worktree: %w", writeErr)
		}
		if syncErr := retired.Sync(); syncErr != nil {
			retired.Close()
			return fmt.Errorf("sync retired worktree: %w", syncErr)
		}
		if closeErr := retired.Close(); closeErr != nil {
			return fmt.Errorf("close retired worktree: %w", closeErr)
		}
	}
	mirror, err := m.mirrorPath(projectID)
	if err != nil {
		return err
	}
	path := filepath.Join(m.worktreesRoot, jobID)
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil
	} else if statErr != nil {
		return fmt.Errorf("inspect worktree before removal: %w", statErr)
	}
	if _, err := m.git(ctx, "--git-dir="+mirror, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	return nil
}

func (m *WorktreeManager) claim(expected worktreeClaim) error {
	path := filepath.Join(m.claimsRoot, expected.JobID+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		actual, readErr := m.readClaim(expected.JobID)
		if readErr != nil {
			return readErr
		}
		if actual != expected {
			return errors.New("worktree identifier is already claimed by a different request")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim worktree identifier: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(expected); err != nil {
		file.Close()
		return fmt.Errorf("write worktree claim: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync worktree claim: %w", err)
	}
	return file.Close()
}

func (m *WorktreeManager) readClaim(jobID string) (worktreeClaim, error) {
	payload, err := os.ReadFile(filepath.Join(m.claimsRoot, jobID+".json"))
	if err != nil {
		return worktreeClaim{}, fmt.Errorf("read worktree claim: %w", err)
	}
	if len(payload) > 4096 {
		return worktreeClaim{}, errors.New("worktree claim is oversized")
	}
	var claim worktreeClaim
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claim); err != nil || !validWorktreeID(claim.ProjectID) ||
		!validWorktreeID(claim.JobID) || !commitPattern.MatchString(claim.BaseSHA) || claim.JobID != jobID {
		return worktreeClaim{}, errors.New("worktree claim is invalid")
	}
	return claim, nil
}

func (m *WorktreeManager) mirrorPath(projectID string) (string, error) {
	path := filepath.Join(m.mirrorsRoot, projectID+".git")
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve trusted mirror: %w", err)
	}
	relative, err := filepath.Rel(m.mirrorsRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("trusted mirror escapes mirror root")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("trusted bare mirror is unavailable")
	}
	return resolved, nil
}

func (m *WorktreeManager) git(ctx context.Context, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, m.gitPath, arguments...)
	command.Env = []string{
		"HOME=/nonexistent", "LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}
	var output cappedBuffer
	output.limit = maxGitOutputBytes
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.String(), fmt.Errorf("git operation failed: %w: %s", err, output.String())
	}
	return output.String(), nil
}

func canonicalRoot(path string, create bool) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrInvalidWorktree
	}
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func claimedWorktree(claim worktreeClaim, path string) Worktree {
	return Worktree{
		ProjectID: claim.ProjectID, JobID: claim.JobID, BaseSHA: claim.BaseSHA,
		Branch: "maintainer/" + claim.JobID, HandleID: "worktree-" + claim.JobID, Path: path,
	}
}

func validWorktreeID(value string) bool {
	return value != "." && value != ".." && worktreeIDPattern.MatchString(value)
}

func coalesce(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

type cappedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(payload) > remaining {
			payload = payload[:remaining]
			b.truncated = true
		}
		_, _ = b.buffer.Write(payload)
	} else {
		b.truncated = true
	}
	return original, nil
}

func (b *cappedBuffer) String() string {
	if b.truncated {
		return b.buffer.String() + "\n[output truncated]"
	}
	return b.buffer.String()
}
