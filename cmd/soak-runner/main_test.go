package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/soak"
)

func TestSoakRunnerWritesValidRetainedJSONReport(t *testing.T) {
	output := filepath.Join(t.TempDir(), "soak-report.json")
	var stdout bytes.Buffer
	err := run([]string{
		"--iterations", "4",
		"--generated-at", "2026-07-24T12:30:00Z",
		"--output", output,
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, stdout.Bytes()) {
		t.Fatal("stdout and retained report differ")
	}
	var report soak.Report
	if err := json.Unmarshal(payload, &report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	if report.Status != soak.StatusPassed {
		t.Fatalf("status = %s", report.Status)
	}
}
