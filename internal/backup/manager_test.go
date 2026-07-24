package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fixtureSnapshotter struct{}

func TestProductRenamePreservesBackupEncryptionContext(t *testing.T) {
	if backupAssociatedData != "local-code-maintainer-backup-v1" {
		t.Fatalf("backup associated data changed to %q; existing encrypted backups would be unreadable", backupAssociatedData)
	}
}

func (fixtureSnapshotter) Snapshot(_ context.Context, target string) error {
	return os.WriteFile(target, []byte("sqlite snapshot fixture"), 0o600)
}

func TestBackupCreatesChecksumBoundBundleAndValidatesDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "objects", "evidence"), []byte("verified evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(root, "mock", fixtureSnapshotter{}, "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Encrypted || record.SHA256 == "" || record.Bytes == 0 {
		t.Fatalf("unexpected record: %#v", record)
	}
	report, err := manager.Validate(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || !report.Compatible || !report.ChecksumsValid || report.Files != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	items, err := manager.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != record.ID {
		t.Fatalf("list = %#v, %v", items, err)
	}
	staged, err := manager.StageRestore(context.Background(), record.ID)
	if err != nil || !staged.Staged || !staged.RequiresRestart {
		t.Fatalf("stage = %#v, %v", staged, err)
	}
	databasePath := filepath.Join(root, "database", "controller.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, []byte("previous database"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(databasePath+suffix, []byte("stale sidecar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	applied, err := ApplyPending(root)
	if err != nil || !applied {
		t.Fatalf("apply pending = %v, %v", applied, err)
	}
	payload, err := os.ReadFile(databasePath)
	if err != nil || string(payload) != "sqlite snapshot fixture" {
		t.Fatalf("restored database = %q, %v", payload, err)
	}
	preserved, err := filepath.Glob(databasePath + ".pre-restore-*")
	if err != nil || len(preserved) != 1 {
		t.Fatalf("preserved previous database = %v, %v", preserved, err)
	}
	payload, err = os.ReadFile(preserved[0])
	if err != nil || string(payload) != "previous database" {
		t.Fatalf("preserved database = %q, %v", payload, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(databasePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar %s still exists or failed stat: %v", suffix, err)
		}
	}
}
