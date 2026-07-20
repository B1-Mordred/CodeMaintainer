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

	"github.com/local-code-maintainer/appliance/internal/runners"
	"github.com/local-code-maintainer/appliance/internal/verification"
)

const (
	maxPacketBytes  = 64 << 10
	maxPacketChecks = 32
)

var workerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type verificationPacket struct {
	SchemaVersion         int                   `json:"schema_version"`
	Language              verification.Language `json:"language"`
	Classes               []verification.Class  `json:"classes"`
	CommandTimeoutSeconds int64                 `json:"command_timeout_seconds"`
	MaxLogBytes           int64                 `json:"max_log_bytes"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "verification" {
		return runVerification("/inputs/00", "/workspace", "/artifacts")
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
		Results       []verification.ExecutionResult `json:"results"`
	}{SchemaVersion: 1, Passed: passed, Results: results})
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
