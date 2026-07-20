package verification

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerFindsProtectedSecretSymlinkBinaryAndOversizedChanges(t *testing.T) {
	repository, base := verificationRepository(t)
	writeFixture(t, filepath.Join(repository, ".github", "workflows", "pwn.yml"), []byte("name: pwn\n"))
	writeFixture(t, filepath.Join(repository, ".gitmodules"), []byte("[submodule \"bad\"]\n"))
	writeFixture(t, filepath.Join(repository, "secret.txt"), []byte("ghp_1234567890abcdefghijklmnop\n"))
	writeFixture(t, filepath.Join(repository, "binary.bin"), []byte{'a', 0, 'b'})
	if err := os.Symlink("/etc/passwd", filepath.Join(repository, "escape")); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, repository, "add", "-A")
	runVerificationGit(t, repository, "commit", "-m", "unsafe patch")

	scanner, err := NewScanner()
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy()
	policy.MaxPatchBytes = 32
	result, err := scanner.Scan(context.Background(), repository, base, policy)
	if err != nil {
		t.Fatal(err)
	}
	rules := make(map[string]bool)
	for _, finding := range result.Findings {
		rules[finding.Rule] = true
	}
	for _, required := range []string{"protected_path", "secret", "symlink", "binary", "patch_size"} {
		if !rules[required] {
			t.Fatalf("missing %s finding: %#v", required, result.Findings)
		}
	}
	if result.PatchSHA256 != "" {
		t.Fatalf("oversized patch received hash %q", result.PatchSHA256)
	}
}

func TestScannerAcceptsBoundedTextPatchAndBindsExactHead(t *testing.T) {
	repository, base := verificationRepository(t)
	writeFixture(t, filepath.Join(repository, "safe.txt"), []byte("safe change\n"))
	runVerificationGit(t, repository, "add", "safe.txt")
	runVerificationGit(t, repository, "commit", "-m", "safe patch")
	head := strings.TrimSpace(runVerificationGit(t, repository, "rev-parse", "HEAD"))
	scanner, err := NewScanner()
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), repository, base, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 || result.HeadSHA != head || len(result.PatchSHA256) != 64 {
		t.Fatalf("safe scan returned %#v", result)
	}
}

func verificationRepository(t *testing.T) (string, string) {
	t.Helper()
	repository := t.TempDir()
	runVerificationGit(t, repository, "init")
	runVerificationGit(t, repository, "config", "user.name", "Fixture")
	runVerificationGit(t, repository, "config", "user.email", "fixture@example.invalid")
	writeFixture(t, filepath.Join(repository, "README.md"), []byte("fixture\n"))
	runVerificationGit(t, repository, "add", "README.md")
	runVerificationGit(t, repository, "commit", "-m", "base")
	return repository, strings.TrimSpace(runVerificationGit(t, repository, "rev-parse", "HEAD"))
}

func writeFixture(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runVerificationGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}
