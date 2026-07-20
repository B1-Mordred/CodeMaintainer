package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type configurationCoverage struct {
	SchemaVersion int `json:"schema_version"`
	Settings      []struct {
		Key                string   `json:"key"`
		Capability         string   `json:"capability"`
		Descriptor         string   `json:"descriptor"`
		Application        []string `json:"application"`
		REST               []string `json:"rest"`
		CLI                []string `json:"cli"`
		UI                 string   `json:"ui"`
		Permission         string   `json:"permission"`
		ValidationDryRun   string   `json:"validation_dry_run"`
		PersistenceRuntime string   `json:"persistence_effective"`
		AuditRedaction     string   `json:"audit_redaction"`
		RollbackHistory    string   `json:"rollback_history"`
		Docs               []string `json:"docs"`
		Tests              []string `json:"tests"`
	} `json:"settings"`
}

func TestEveryDescriptorHasCompleteMachineReadableCoverage(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "configuration-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage configurationCoverage
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatalf("decode configuration coverage: %v", err)
	}
	if coverage.SchemaVersion != RegistrySchemaVersion {
		t.Fatalf("coverage schema version = %d, want %d", coverage.SchemaVersion, RegistrySchemaVersion)
	}
	registry, err := BuiltInRegistry(Default(".data"))
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for _, descriptor := range registry.Descriptors() {
		want[descriptor.Key] = true
	}
	seen := make(map[string]bool)
	for index, setting := range coverage.Settings {
		if !want[setting.Key] {
			t.Errorf("coverage setting %d has unregistered key %q", index, setting.Key)
		}
		if seen[setting.Key] {
			t.Errorf("coverage duplicates key %q", setting.Key)
		}
		seen[setting.Key] = true
		values := []string{
			setting.Key, setting.Capability, setting.Descriptor, setting.UI, setting.Permission,
			setting.ValidationDryRun, setting.PersistenceRuntime, setting.AuditRedaction,
			setting.RollbackHistory,
		}
		for field, value := range values {
			if strings.TrimSpace(value) == "" {
				t.Errorf("coverage for %q has empty required string field %d", setting.Key, field)
			}
		}
		for name, values := range map[string][]string{
			"application": setting.Application, "rest": setting.REST, "cli": setting.CLI,
			"docs": setting.Docs, "tests": setting.Tests,
		} {
			if len(values) == 0 {
				t.Errorf("coverage for %q has no %s mapping", setting.Key, name)
			}
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					t.Errorf("coverage for %q has an empty %s entry", setting.Key, name)
				}
			}
		}
		for _, document := range setting.Docs {
			path := strings.SplitN(document, "#", 2)[0]
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				t.Errorf("coverage for %q references missing documentation %q: %v", setting.Key, document, err)
			}
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("compiled descriptor %q has no configuration coverage entry", key)
		}
	}
}
