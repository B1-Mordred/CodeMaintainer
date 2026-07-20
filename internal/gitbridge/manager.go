package gitbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	github        *GitHubConfiguration
}

func (m *Manager) EnableGitHub(configuration *GitHubConfiguration) error {
	if configuration == nil {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.github != nil || len(m.projects) != 0 {
		return ErrConflict
	}
	m.github = configuration
	return nil
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
	if !safeID.MatchString(registration.ProjectID) || !safeID.MatchString(registration.DefaultBranch) || !validRepository(registration.Repository) {
		return ErrInvalid
	}
	if registration.Provider == "local" {
		if !safeID.MatchString(registration.LocalRemoteName) || !strings.HasSuffix(registration.LocalRemoteName, ".git") {
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
	} else if registration.Provider != "github" || registration.LocalRemoteName != "" || m.github == nil {
		return ErrInvalid
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
	remote, authenticated, err := m.remote(registration)
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
		if _, cloneErr := m.gitRemote(ctx, authenticated, "clone", "--mirror", "--no-local", remote, clonePath); cloneErr != nil {
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
		if _, err := m.gitRemote(ctx, authenticated, "--git-dir="+mirror, "fetch", "--prune", remote,
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

func (m *Manager) Snapshot(ctx context.Context, projectID, revision string) (RepositorySnapshot, error) {
	registration, err := m.registration(projectID)
	if err != nil || !commit.MatchString(revision) {
		return RepositorySnapshot{}, ErrInvalid
	}
	mirror := filepath.Join(m.mirrorsRoot, projectID+".git")
	resolved, err := m.git(ctx, "--git-dir="+mirror, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil || strings.TrimSpace(resolved) != revision {
		return RepositorySnapshot{}, ErrNotFound
	}
	listing, err := m.git(ctx, "--git-dir="+mirror, "ls-tree", "-r", "-z", "--long", revision)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	result := RepositorySnapshot{ProjectID: projectID, Repository: registration.Repository, Revision: revision, Files: []SnapshotFile{}}
	total := 0
	for _, record := range strings.Split(listing, "\x00") {
		if record == "" {
			continue
		}
		header, filePath, ok := strings.Cut(record, "\t")
		if !ok {
			return RepositorySnapshot{}, ErrInvalid
		}
		fields := strings.Fields(header)
		if len(fields) != 4 || fields[1] != "blob" {
			result.Excluded++
			continue
		}
		size, sizeErr := strconv.Atoi(fields[3])
		if sizeErr != nil || size < 0 {
			return RepositorySnapshot{}, ErrInvalid
		}
		if size > 512<<10 || len(result.Files) >= 2000 || total+size > 2<<20 {
			result.Excluded++
			continue
		}
		content, readErr := m.git(ctx, "--git-dir="+mirror, "cat-file", "blob", fields[2])
		if readErr != nil {
			return RepositorySnapshot{}, readErr
		}
		if len(content) != size {
			return RepositorySnapshot{}, ErrInvalid
		}
		result.Files = append(result.Files, SnapshotFile{Path: filePath, Content: []byte(content)})
		total += size
	}
	return result, nil
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
	remote, authenticated, err := m.remote(registration)
	if err != nil {
		return Publication{}, err
	}
	var upstream string
	if authenticated {
		upstream, err = m.gitRemote(ctx, true, "ls-remote", "--heads", remote, "refs/heads/"+registration.DefaultBranch)
		upstream = firstRemoteSHA(upstream)
	} else {
		upstream, err = m.git(ctx, "--git-dir="+remote, "rev-parse", "--verify", "refs/heads/"+registration.DefaultBranch+"^{commit}")
		upstream = strings.TrimSpace(upstream)
	}
	if err != nil || upstream != request.BaseSHA {
		return Publication{}, ErrUpstreamMoved
	}
	path := filepath.Join(m.worktreesRoot, request.JobID)
	head, err := m.git(ctx, "-C", path, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != request.ResultSHA {
		return Publication{}, ErrConflict
	}
	branch := "maintainer/" + request.JobID
	var existing string
	var existingErr error
	if authenticated {
		existing, existingErr = m.gitRemote(ctx, true, "ls-remote", "--heads", remote, "refs/heads/"+branch)
		if existingErr == nil && strings.TrimSpace(existing) == "" {
			existingErr = ErrNotFound
		}
		existing = firstRemoteSHA(existing)
	} else {
		existing, existingErr = m.git(ctx, "--git-dir="+remote, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
		existing = strings.TrimSpace(existing)
	}
	if existingErr == nil {
		if existing != request.ResultSHA {
			return Publication{}, ErrConflict
		}
	} else if _, err := m.gitRemote(ctx, authenticated, "-C", path, "-c", "core.hooksPath=/dev/null", "push", "--porcelain", remote,
		"HEAD:refs/heads/"+branch); err != nil {
		return Publication{}, err
	}
	if authenticated {
		api, err := m.github.api(registration.Repository)
		if err != nil {
			return Publication{}, err
		}
		pull, err := api.CreateDraftPullRequest(ctx, DraftPullRequestRequest{
			IdempotencyKey: request.JobID + "_publication", Branch: branch, Base: registration.DefaultBranch,
			Title: "maintainer: verified repair for " + request.JobID,
			Body:  "Automated maintenance result " + request.ResultSHA + " was approved for draft publication. Review all controller evidence before merging.",
		})
		if err != nil {
			return Publication{}, err
		}
		return Publication{
			Provider: "github", Branch: branch, ResultSHA: request.ResultSHA, Draft: true,
			ExternalID: pull.NodeID, URL: pull.HTMLURL, Number: pull.Number,
		}, nil
	}
	return Publication{
		Provider: "local", Branch: branch, ResultSHA: request.ResultSHA, Draft: true,
		ExternalID: "local:" + branch, URL: "local://" + registration.LocalRemoteName + "/" + branch,
	}, nil
}

func (m *Manager) PullRequestEvent(ctx context.Context, projectID string, number int) (PullRequestEvent, error) {
	registration, err := m.registration(projectID)
	if err != nil || registration.Provider != "github" || m.github == nil || number <= 0 {
		return PullRequestEvent{}, ErrInvalid
	}
	api, err := m.github.api(registration.Repository)
	if err != nil {
		return PullRequestEvent{}, err
	}
	pull, err := api.PullRequest(ctx, number)
	if err != nil {
		return PullRequestEvent{}, err
	}
	payload, _ := json.Marshal(pull)
	digest := sha256.Sum256(payload)
	event := PullRequestEvent{
		DeliveryID: "poll-" + hex.EncodeToString(digest[:16]), Repository: registration.Repository,
		Number: pull.Number, Action: pull.State, Outcome: "pending", Branch: pull.Branch,
		BaseBranch: pull.Base, HeadSHA: pull.HeadSHA, PayloadSHA256: hex.EncodeToString(digest[:]),
	}
	if pull.State == "closed" {
		event.Action, event.Outcome = "closed", "rejected"
		if pull.Merged {
			event.Outcome, event.MergedCommit = "merged", pull.MergedCommit
		}
	}
	return event, nil
}

func (m *Manager) RepositoryDiagnostics(ctx context.Context, projectID string) (RepositoryDiagnostics, error) {
	registration, err := m.registration(projectID)
	if err != nil {
		return RepositoryDiagnostics{}, err
	}
	if registration.Provider == "local" {
		return RepositoryDiagnostics{
			Repository: registration.Repository, DefaultBranch: registration.DefaultBranch,
			CanRead: true, CanPublish: true, Ready: true, Problems: []string{},
		}, nil
	}
	if m.github == nil {
		return RepositoryDiagnostics{}, ErrInvalid
	}
	api, err := m.github.api(registration.Repository)
	if err != nil {
		return RepositoryDiagnostics{}, err
	}
	return api.Diagnostics(ctx)
}

func (m *Manager) Issue(ctx context.Context, projectID string, number int) (GitHubIssue, error) {
	registration, err := m.registration(projectID)
	if err != nil || registration.Provider != "github" || m.github == nil {
		return GitHubIssue{}, ErrInvalid
	}
	api, err := m.github.api(registration.Repository)
	if err != nil {
		return GitHubIssue{}, err
	}
	return api.Issue(ctx, number)
}

func (m *Manager) PullRequest(ctx context.Context, projectID string, number int) (GitHubPullRequest, error) {
	registration, err := m.registration(projectID)
	if err != nil || registration.Provider != "github" || m.github == nil {
		return GitHubPullRequest{}, ErrInvalid
	}
	api, err := m.github.api(registration.Repository)
	if err != nil {
		return GitHubPullRequest{}, err
	}
	return api.PullRequest(ctx, number)
}

func (m *Manager) remote(registration Registration) (string, bool, error) {
	if registration.Provider == "local" {
		remote, err := m.localRemote(registration.LocalRemoteName)
		return remote, false, err
	}
	if registration.Provider == "github" && m.github != nil {
		remote, err := m.github.remote(registration.Repository)
		return remote, true, err
	}
	return "", false, ErrInvalid
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

func (m *Manager) gitRemote(ctx context.Context, authenticated bool, args ...string) (string, error) {
	if !authenticated {
		return m.git(ctx, args...)
	}
	token, err := m.github.token(ctx)
	if err != nil {
		return "", err
	}
	askpass, err := os.CreateTemp("", "maintainer-git-askpass-*")
	if err != nil {
		return "", err
	}
	name := askpass.Name()
	defer os.Remove(name)
	if _, err := askpass.WriteString("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s\\n' x-access-token;; *) printf '%s\\n' \"$GITHUB_TOKEN\";; esac\n"); err != nil {
		askpass.Close()
		return "", err
	}
	if err := askpass.Chmod(0o700); err != nil {
		askpass.Close()
		return "", err
	}
	if err := askpass.Close(); err != nil {
		return "", err
	}
	return m.runGit(ctx, []string{"GIT_ASKPASS=" + name, "GIT_ASKPASS_REQUIRE=force", "GITHUB_TOKEN=" + token}, args...)
}

func firstRemoteSHA(output string) string {
	fields := strings.Fields(output)
	if len(fields) >= 2 && commit.MatchString(fields[0]) {
		return fields[0]
	}
	return ""
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
