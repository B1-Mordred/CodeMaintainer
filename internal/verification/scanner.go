package verification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var exactCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)

type ScanResult struct {
	HeadSHA     string
	PatchSHA256 string
	Findings    []Finding
	Changed     []string
}

type Scanner struct {
	gitPath string
}

func NewScanner() (*Scanner, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("find git for verifier: %w", err)
	}
	return &Scanner{gitPath: path}, nil
}

func (s *Scanner) Scan(ctx context.Context, worktree, baseSHA string, policy Policy) (ScanResult, error) {
	if !filepath.IsAbs(worktree) || !exactCommit.MatchString(baseSHA) {
		return ScanResult{}, ErrInvalidRequest
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return ScanResult{}, fmt.Errorf("resolve verification worktree: %w", err)
	}
	policy = mergePolicyDefaults(policy)
	headOutput, _, err := s.git(ctx, root, 4096, "rev-parse", "HEAD")
	if err != nil {
		return ScanResult{}, err
	}
	head := strings.TrimSpace(string(headOutput))
	if !exactCommit.MatchString(head) {
		return ScanResult{}, errors.New("worktree HEAD is not an exact commit")
	}
	if _, _, err := s.git(ctx, root, 4096, "merge-base", "--is-ancestor", baseSHA, head); err != nil {
		return ScanResult{}, errors.New("verification head does not descend from the recorded base commit")
	}

	nameLimit := int64(policy.MaxChangedFiles*4096 + 1)
	nameOutput, namesTruncated, err := s.git(ctx, root, nameLimit, "diff", "--name-only", "-z", "--diff-filter=ACDMRTUXB", baseSHA, head, "--")
	if err != nil {
		return ScanResult{}, err
	}
	changed := splitNUL(nameOutput)
	result := ScanResult{HeadSHA: head, Changed: changed, Findings: make([]Finding, 0)}
	if namesTruncated || len(changed) > policy.MaxChangedFiles {
		result.Findings = append(result.Findings, Finding{
			Rule: "changed_file_limit", Severity: "blocker", Evidence: "changed path enumeration exceeded the configured limit",
			Verification: "reduce the patch and rerun diff policy scanning",
		})
	}

	patchOutput, patchTruncated, err := s.git(ctx, root, policy.MaxPatchBytes+1,
		"diff", "--binary", "--no-ext-diff", baseSHA, head, "--")
	if err != nil {
		return ScanResult{}, err
	}
	if patchTruncated || int64(len(patchOutput)) > policy.MaxPatchBytes {
		result.Findings = append(result.Findings, Finding{
			Rule: "patch_size", Severity: "blocker", Evidence: fmt.Sprintf("patch exceeds %d bytes", policy.MaxPatchBytes),
			Verification: "reduce the patch below the configured byte limit and rescan",
		})
	} else {
		digest := sha256.Sum256(patchOutput)
		result.PatchSHA256 = hex.EncodeToString(digest[:])
	}

	for _, path := range changed {
		if err := validateRepositoryPath(path); err != nil {
			result.Findings = append(result.Findings, Finding{
				Rule: "unsafe_path", Severity: "blocker", Path: path, Evidence: "Git reported an unsafe repository path",
				Verification: "remove the unsafe path and recreate the patch",
			})
			continue
		}
		for _, protected := range policy.ProtectedPaths {
			if path == strings.TrimSuffix(protected, "/") || strings.HasPrefix(path, protected) {
				result.Findings = append(result.Findings, Finding{
					Rule: "protected_path", Severity: "blocker", Path: path, Evidence: "changed path matches protected policy " + protected,
					Verification: "remove the protected-path change or obtain an authenticated policy exception",
				})
				break
			}
		}
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		info, statErr := os.Lstat(fullPath)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return ScanResult{}, fmt.Errorf("inspect changed path %s: %w", path, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			result.Findings = append(result.Findings, Finding{
				Rule: "symlink", Severity: "blocker", Path: path, Evidence: "patch introduces or modifies a symbolic link",
				Verification: "replace the symlink with a regular repository file and rescan",
			})
			continue
		}
		stage, _, stageErr := s.git(ctx, root, 8192, "ls-files", "--stage", "--", path)
		if stageErr == nil && bytes.HasPrefix(stage, []byte("160000 ")) {
			result.Findings = append(result.Findings, Finding{
				Rule: "submodule", Severity: "blocker", Path: path, Evidence: "patch contains a Git submodule entry",
				Verification: "remove the gitlink and rescan",
			})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > policy.MaxFileBytes {
			result.Findings = append(result.Findings, Finding{
				Rule: "file_size", Severity: "blocker", Path: path, Evidence: fmt.Sprintf("file is %d bytes", info.Size()),
				Verification: fmt.Sprintf("reduce the file below %d bytes", policy.MaxFileBytes),
			})
			continue
		}
		payload, readErr := readBoundedFile(fullPath, policy.MaxFileBytes)
		if readErr != nil {
			return ScanResult{}, fmt.Errorf("read changed file %s: %w", path, readErr)
		}
		if bytes.IndexByte(payload, 0) >= 0 {
			result.Findings = append(result.Findings, Finding{
				Rule: "binary", Severity: "blocker", Path: path, Evidence: "changed file contains NUL bytes",
				Verification: "remove the binary file or obtain an authenticated policy exception",
			})
		}
		for _, secret := range detectSecrets(payload) {
			result.Findings = append(result.Findings, Finding{
				Rule: "secret", Severity: "blocker", Path: path, Evidence: "changed file matches secret pattern " + secret,
				Verification: "remove and rotate the suspected credential, then rerun the secret scan",
			})
		}
	}
	return result, nil
}

func (s *Scanner) git(ctx context.Context, directory string, maximum int64, arguments ...string) ([]byte, bool, error) {
	command := exec.CommandContext(ctx, s.gitPath, append([]string{"-C", directory, "-c", "core.hooksPath=/dev/null"}, arguments...)...)
	command.Env = []string{
		"HOME=/nonexistent", "LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}
	output := &scanBuffer{limit: maximum}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return output.Bytes(), output.truncated, fmt.Errorf("verification git operation failed: %w: %s", err, redact(string(output.Bytes())))
	}
	return output.Bytes(), output.truncated, nil
}

type scanBuffer struct {
	buffer    bytes.Buffer
	limit     int64
	truncated bool
}

func (b *scanBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := b.limit - int64(b.buffer.Len())
	if remaining > 0 {
		if int64(len(payload)) > remaining {
			payload = payload[:remaining]
			b.truncated = true
		}
		_, _ = b.buffer.Write(payload)
	} else {
		b.truncated = true
	}
	return original, nil
}

func (b *scanBuffer) Bytes() []byte { return b.buffer.Bytes() }

func mergePolicyDefaults(policy Policy) Policy {
	defaults := DefaultPolicy()
	if len(policy.ProtectedPaths) == 0 {
		policy.ProtectedPaths = defaults.ProtectedPaths
	}
	if policy.MaxChangedFiles <= 0 {
		policy.MaxChangedFiles = defaults.MaxChangedFiles
	}
	if policy.MaxPatchBytes <= 0 {
		policy.MaxPatchBytes = defaults.MaxPatchBytes
	}
	if policy.MaxFileBytes <= 0 {
		policy.MaxFileBytes = defaults.MaxFileBytes
	}
	return policy
}

func splitNUL(payload []byte) []string {
	parts := bytes.Split(payload, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func validateRepositoryPath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsRune(path, 0) || strings.Contains(path, "\\") {
		return ErrInvalidRequest
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != path {
		return ErrInvalidRequest
	}
	return nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maximum {
		return nil, errors.New("file grew beyond verification limit")
	}
	return payload, nil
}

var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"private_key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

func detectSecrets(payload []byte) []string {
	result := make([]string, 0)
	for _, candidate := range secretPatterns {
		if candidate.pattern.Match(payload) {
			result = append(result, candidate.name)
		}
	}
	return result
}

func parseNumstat(payload []byte) (int64, error) {
	var total int64
	for _, line := range bytes.Split(payload, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) < 2 || bytes.Equal(fields[0], []byte("-")) {
			continue
		}
		added, err := strconv.ParseInt(string(fields[0]), 10, 64)
		if err != nil {
			return 0, err
		}
		removed, err := strconv.ParseInt(string(fields[1]), 10, 64)
		if err != nil {
			return 0, err
		}
		total += added + removed
	}
	return total, nil
}
