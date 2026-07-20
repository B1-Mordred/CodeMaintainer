package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerRejectsUnknownModesAndUnformattedGo(t *testing.T) {
	if err := run([]string{"shell", "-c", "id"}); err == nil {
		t.Fatal("arbitrary worker mode was accepted")
	}
	root := t.TempDir()
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\nfunc X( ){ }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkGoFormat(root); err == nil {
		t.Fatal("unformatted Go source passed")
	}
	if err := os.WriteFile(path, []byte("package fixture\n\nfunc X() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkGoFormat(root); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerPacketAndArtifactPublicationAreBoundedAndImmutable(t *testing.T) {
	root := t.TempDir()
	packetPath := filepath.Join(root, "packet.json")
	packet := `{"schema_version":1,"language":"go","classes":["compile"],"command_timeout_seconds":30,"max_log_bytes":4096}`
	if err := os.WriteFile(packetPath, []byte(packet), 0o400); err != nil {
		t.Fatal(err)
	}
	parsed, err := readPacket(packetPath)
	if err != nil || len(parsed.Classes) != 1 {
		t.Fatalf("packet returned %#v, %v", parsed, err)
	}
	if err := os.Chmod(packetPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packetPath, []byte(strings.Replace(packet, "}", `,"command":["sh"]}`, 1)), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := readPacket(packetPath); err == nil {
		t.Fatal("packet with arbitrary command was accepted")
	}

	t.Setenv("MAINTAINER_RUN_ID", "run_fixture")
	t.Setenv("MAINTAINER_MAX_ARTIFACT_BYTES", "4096")
	if err := publishArtifact(root, "command_results", "command_results", []byte(`{"passed":true}`)); err != nil {
		t.Fatal(err)
	}
	manifestPayload, err := os.ReadFile(filepath.Join(root, "run_fixture.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal(manifestPayload, &manifest); err != nil || len(manifest) != 1 {
		t.Fatalf("manifest returned %#v, %v", manifest, err)
	}
	if err := publishArtifact(root, "command_results", "command_results", []byte(`{"passed":true}`)); err == nil {
		t.Fatal("artifact run directory was reused")
	}
	if err := publishArtifact(root, "too_large", "report", []byte(strings.Repeat("x", 4097))); err == nil {
		t.Fatal("oversized artifact was accepted")
	}
}
