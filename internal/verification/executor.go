package verification

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// LocalExecutor exists for deterministic offline fixtures and the explicit
// single-user development profile. Production invokes the same fixed command
// registry inside runnerd-managed verifier containers.
type LocalExecutor struct{}

func (LocalExecutor) Run(parent context.Context, worktree string, command Command, timeout time.Duration, maxLogBytes int64) ExecutionResult {
	started := time.Now().UTC()
	result := ExecutionResult{
		Class: command.Class, Executable: command.Executable,
		Arguments: append([]string(nil), command.Arguments...), StartedAt: started, ExitCode: -1,
	}
	if timeout <= 0 || timeout > 2*time.Hour {
		timeout = 15 * time.Minute
	}
	if maxLogBytes <= 0 || maxLogBytes > 64<<20 {
		maxLogBytes = 8 << 20
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	executable, err := exec.LookPath(command.Executable)
	if err != nil {
		result.Output = "executable is unavailable in this runner profile"
		result.Duration = time.Since(started)
		return result
	}
	process := exec.CommandContext(ctx, executable, command.Arguments...)
	process.Dir = worktree
	cacheKey := strings.NewReplacer("/", "_", string(filepath.Separator), "_").Replace(filepath.Base(worktree))
	temporaryRoot := os.TempDir()
	process.Env = []string{
		"HOME=/nonexistent", "LANG=C", "LC_ALL=C", "PATH=/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/bin:/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GOCACHE=" + filepath.Join(temporaryRoot, "maintainer-gocache-"+cacheKey),
		"GOMODCACHE=" + filepath.Join(temporaryRoot, "maintainer-gomodcache-"+cacheKey),
		"npm_config_cache=" + filepath.Join(temporaryRoot, "maintainer-npmcache-"+cacheKey),
		"CARGO_HOME=" + filepath.Join(temporaryRoot, "maintainer-cargo-"+cacheKey),
	}
	output := &boundedOutput{limit: int(maxLogBytes)}
	process.Stdout = output
	process.Stderr = output
	err = process.Run()
	result.Duration = time.Since(started)
	result.Output = redact(output.buffer.String())
	result.Truncated = output.truncated
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if process.ProcessState != nil {
		result.ExitCode = process.ProcessState.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	return result
}

type boundedOutput struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedOutput) Write(payload []byte) (int, error) {
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

var redactions = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]+`),
	regexp.MustCompile(`AKIA[0-9A-Z]+`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*`),
}

func redact(value string) string {
	for _, pattern := range redactions {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}
