package gitbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/local-code-maintainer/appliance/internal/repositories"
)

const maxGitOutput = 2 << 20

type Manager struct {
	mu            sync.Mutex
	mirrorsRoot   string
	worktreesRoot string
	remotesRoot   string
	gitPath       string
	worktrees     *repositories.WorktreeManager
	projects      map[string]Registration
}

func NewManager(mirrorsRoot, worktreesRoot, remotesRoot string) (*Manager, error) {
	for _, root := range []string{mirrorsRoot, worktreesRoot, remotesRoot} {
		if !filepath.IsAbs(root) {
			return nil, ErrInvalid
		}
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, err
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	worktrees, err := repositories.NewWorktreeManager(mirrorsRoot, worktreesRoot)
	if err != nil {
		return nil, err
	}
	return &Manager{
		mirrorsRoot: filepath.Clean(mirrorsRoot), worktreesRoot: filepath.Clean(worktreesRoot),
		remotesRoot: filepath.Clean(remotesRoot), gitPath: gitPath, worktrees: worktrees,
		projects: make(map[string]Registration),
	}, nil
}

func (m *Manager) Register(_ context.Context, registration Registration) error {
	if !safeID.MatchString(registration.ProjectID) || !safeID.MatchString(registration.DefaultBranch) ||
		registration.Provider != "local" || !safeID.MatchString(registration.LocalRemoteName) ||
		!strings.HasSuffix(registration.LocalRemoteName, ".git") || registration.Repository == "" {
		return ErrInvalid
	}
	remote, err := m.localRemote(registration.LocalRemoteName)
	if err != nil {
		return err
	}
	info, err := os.Stat(remote)
	if err != nil || !info.IsDir() {
		return ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.projects[registration.ProjectID]; ok && existing != registration {
		return ErrConflict
	}
	m.projects[registration.ProjectID] = registration
	return nil
}

func (m *Manager) Sync(ctx context.Context, projectID string) (SyncResult, error) {
	registration, err := m.registration(projectID)
	if err != nil {
		return SyncResult{}, err
	}
	remote, err := m.localRemote(registration.LocalRemoteName)
	if err != nil {
		return SyncResult{}, err
	}
	mirror := filepath.Join(m.mirrorsRoot, projectID+".git")
	if _, err := os.Stat(mirror); errors.Is(err, os.ErrNotExist) {
		temporary, tempErr := os.MkdirTemp(m.mirrorsRoot, ".clone-*")
		if tempErr != nil {
			return SyncResult{}, tempErr
		}
		defer os.RemoveAll(temporary)
		clonePath := filepath.Join(temporary, "mirror.git")
		if _, cloneErr := m.git(ctx, "clone", "--mirror", "--no-local", remote, clonePath); cloneErr != nil {
			return SyncResult{}, cloneErr
		}
		if renameErr := os.Rename(clonePath, mirror); renameErr != nil {
			if _, statErr := os.Stat(mirror); statErr != nil {
				return SyncResult{}, renameErr
			}
		}
	} else if err != nil {
		return SyncResult{}, err
	} else {
		if _, err := m.git(ctx, "--git-dir="+mirror, "fetch", "--prune", remote,
			"+refs/heads/*:refs/heads/*"); err != nil {
			return SyncResult{}, err
		}
	}
	base, err := m.git(ctx, "--git-dir="+mirror, "rev-parse", "--verify", "refs/heads/"+registration.DefaultBranch+"^{commit}")
	if err != nil || !commit.MatchString(strings.TrimSpace(base)) {
		return SyncResult{}, ErrNotFound
	}
	return SyncResult{ProjectID: projectID, BaseSHA: strings.TrimSpace(base)}, nil
}

func (m *Manager) CreateWorktree(ctx context.Context, request WorktreeRequest) (WorktreeResult, error) {
	if _, err := m.registration(request.ProjectID); err != nil || !safeID.MatchString(request.JobID) || !commit.MatchString(request.BaseSHA) {
		return WorktreeResult{}, ErrInvalid
	}
	worktree, err := m.worktrees.Create(ctx, request.ProjectID, request.JobID, request.BaseSHA)
	if err != nil {
		return WorktreeResult{}, err
	}
	return WorktreeResult{ProjectID: worktree.ProjectID, JobID: worktree.JobID, BaseSHA: worktree.BaseSHA, Branch: worktree.Branch}, nil
}

func (m *Manager) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	if _, err := m.registration(request.ProjectID); err != nil || !safeID.MatchString(request.JobID) ||
		!commit.MatchString(request.ExpectedHead) || !safeID.MatchString(request.OperationID) {
		return CommitResult{}, ErrInvalid
	}
	path := filepath.Join(m.worktreesRoot, request.JobID)
	head, err := m.git(ctx, "-C", path, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, err
	}
	head = strings.TrimSpace(head)
	if head != request.ExpectedHead {
		message, messageErr := m.git(ctx, "-C", path, "show", "-s", "--format=%B", "HEAD")
		if messageErr == nil && strings.Contains(message, "Maintainer-Operation: "+request.OperationID) {
			return CommitResult{ResultSHA: head}, nil
		}
		return CommitResult{}, ErrConflict
	}
	status, err := m.git(ctx, "-C", path, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(status) == "" {
		return CommitResult{ResultSHA: head}, nil
	}
	if _, err := m.git(ctx, "-C", path, "diff", "--check"); err != nil {
		return CommitResult{}, err
	}
	if _, err := m.git(ctx, "-C", path, "-c", "core.hooksPath=/dev/null", "add", "-A"); err != nil {
		return CommitResult{}, err
	}
	message := "maintainer: apply verified job " + request.JobID + "\n\nMaintainer-Operation: " + request.OperationID
	if _, err := m.gitWithIdentity(ctx, path, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false", "commit", "-m", message); err != nil {
		return CommitResult{}, err
	}
	result, err := m.git(ctx, "-C", path, "rev-parse", "HEAD")
	if err != nil || !commit.MatchString(strings.TrimSpace(result)) {
		return CommitResult{}, ErrConflict
	}
	return CommitResult{ResultSHA: strings.TrimSpace(result)}, nil
}

func (m *Manager) Diff(ctx context.Context, request DiffRequest) (DiffResult, error) {
	if _, err := m.registration(request.ProjectID); err != nil || !safeID.MatchString(request.JobID) ||
		!commit.MatchString(request.BaseSHA) || !commit.MatchString(request.ResultSHA) {
		return DiffResult{}, ErrInvalid
	}
	path := filepath.Join(m.worktreesRoot, request.JobID)
	patch, err := m.git(ctx, "-C", path, "diff", "--binary", "--no-ext-diff", request.BaseSHA+".."+request.ResultSHA)
	if err != nil {
		return DiffResult{}, err
	}
	return DiffResult{Patch: patch}, nil
}

func (m *Manager) Publish(ctx context.Context, request PublishRequest) (Publication, error) {
	registration, err := m.registration(request.ProjectID)
	if err != nil || !safeID.MatchString(request.JobID) || !commit.MatchString(request.BaseSHA) || !commit.MatchString(request.ResultSHA) {
		return Publication{}, ErrInvalid
	}
	remote, err := m.localRemote(registration.LocalRemoteName)
	if err != nil {
		return Publication{}, err
	}
	upstream, err := m.git(ctx, "--git-dir="+remote, "rev-parse", "--verify", "refs/heads/"+registration.DefaultBranch+"^{commit}")
	if err != nil || strings.TrimSpace(upstream) != request.BaseSHA {
		return Publication{}, ErrUpstreamMoved
	}
	path := filepath.Join(m.worktreesRoot, request.JobID)
	head, err := m.git(ctx, "-C", path, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != request.ResultSHA {
		return Publication{}, ErrConflict
	}
	branch := "maintainer/" + request.JobID
	existing, existingErr := m.git(ctx, "--git-dir="+remote, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if existingErr == nil {
		if strings.TrimSpace(existing) != request.ResultSHA {
			return Publication{}, ErrConflict
		}
	} else if _, err := m.git(ctx, "-C", path, "-c", "core.hooksPath=/dev/null", "push", "--porcelain", remote,
		"HEAD:refs/heads/"+branch); err != nil {
		return Publication{}, err
	}
	return Publication{
		Provider: "local", Branch: branch, ResultSHA: request.ResultSHA, Draft: true,
		ExternalID: "local:" + branch, URL: "local://" + registration.LocalRemoteName + "/" + branch,
	}, nil
}

func (m *Manager) registration(projectID string) (Registration, error) {
	if !safeID.MatchString(projectID) {
		return Registration{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	registration, ok := m.projects[projectID]
	if !ok {
		return Registration{}, ErrNotFound
	}
	return registration, nil
}

func (m *Manager) localRemote(name string) (string, error) {
	if !safeID.MatchString(name) || !strings.HasSuffix(name, ".git") {
		return "", ErrInvalid
	}
	path := filepath.Join(m.remotesRoot, name)
	relative, err := filepath.Rel(m.remotesRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalid
	}
	return path, nil
}

func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	return m.runGit(ctx, nil, args...)
}

func (m *Manager) gitWithIdentity(ctx context.Context, path string, args ...string) (string, error) {
	return m.runGit(ctx, []string{
		"GIT_AUTHOR_NAME=Local Code Maintainer", "GIT_AUTHOR_EMAIL=maintainer@localhost",
		"GIT_COMMITTER_NAME=Local Code Maintainer", "GIT_COMMITTER_EMAIL=maintainer@localhost",
	}, append([]string{"-C", path}, args...)...)
}

func (m *Manager) runGit(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, m.gitPath, args...)
	command.Env = append([]string{
		"HOME=/nonexistent", "LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}, extraEnv...)
	var output limitedBuffer
	output.limit = maxGitOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.String(), fmt.Errorf("bounded git operation failed: %w: %s", err, output.String())
	}
	return output.String(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
	cut   bool
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(payload) > remaining {
			payload = payload[:remaining]
			b.cut = true
		}
		_, _ = b.Buffer.Write(payload)
	} else {
		b.cut = true
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	if b.cut {
		return b.Buffer.String() + "\n[output truncated]"
	}
	return b.Buffer.String()
}
