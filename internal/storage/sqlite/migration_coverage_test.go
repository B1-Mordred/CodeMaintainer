package sqlite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type migrationCoverageFile struct {
	SchemaVersion        int                      `json:"schema_version"`
	CurrentSchemaVersion int                      `json:"current_schema_version"`
	Migrations           []migrationCoverageEntry `json:"migrations"`
}

type migrationCoverageEntry struct {
	ID                    string   `json:"id"`
	Version               int      `json:"version"`
	Title                 string   `json:"title"`
	File                  string   `json:"file"`
	DurableRecords        []string `json:"durable_records"`
	RestartResumeEvidence []string `json:"restart_resume_evidence"`
	BackupRestoreEvidence []string `json:"backup_restore_evidence"`
	Docs                  []string `json:"docs"`
	RollbackGuidance      string   `json:"rollback_guidance"`
}

func TestMigrationCoverageInventoryMatchesEmbeddedForwardMigrations(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	payload, err := os.ReadFile(filepath.Join(repoRoot, "config", "migration-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage migrationCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.SchemaVersion != 1 {
		t.Fatalf("coverage schema version = %d, want 1", coverage.SchemaVersion)
	}
	if coverage.CurrentSchemaVersion != CurrentSchemaVersion {
		t.Fatalf("coverage current schema version = %d, want %d", coverage.CurrentSchemaVersion, CurrentSchemaVersion)
	}
	migrations := allMigrations()
	if len(migrations) != CurrentSchemaVersion {
		t.Fatalf("embedded migration count = %d, want %d", len(migrations), CurrentSchemaVersion)
	}
	seenEmbedded := map[int]bool{}
	for _, migration := range migrations {
		if migration.version < 1 || migration.version > CurrentSchemaVersion {
			t.Fatalf("embedded migration version out of range: %d", migration.version)
		}
		if strings.TrimSpace(migration.sql) == "" {
			t.Fatalf("embedded migration %03d is empty", migration.version)
		}
		if seenEmbedded[migration.version] {
			t.Fatalf("duplicate embedded migration version %03d", migration.version)
		}
		seenEmbedded[migration.version] = true
	}

	filePattern := regexp.MustCompile(`^([0-9]{3})_.+\.sql$`)
	files, err := filepath.Glob(filepath.Join(repoRoot, "internal", "storage", "sqlite", "migrations", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != CurrentSchemaVersion {
		t.Fatalf("migration file count = %d, want %d", len(files), CurrentSchemaVersion)
	}
	expectedFiles := map[int]string{}
	for _, file := range files {
		name := filepath.Base(file)
		matches := filePattern.FindStringSubmatch(name)
		if len(matches) != 2 {
			t.Fatalf("migration file has unexpected name: %s", name)
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repoRoot, file)
		if err != nil {
			t.Fatal(err)
		}
		expectedFiles[version] = filepath.ToSlash(relative)
	}

	if len(coverage.Migrations) != CurrentSchemaVersion {
		t.Fatalf("coverage entry count = %d, want %d", len(coverage.Migrations), CurrentSchemaVersion)
	}
	seenCoverage := map[int]bool{}
	for _, entry := range coverage.Migrations {
		if entry.Version < 1 || entry.Version > CurrentSchemaVersion {
			t.Fatalf("coverage migration version out of range: %d", entry.Version)
		}
		if seenCoverage[entry.Version] {
			t.Fatalf("duplicate coverage entry for migration %03d", entry.Version)
		}
		seenCoverage[entry.Version] = true
		wantID := fmt.Sprintf("migration-%03d", entry.Version)
		if entry.ID != wantID {
			t.Fatalf("coverage ID for version %03d = %q, want %q", entry.Version, entry.ID, wantID)
		}
		if entry.File != expectedFiles[entry.Version] {
			t.Fatalf("coverage file for migration %03d = %q, want %q", entry.Version, entry.File, expectedFiles[entry.Version])
		}
		if strings.TrimSpace(entry.Title) == "" || strings.TrimSpace(entry.RollbackGuidance) == "" {
			t.Fatalf("coverage entry %03d is missing title or rollback guidance", entry.Version)
		}
		requireNonEmptyEvidence(t, entry.Version, "durable_records", entry.DurableRecords)
		requireExistingEvidencePaths(t, repoRoot, entry.Version, "restart_resume_evidence", entry.RestartResumeEvidence)
		requireExistingEvidencePaths(t, repoRoot, entry.Version, "backup_restore_evidence", entry.BackupRestoreEvidence)
		requireExistingEvidencePaths(t, repoRoot, entry.Version, "docs", entry.Docs)
	}
	for version := 1; version <= CurrentSchemaVersion; version++ {
		if !seenEmbedded[version] {
			t.Fatalf("embedded migration %03d missing", version)
		}
		if expectedFiles[version] == "" {
			t.Fatalf("migration file %03d missing", version)
		}
		if !seenCoverage[version] {
			t.Fatalf("coverage entry for migration %03d missing", version)
		}
	}
}

func requireNonEmptyEvidence(t *testing.T, version int, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("coverage entry %03d has no %s", version, field)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("coverage entry %03d has blank %s", version, field)
		}
	}
}

func requireExistingEvidencePaths(t *testing.T, repoRoot string, version int, field string, values []string) {
	t.Helper()
	requireNonEmptyEvidence(t, version, field, values)
	for _, value := range values {
		path := strings.Split(value, ":")[0]
		if path == "" {
			t.Fatalf("coverage entry %03d has invalid %s path %q", version, field, value)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil {
			t.Fatalf("coverage entry %03d references missing %s path %q: %v", version, field, path, err)
		}
	}
}

func TestMigrationCoverageEntriesAreSorted(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	payload, err := os.ReadFile(filepath.Join(repoRoot, "config", "migration-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage migrationCoverageFile
	if err := json.Unmarshal(payload, &coverage); err != nil {
		t.Fatal(err)
	}
	versions := make([]int, 0, len(coverage.Migrations))
	for _, entry := range coverage.Migrations {
		versions = append(versions, entry.Version)
	}
	if !sort.IntsAreSorted(versions) {
		t.Fatalf("coverage migration entries are not sorted: %v", versions)
	}
}
