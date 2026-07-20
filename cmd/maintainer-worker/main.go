package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/agents"
	"github.com/local-code-maintainer/appliance/internal/runners"
	"github.com/local-code-maintainer/appliance/internal/verification"
)

const (
	maxPacketBytes  = 64 << 10
	maxPacketChecks = 32
)

var workerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type verificationPacket struct {
	SchemaVersion         int                      `json:"schema_version"`
	Language              verification.Language    `json:"language"`
	Classes               []verification.Class     `json:"classes"`
	BaseSHA               string                   `json:"base_sha,omitempty"`
	Policy                verificationPacketPolicy `json:"policy,omitempty"`
	CommandTimeoutSeconds int64                    `json:"command_timeout_seconds"`
	MaxLogBytes           int64                    `json:"max_log_bytes"`
}

type verificationPacketPolicy struct {
	ProtectedPaths  []string `json:"protected_paths,omitempty"`
	MaxChangedFiles int      `json:"max_changed_files,omitempty"`
	MaxPatchBytes   int64    `json:"max_patch_bytes,omitempty"`
	MaxFileBytes    int64    `json:"max_file_bytes,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "dependencies" {
		return runDependencies("/workspace")
	}
	if len(arguments) == 1 && arguments[0] == "verification" {
		return runVerification("/inputs/00", "/workspace", "/artifacts")
	}
	if len(arguments) == 1 && (arguments[0] == "implementation" || arguments[0] == "qc") {
		return runAgent(arguments[0], "/inputs/00", "/workspace", "/artifacts")
	}
	if len(arguments) == 2 && arguments[0] == "verify" && arguments[1] == "go-format" {
		return checkGoFormat(".")
	}
	if len(arguments) == 2 && arguments[0] == "scan" {
		switch arguments[1] {
		case "secrets":
			return scanSecrets(".")
		case "dependencies":
			return inventoryDependencies(".")
		case "diff-policy":
			return fixedCommand("git", "diff", "--check")
		}
	}
	return errors.New("worker mode is not allow-listed")
}

func runDependencies(root string) error {
	type dependencyCommand struct {
		marker string
		name   string
		args   []string
	}
	commands := []dependencyCommand{
		{marker: "go.mod", name: "go", args: []string{"mod", "download"}},
		{marker: "package-lock.json", name: "npm", args: []string{"ci", "--ignore-scripts", "--cache", "/cache/npm"}},
		{marker: "Cargo.lock", name: "cargo", args: []string{"fetch", "--locked"}},
		{marker: "requirements.lock", name: "python3", args: []string{"-m", "pip", "download", "--require-hashes", "-r", "requirements.lock", "-d", "/cache/pip"}},
	}
	selected := make([]dependencyCommand, 0, len(commands))
	for _, candidate := range commands {
		info, err := os.Lstat(filepath.Join(root, candidate.marker))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("dependency marker %s is not a regular file", candidate.marker)
		}
		selected = append(selected, candidate)
	}
	if len(selected) == 0 {
		return nil
	}
	if len(selected) > 2 {
		return errors.New("repository declares too many dependency ecosystems for the default profile")
	}
	for _, selectedCommand := range selected {
		command := exec.Command(selectedCommand.name, selectedCommand.args...)
		command.Dir = root
		command.Env = append(os.Environ(),
			"HOME=/tmp", "GOMODCACHE=/cache/go-mod", "GOCACHE=/cache/go-build",
			"CARGO_HOME=/cache/cargo", "PIP_CACHE_DIR=/cache/pip", "npm_config_cache=/cache/npm",
		)
		var output cappedOutput
		output.maximum = 8 << 20
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Run(); err != nil {
			return fmt.Errorf("fixed dependency acquisition for %s failed: %w: %s", selectedCommand.marker, err, output.String())
		}
	}
	return nil
}

type cappedOutput struct {
	buffer  bytes.Buffer
	maximum int
	cut     bool
}

func (w *cappedOutput) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := w.maximum - w.buffer.Len()
	if remaining > 0 {
		if len(payload) > remaining {
			payload = payload[:remaining]
			w.cut = true
		}
		_, _ = w.buffer.Write(payload)
	} else {
		w.cut = true
	}
	return original, nil
}

func (w *cappedOutput) String() string {
	if w.cut {
		return w.buffer.String() + "\n[output truncated]"
	}
	return w.buffer.String()
}

func runAgent(mode, packetPath, worktree, artifactRoot string) error {
	file, err := os.Open(packetPath)
	if err != nil {
		return fmt.Errorf("open agent packet: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(file, agents.MaxPacketBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(payload) > agents.MaxPacketBytes {
		return errors.New("agent packet is unreadable or oversized")
	}
	client, err := agents.NewModelClient(os.Getenv("MAINTAINER_MODEL_ENDPOINT"))
	if err != nil {
		return err
	}
	var result []byte
	switch mode {
	case "implementation":
		result, err = agents.RunImplementation(context.Background(), client, payload, worktree)
		if err == nil {
			err = publishArtifact(artifactRoot, "implementation_result", "implementation_result", result)
		}
	case "qc":
		result, err = agents.RunQC(context.Background(), client, payload)
		if err == nil {
			err = publishArtifact(artifactRoot, "qc_report", "qc_report", result)
		}
	default:
		err = errors.New("agent mode is not allow-listed")
	}
	return err
}

func runVerification(packetPath, worktree, artifactRoot string) error {
	packet, err := readPacket(packetPath)
	if err != nil {
		return err
	}
	registry := verification.NewRegistry()
	commands := make([]verification.Command, 0, len(packet.Classes))
	seen := make(map[verification.Class]struct{}, len(packet.Classes))
	for _, class := range packet.Classes {
		if _, exists := seen[class]; exists {
			return fmt.Errorf("duplicate verification class %q", class)
		}
		seen[class] = struct{}{}
		command, resolveErr := registry.Resolve(packet.Language, class)
		if resolveErr != nil {
			return resolveErr
		}
		commands = append(commands, command)
	}
	results := make([]verification.ExecutionResult, 0, len(commands))
	passed := true
	var scan *verification.ScanResult
	if packet.BaseSHA != "" {
		scanner, scannerErr := verification.NewScanner()
		if scannerErr != nil {
			return scannerErr
		}
		value, scanErr := scanner.Scan(context.Background(), worktree, packet.BaseSHA, verification.Policy{
			ProtectedPaths: packet.Policy.ProtectedPaths, MaxChangedFiles: packet.Policy.MaxChangedFiles,
			MaxPatchBytes: packet.Policy.MaxPatchBytes, MaxFileBytes: packet.Policy.MaxFileBytes,
		})
		if scanErr != nil {
			return scanErr
		}
		scan = &value
		for _, finding := range value.Findings {
			if finding.Severity == "blocker" || finding.Severity == "must_fix" {
				passed = false
			}
		}
	}
	executor := verification.LocalExecutor{}
	for _, command := range commands {
		result := executor.Run(context.Background(), worktree, command,
			time.Duration(packet.CommandTimeoutSeconds)*time.Second, packet.MaxLogBytes)
		if result.ExitCode != 0 || result.TimedOut {
			passed = false
		}
		results = append(results, result)
	}
	payload, err := json.Marshal(struct {
		SchemaVersion int                            `json:"schema_version"`
		Passed        bool                           `json:"passed"`
		Scan          *verification.ScanResult       `json:"scan,omitempty"`
		Results       []verification.ExecutionResult `json:"results"`
	}{SchemaVersion: 1, Passed: passed, Scan: scan, Results: results})
	if err != nil {
		return err
	}
	if err := publishArtifact(artifactRoot, "command_results", "command_results", payload); err != nil {
		return err
	}
	if !passed {
		return errors.New("one or more fixed verification commands failed")
	}
	return nil
}

func readPacket(path string) (verificationPacket, error) {
	file, err := os.Open(path)
	if err != nil {
		return verificationPacket{}, fmt.Errorf("open verification packet: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxPacketBytes+1))
	if err != nil || len(payload) > maxPacketBytes {
		return verificationPacket{}, errors.New("verification packet is unreadable or oversized")
	}
	var packet verificationPacket
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&packet); err != nil {
		return verificationPacket{}, fmt.Errorf("decode verification packet: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return verificationPacket{}, errors.New("verification packet must contain one JSON document")
	}
	if packet.SchemaVersion != 1 || len(packet.Classes) == 0 || len(packet.Classes) > maxPacketChecks ||
		packet.CommandTimeoutSeconds < 1 || packet.CommandTimeoutSeconds > 3600 ||
		packet.MaxLogBytes < 1024 || packet.MaxLogBytes > 64<<20 {
		return verificationPacket{}, errors.New("verification packet exceeds fixed worker bounds")
	}
	if packet.BaseSHA != "" && !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(packet.BaseSHA) {
		return verificationPacket{}, errors.New("verification packet base SHA is invalid")
	}
	if len(packet.Policy.ProtectedPaths) > 100 || packet.Policy.MaxChangedFiles < 0 || packet.Policy.MaxChangedFiles > 10000 ||
		packet.Policy.MaxPatchBytes < 0 || packet.Policy.MaxPatchBytes > 64<<20 || packet.Policy.MaxFileBytes < 0 || packet.Policy.MaxFileBytes > 64<<20 {
		return verificationPacket{}, errors.New("verification packet policy exceeds fixed bounds")
	}
	return packet, nil
}

func publishArtifact(root, id, kind string, payload []byte) error {
	runID := os.Getenv("MAINTAINER_RUN_ID")
	maximum, err := strconv.ParseInt(os.Getenv("MAINTAINER_MAX_ARTIFACT_BYTES"), 10, 64)
	if !workerID.MatchString(runID) || !workerID.MatchString(id) || !workerID.MatchString(kind) ||
		err != nil || maximum <= 0 || int64(len(payload)) > maximum {
		return errors.New("worker artifact identity or size is invalid")
	}
	runDirectory := filepath.Join(root, runID)
	if err := os.Mkdir(runDirectory, 0o700); err != nil {
		return fmt.Errorf("create unique artifact directory: %w", err)
	}
	artifactPath := filepath.Join(runDirectory, id)
	if err := writeExclusive(artifactPath, payload); err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	manifest, err := json.Marshal([]runners.Artifact{{
		ID: id, Kind: kind, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(payload)),
	}})
	if err != nil {
		return err
	}
	temporary := filepath.Join(root, "."+runID+".manifest.tmp")
	if err := writeExclusive(temporary, manifest); err != nil {
		return err
	}
	final := filepath.Join(root, runID+".manifest.json")
	if err := os.Link(temporary, final); err != nil {
		return fmt.Errorf("publish immutable artifact manifest: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return fmt.Errorf("remove temporary artifact manifest: %w", err)
	}
	return nil
}

func writeExclusive(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return fmt.Errorf("create immutable worker artifact: %w", err)
	}
	if _, err = file.Write(payload); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write immutable worker artifact: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close immutable worker artifact: %w", closeErr)
	}
	return nil
}

func checkGoFormat(root string) error {
	unformatted := make([]string, 0)
	visited := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && (entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".go") {
			visited++
			if visited > 10000 {
				return errors.New("Go format scan exceeds 10000 files")
			}
			payload, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			formatted, formatErr := format.Source(payload)
			if formatErr != nil || !bytes.Equal(payload, formatted) {
				unformatted = append(unformatted, path)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(unformatted) != 0 {
		return fmt.Errorf("Go files require formatting: %s", strings.Join(unformatted, ", "))
	}
	return nil
}

var workerSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{12,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

func scanSecrets(root string) error {
	findings := make([]string, 0)
	visited := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		visited++
		if visited > 20000 {
			return errors.New("secret scan exceeds 20000 files")
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 5<<20 {
			return err
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(payload, 0) >= 0 {
			return nil
		}
		for _, pattern := range workerSecretPatterns {
			if pattern.Match(payload) {
				findings = append(findings, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return fmt.Errorf("possible secrets detected in: %s", strings.Join(findings, ", "))
	}
	return nil
}

func inventoryDependencies(root string) error {
	lockfiles := map[string]struct{}{
		"go.sum": {}, "package-lock.json": {}, "pnpm-lock.yaml": {}, "yarn.lock": {},
		"poetry.lock": {}, "uv.lock": {}, "Pipfile.lock": {}, "Cargo.lock": {},
	}
	found := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if _, exists := lockfiles[entry.Name()]; exists && entry.Type().IsRegular() {
			found = append(found, path)
		}
		if len(found) > 1000 {
			return errors.New("dependency inventory exceeds 1000 lockfiles")
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "offline dependency inventory: %s\n", strings.Join(found, ", "))
	return nil
}

func fixedCommand(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	command.Env = []string{"HOME=/nonexistent", "LANG=C", "LC_ALL=C", "PATH=/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
